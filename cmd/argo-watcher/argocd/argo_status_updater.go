package argocd

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/notifications"

	"github.com/shini4i/argo-watcher/internal/helpers"

	"github.com/avast/retry-go/v4"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/internal/lock"
	"github.com/shini4i/argo-watcher/internal/models"
)

const (
	failedToUpdateTaskStatusTemplate = "Failed to change task status: %s"
)

// DeploymentMonitor encapsulates the logic for tracking ArgoCD application rollouts.
type DeploymentMonitor struct {
	argo             Argo
	registryProxyUrl string
	retryOptions     []retry.Option
	acceptSuspended  bool
}

// GitUpdater encapsulates the logic required to update Git repositories watched by ArgoCD.
type GitUpdater struct {
	locker        lock.Locker
	repoCachePath string
}

// NewDeploymentMonitor creates a deployment monitor with the supplied configuration.
func NewDeploymentMonitor(argo Argo, registryProxyUrl string, retryOptions []retry.Option, acceptSuspended bool) *DeploymentMonitor {
	return &DeploymentMonitor{
		argo:             argo,
		registryProxyUrl: registryProxyUrl,
		retryOptions:     retryOptions,
		acceptSuspended:  acceptSuspended,
	}
}

// BeginTracking increments the in-progress task counter.
func (monitor *DeploymentMonitor) BeginTracking() {
	monitor.argo.metrics.AddInProgressTask()
}

// EndTracking decrements the in-progress task counter.
func (monitor *DeploymentMonitor) EndTracking() {
	monitor.argo.metrics.RemoveInProgressTask()
}

// FetchApplication retrieves the ArgoCD application by name.
func (monitor *DeploymentMonitor) FetchApplication(appName string) (*models.Application, error) {
	return monitor.argo.api.GetApplication(appName)
}

// StoreInitialAppStatus caches the initial rollout status for comparison during monitoring.
func (monitor *DeploymentMonitor) StoreInitialAppStatus(task *models.Task, application *models.Application) error {
	if application == nil {
		return errors.New("application is nil")
	}

	status := application.GetRolloutStatus(task.ListImages(), monitor.registryProxyUrl, monitor.acceptSuspended)

	// sort images to avoid hash mismatch
	slices.Sort(application.Status.Summary.Images)

	task.SavedAppStatus = models.SavedAppStatus{
		Status:     status,
		ImagesHash: helpers.GenerateHash(strings.Join(application.Status.Summary.Images, ",")),
	}

	return nil
}

// WaitRollout polls the application status until it reaches a final state or times out.
func (monitor *DeploymentMonitor) WaitRollout(task models.Task) (*models.Application, error) {
	var application *models.Application
	var err error

	retryOptions := monitor.configureRetryOptions(task)
	log.Debug().Str("id", task.Id).Msg("Waiting for rollout")

	err = retry.Do(func() error {
		application, err = monitor.argo.api.GetApplication(task.App)
		if err != nil {
			return handleApplicationFetchError(task, err)
		}

		// Early return for fire and forget mode
		if application.IsFireAndForgetModeActive() {
			log.Debug().Str("id", task.Id).Msg("Fire and forget mode is active, skipping checks...")
			return nil
		}

		return checkRolloutStatus(task, application, monitor.registryProxyUrl, monitor.acceptSuspended)
	}, retryOptions...)

	return application, err
}

func (monitor *DeploymentMonitor) configureRetryOptions(task models.Task) []retry.Option {
	const (
		minAttempts        = 15
		retryWindowSeconds = 15
	)

	retryOptions := append([]retry.Option{}, monitor.retryOptions...)
	if task.Timeout <= 0 {
		log.Debug().Str("id", task.Id).Msgf("Task timeout is non-positive, defaulting to %d attempts", minAttempts)
		return append(retryOptions, retry.Attempts(uint(minAttempts))) // #nosec G115
	}

	log.Debug().Str("id", task.Id).Msgf("Overriding task timeout to %ds", task.Timeout)

	calculatedAttempts := task.Timeout/retryWindowSeconds + 1

	if calculatedAttempts <= 0 {
		log.Warn().Msgf("Calculated attempts resulted in a non-positive number (%d), defaulting to %d attempts.", calculatedAttempts, minAttempts)
		calculatedAttempts = minAttempts
	}

	return append(retryOptions, retry.Attempts(uint(calculatedAttempts))) // #nosec G115
}

// ProcessDeploymentResult determines if the deployment was successful and updates the appropriate status and metrics.
func (monitor *DeploymentMonitor) ProcessDeploymentResult(task *models.Task, application *models.Application) {
	status := application.GetRolloutStatus(task.ListImages(), monitor.registryProxyUrl, monitor.acceptSuspended)
	if application.IsFireAndForgetModeActive() {
		status = models.ArgoRolloutAppSuccess
	}

	if status == models.ArgoRolloutAppSuccess {
		monitor.handleDeploymentSuccess(task)
	} else {
		monitor.handleDeploymentFailure(task, status, application)
	}
}

// HandleArgoAPIFailure processes API errors and updates task status accordingly.
func (monitor *DeploymentMonitor) HandleArgoAPIFailure(task models.Task, err error) {
	monitor.argo.metrics.AddFailedDeployment(task.App)
	finalStatus := determineFailureStatus(task, err)
	reason := fmt.Sprintf(ArgoAPIErrorTemplate, err.Error())
	log.Warn().Str("id", task.Id).Msgf("Deployment failed with status \"%s\". Aborting with error: %s", finalStatus, reason)

	if err := monitor.argo.State.SetTaskStatus(task.Id, finalStatus, reason); err != nil {
		log.Error().Str("id", task.Id).Msgf(failedToUpdateTaskStatusTemplate, err)
	}
}

func (monitor *DeploymentMonitor) handleDeploymentSuccess(task *models.Task) {
	log.Info().Str("id", task.Id).Msg("App is running on the expected version.")
	monitor.argo.metrics.ResetFailedDeployment(task.App)
	if err := monitor.argo.State.SetTaskStatus(task.Id, models.StatusDeployedMessage, ""); err != nil {
		log.Error().Str("id", task.Id).Msgf(failedToUpdateTaskStatusTemplate, err)
	}
	task.Status = models.StatusDeployedMessage
}

func (monitor *DeploymentMonitor) handleDeploymentFailure(task *models.Task, status string, application *models.Application) {
	log.Info().Str("id", task.Id).Msg("App deployment failed.")
	monitor.argo.metrics.AddFailedDeployment(task.App)
	reason := fmt.Sprintf(
		"Application deployment failed. Rollout status %q\n\n%s",
		status,
		application.GetRolloutMessage(status, task.ListImages()),
	)
	if err := monitor.argo.State.SetTaskStatus(task.Id, models.StatusFailedMessage, reason); err != nil {
		log.Error().Str("id", task.Id).Msgf(failedToUpdateTaskStatusTemplate, err)
	}
	task.Status = models.StatusFailedMessage
}

// NewGitUpdater creates a GitUpdater instance.
func NewGitUpdater(locker lock.Locker, repoCachePath string) *GitUpdater {
	return &GitUpdater{
		locker:        locker,
		repoCachePath: repoCachePath,
	}
}

// UpdateIfNeeded updates the git repository if the application is managed by the watcher and has valid credentials.
func (gitUpdater *GitUpdater) UpdateIfNeeded(app *models.Application, task models.Task) error {
	if !app.IsManagedByWatcher() || !task.Validated {
		log.Debug().Str("id", task.Id).Msg("Skipping git repo update: Application does not have the necessary annotations or token is missing.")
		return nil
	}

	gitopsRepo, err := models.NewGitopsRepo(app, gitUpdater.repoCachePath)
	if err != nil {
		log.Error().Str("id", task.Id).Msgf("Failed to get gitops repo info for app %s: %s", task.App, err)
		return err
	}

	gitUpdateFunc := func() error {
		log.Debug().Str("id", task.Id).Msg("Application managed by watcher. Initiating git repo update.")
		return gitUpdater.updateGitRepo(app, &task, &gitopsRepo)
	}

	err = gitUpdater.locker.WithLock(gitopsRepo.RepoUrl, gitUpdateFunc)
	if err != nil {
		log.Error().Str("id", task.Id).Msgf("Failed git repo update for app %s: %s", task.App, err)
		return err
	}

	return nil
}

func (gitUpdater *GitUpdater) updateGitRepo(app *models.Application, task *models.Task, gitopsRepo *models.GitopsRepo) error {
	err := app.UpdateGitImageTag(task, gitopsRepo)
	if err != nil {
		log.Error().Str("id", task.Id).Msgf("Failed to update git repo. Error: %s", err.Error())
		return err
	}
	return nil
}

// ArgoStatusUpdater handles the monitoring and updating of ArgoCD application deployments
type ArgoStatusUpdater struct {
	monitor    *DeploymentMonitor
	gitUpdater *GitUpdater
	notifier   *notifications.Notifier
}

// Init initializes the ArgoStatusUpdater with the provided configuration
func (updater *ArgoStatusUpdater) Init(argo Argo, retryAttempts uint, retryDelay time.Duration, registryProxyUrl string, repoCachePath string, acceptSuspended bool, webhookConfig *config.WebhookConfig, locker lock.Locker) error {
	retryOptions := []retry.Option{
		retry.DelayType(retry.FixedDelay),
		retry.Attempts(retryAttempts),
		retry.Delay(retryDelay),
		retry.LastErrorOnly(true),
	}

	updater.monitor = NewDeploymentMonitor(argo, registryProxyUrl, retryOptions, acceptSuspended)
	updater.gitUpdater = NewGitUpdater(locker, repoCachePath)

	if webhookConfig == nil || !webhookConfig.Enabled {
		return nil
	}

	httpClient := &http.Client{
		Timeout: 15 * time.Second,
	}

	webhookStrategy, err := notifications.NewWebhookStrategy(webhookConfig, httpClient)
	if err != nil {
		return err
	}

	updater.notifier = notifications.NewNotifier(webhookStrategy)
	return nil
}

// WaitForRollout is the main entry point for tracking an application deployment
// It monitors the application until it reaches a final state (deployed or failed)
func (updater *ArgoStatusUpdater) WaitForRollout(task models.Task) {
	updater.monitor.BeginTracking()
	defer updater.monitor.EndTracking()

	// notify about the deployment start
	sendNotification(task, updater.notifier)

	// wait for application to get into deployed status or timeout
	application, err := updater.waitForApplicationDeployment(task)

	if err != nil {
		// handle application failure
		updater.monitor.HandleArgoAPIFailure(task, err)
	} else {
		// process deployment result
		updater.monitor.ProcessDeploymentResult(&task, application)
	}

	// send webhook event about the deployment result
	sendNotification(task, updater.notifier)
}

// waitForApplicationDeployment coordinates the deployment monitoring process
// It checks initial status, updates the git repo if needed, and waits for rollout
func (updater *ArgoStatusUpdater) waitForApplicationDeployment(task models.Task) (*models.Application, error) {
	// Fetch initial app state
	app, err := updater.monitor.FetchApplication(task.App)
	if err != nil {
		return nil, err
	}

	// Save the initial application status
	if err := updater.monitor.StoreInitialAppStatus(&task, app); err != nil {
		return nil, err
	}

	// Handle git repo update if needed
	if err := updater.gitUpdater.UpdateIfNeeded(app, task); err != nil {
		return nil, err
	}

	// Wait for rollout completion
	return updater.monitor.WaitRollout(task)
}

// handleApplicationFetchError ensures we don't retry for not found errors
func handleApplicationFetchError(task models.Task, err error) error {
	if task.IsAppNotFoundError(err) {
		return retry.Unrecoverable(err)
	}
	log.Debug().Str("id", task.Id).Msgf("Failed fetching application status. Error: %s", err.Error())
	return err
}

// checkRolloutStatus checks if the application completed rollout successfully
func checkRolloutStatus(task models.Task, application *models.Application, registryProxyUrl string, acceptSuspended bool) error {
	status := application.GetRolloutStatus(task.ListImages(), registryProxyUrl, acceptSuspended)

	switch status {
	case models.ArgoRolloutAppDegraded:
		log.Debug().Str("id", task.Id).Msgf("Application is degraded")
		hash := helpers.GenerateHash(strings.Join(application.Status.Summary.Images, ","))
		if !bytes.Equal(task.SavedAppStatus.ImagesHash, hash) {
			return retry.Unrecoverable(errors.New("application has degraded"))
		}
	case models.ArgoRolloutAppSuccess:
		log.Debug().Str("id", task.Id).Msgf("Application rollout finished")
		return nil
	default:
		log.Debug().Str("id", task.Id).Msgf("Application status is not final. Status received \"%s\"", status)
	}
	return errors.New("force retry")
}

// determineFailureStatus converts API errors into appropriate status messages
func determineFailureStatus(task models.Task, err error) string {
	if task.IsAppNotFoundError(err) {
		return models.StatusAppNotFoundMessage
	}
	if strings.Contains(err.Error(), argoUnavailableErrorMessage) {
		return models.StatusAborted
	}
	return models.StatusFailedMessage
}

// sendNotification sends the task update through the configured notifier if available.
func sendNotification(task models.Task, notifier *notifications.Notifier) {
	if notifier == nil {
		return
	}

	if err := notifier.Send(task); err != nil {
		log.Error().Str("id", task.Id).Msgf("Failed to dispatch notification. Error: %s", err.Error())
	}
}
