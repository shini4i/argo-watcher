package argocd

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/notifications"

	"github.com/shini4i/argo-watcher/internal/helpers"

	"github.com/avast/retry-go/v4"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/internal/models"
)

const failedToUpdateTaskStatusTemplate string = "Failed to change task status: %s"

// MutexMap provides a thread-safe way to get a mutex for a specific key
type MutexMap struct {
	m sync.Map
}

// Get returns a mutex for the given key. If the key doesn't exist, a new mutex is created.
func (mm *MutexMap) Get(key string) *sync.Mutex {
	log.Debug().Msgf("acquiring mutex for %s app", key)
	m, _ := mm.m.LoadOrStore(key, &sync.Mutex{})
	return m.(*sync.Mutex) // nolint:forcetypeassert // type assertion is guaranteed to be correct
}

// ArgoStatusUpdater handles the monitoring and updating of ArgoCD application deployments
type ArgoStatusUpdater struct {
	argo             Argo
	registryProxyUrl string
	retryOptions     []retry.Option
	mutex            MutexMap
	acceptSuspended  bool
	webhookService   *notifications.WebhookService
}

// Init initializes the ArgoStatusUpdater with the provided configuration
func (updater *ArgoStatusUpdater) Init(argo Argo, retryAttempts uint, retryDelay time.Duration, registryProxyUrl string, acceptSuspended bool, webhookConfig *config.WebhookConfig) error {
	var err error

	updater.argo = argo
	updater.registryProxyUrl = registryProxyUrl
	updater.retryOptions = []retry.Option{
		retry.DelayType(retry.FixedDelay),
		retry.Attempts(retryAttempts),
		retry.Delay(retryDelay),
		retry.LastErrorOnly(true),
	}
	updater.acceptSuspended = acceptSuspended

	if !webhookConfig.Enabled {
		return nil
	}

	httpClient := &http.Client{
		Timeout: 15 * time.Second,
	}

	webhookService, err := notifications.NewWebhookService(webhookConfig, httpClient)
	if err != nil {
		return err
	}

	updater.webhookService = webhookService
	return nil
}

// collectInitialAppStatus fetches and stores the initial application status
// This is used to detect changes during the deployment process
func (updater *ArgoStatusUpdater) collectInitialAppStatus(task *models.Task) error {
	application, err := updater.argo.api.GetApplication(task.App)
	if err != nil {
		return err
	}

	status := application.GetRolloutStatus(task.ListImages(), updater.registryProxyUrl, updater.acceptSuspended)

	// sort images to avoid hash mismatch
	slices.Sort(application.Status.Summary.Images)

	task.SavedAppStatus = models.SavedAppStatus{
		Status:     status,
		ImagesHash: helpers.GenerateHash(strings.Join(application.Status.Summary.Images, ",")),
	}

	return nil
}

// WaitForRollout is the main entry point for tracking an application deployment
// It monitors the application until it reaches a final state (deployed or failed)
func (updater *ArgoStatusUpdater) WaitForRollout(task models.Task) {
	// increment in progress task counter
	updater.argo.metrics.AddInProgressTask()
	defer updater.argo.metrics.RemoveInProgressTask()

	// notify about the deployment start
	sendWebhookEvent(task, updater.webhookService)

	// wait for application to get into deployed status or timeout
	application, err := updater.waitForApplicationDeployment(task)

	if err != nil {
		// handle application failure
		updater.handleArgoAPIFailure(task, err)
	} else {
		// process deployment result
		updater.processDeploymentResult(&task, application)
	}

	// send webhook event about the deployment result
	sendWebhookEvent(task, updater.webhookService)
}

// processDeploymentResult determines if the deployment was successful and
// updates the appropriate status and metrics
func (updater *ArgoStatusUpdater) processDeploymentResult(task *models.Task, application *models.Application) {
	status := application.GetRolloutStatus(task.ListImages(), updater.registryProxyUrl, updater.acceptSuspended)
	if application.IsFireAndForgetModeActive() {
		status = models.ArgoRolloutAppSuccess
	}

	if status == models.ArgoRolloutAppSuccess {
		updater.handleDeploymentSuccess(task)
	} else {
		updater.handleDeploymentFailure(task, status, application)
	}
}

// handleDeploymentSuccess processes a successful deployment by updating metrics and status
func (updater *ArgoStatusUpdater) handleDeploymentSuccess(task *models.Task) {
	log.Info().Str("id", task.Id).Msg("App is running on the expected version.")
	updater.argo.metrics.ResetFailedDeployment(task.App)
	if err := updater.argo.State.SetTaskStatus(task.Id, models.StatusDeployedMessage, ""); err != nil {
		log.Error().Str("id", task.Id).Msgf(failedToUpdateTaskStatusTemplate, err)
	}
	task.Status = models.StatusDeployedMessage
}

// handleDeploymentFailure processes a failed deployment with detailed error information
func (updater *ArgoStatusUpdater) handleDeploymentFailure(task *models.Task, status string, application *models.Application) {
	log.Info().Str("id", task.Id).Msg("App deployment failed.")
	updater.argo.metrics.AddFailedDeployment(task.App)
	reason := fmt.Sprintf(
		"Application deployment failed. Rollout status \"%s\"\n\n%s",
		status,
		application.GetRolloutMessage(status, task.ListImages()),
	)
	if err := updater.argo.State.SetTaskStatus(task.Id, models.StatusFailedMessage, reason); err != nil {
		log.Error().Str("id", task.Id).Msgf(failedToUpdateTaskStatusTemplate, err)
	}
	task.Status = models.StatusFailedMessage
}

// waitForApplicationDeployment coordinates the deployment monitoring process
// It checks initial status, updates the git repo if needed, and waits for rollout
func (updater *ArgoStatusUpdater) waitForApplicationDeployment(task models.Task) (*models.Application, error) {
	// Fetch initial app state
	app, err := updater.argo.api.GetApplication(task.App)
	if err != nil {
		return nil, err
	}

	// Save the initial application status
	if err := updater.collectInitialAppStatus(&task); err != nil {
		return nil, err
	}

	// Handle git repo update if needed
	if err := updater.handleGitRepoUpdateIfNeeded(app, task); err != nil {
		return nil, err
	}

	// Wait for rollout completion
	return updater.waitRollout(task)
}

// handleGitRepoUpdateIfNeeded updates the git repository if the application
// is managed by the watcher and has valid credentials
func (updater *ArgoStatusUpdater) handleGitRepoUpdateIfNeeded(app *models.Application, task models.Task) error {
	// This mutex is used only to avoid concurrent updates of the same application
	mutex := updater.mutex.Get(task.App)
	mutex.Lock()
	defer mutex.Unlock()

	// Skip if not managed by watcher or not validated
	if !app.IsManagedByWatcher() || !task.Validated {
		log.Debug().Str("id", task.Id).Msg("Skipping git repo update: Application does not have the necessary annotations or token is missing.")
		return nil
	}

	// Application is managed by watcher, update git repo
	log.Debug().Str("id", task.Id).Msg("Application managed by watcher. Initiating git repo update.")
	return updater.updateGitRepo(app, task)
}

// updateGitRepo attempts to update the git repository with retries
func (updater *ArgoStatusUpdater) updateGitRepo(app *models.Application, task models.Task) error {
	err := retry.Do(
		func() error {
			return app.UpdateGitImageTag(&task)
		},
		retry.DelayType(retry.BackOffDelay),
		retry.Attempts(5),
		retry.OnRetry(func(n uint, err error) {
			log.Warn().Str("id", task.Id).Msgf("Failed to update git repo. Error: %s, retrying...", err.Error())
		}),
		retry.LastErrorOnly(true),
	)

	if err != nil {
		log.Error().Str("id", task.Id).Msgf("Failed to update git repo. Error: %s", err.Error())
		return err
	}

	return nil
}

// waitRollout polls the application status until it reaches a final state or times out
func (updater *ArgoStatusUpdater) waitRollout(task models.Task) (*models.Application, error) {
	var application *models.Application
	var err error

	retryOptions := updater.configureRetryOptions(task)
	log.Debug().Str("id", task.Id).Msg("Waiting for rollout")

	_ = retry.Do(func() error {
		application, err = updater.argo.api.GetApplication(task.App)
		if err != nil {
			return handleApplicationFetchError(task, err)
		}

		// Early return for fire and forget mode
		if application.IsFireAndForgetModeActive() {
			log.Debug().Str("id", task.Id).Msg("Fire and forget mode is active, skipping checks...")
			return nil
		}

		return checkRolloutStatus(task, application, updater.registryProxyUrl, updater.acceptSuspended)
	}, retryOptions...)

	return application, err
}

// configureRetryOptions sets up retry behavior based on task timeout
func (updater *ArgoStatusUpdater) configureRetryOptions(task models.Task) []retry.Option {
	retryOptions := updater.retryOptions
	if task.Timeout <= 0 {
		return retryOptions
	}

	log.Debug().Str("id", task.Id).Msgf("Overriding task timeout to %ds", task.Timeout)
	calculatedAttempts := task.Timeout/15 + 1

	if calculatedAttempts < 0 {
		log.Error().Msg("Calculated attempts resulted in a negative number, defaulting to 15 attempts.")
		calculatedAttempts = 15
	}

	return append(retryOptions, retry.Attempts(uint(calculatedAttempts))) // #nosec G115
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

// handleArgoAPIFailure processes API errors and updates task status accordingly
func (updater *ArgoStatusUpdater) handleArgoAPIFailure(task models.Task, err error) {
	updater.argo.metrics.AddFailedDeployment(task.App)
	finalStatus := determineFailureStatus(task, err)
	reason := fmt.Sprintf(ArgoAPIErrorTemplate, err.Error())
	log.Warn().Str("id", task.Id).Msgf("Deployment failed with status \"%s\". Aborting with error: %s", finalStatus, reason)

	if err := updater.argo.State.SetTaskStatus(task.Id, finalStatus, reason); err != nil {
		log.Error().Str("id", task.Id).Msgf(failedToUpdateTaskStatusTemplate, err)
	}
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

// sendWebhookEvent sends a notification about deployment status if webhooks are enabled
func sendWebhookEvent(task models.Task, webhookService *notifications.WebhookService) {
	if webhookService.Enabled {
		if err := webhookService.SendWebhook(task); err != nil {
			log.Error().Str("id", task.Id).Msgf("Failed to send webhook. Error: %s", err.Error())
		}
	}
}
