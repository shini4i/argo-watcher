package argocd

import (
	"bytes"
	"errors"
	"fmt"
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

const (
	failedToUpdateTaskStatusTemplate string = "Failed to change task status: %s"
)

// MutexMap provides thread-safe access to application-specific mutexes
type MutexMap struct {
	m sync.Map
}

func (mm *MutexMap) Get(key string) *sync.Mutex {
	log.Debug().Msgf("acquiring mutex for %s app", key)
	m, _ := mm.m.LoadOrStore(key, &sync.Mutex{})
	return m.(*sync.Mutex) // nolint:forcetypeassert // type assertion is guaranteed to be correct
}

type ArgoStatusUpdater struct {
	argo             Argo
	registryProxyUrl string
	retryOptions     []retry.Option
	mutex            MutexMap
	acceptSuspended  bool
	rolloutMonitor   RolloutMonitor
	gitOps           GitOperations
	notifier         StatusNotifier
	webhookService   *notifications.WebhookService
}

func (updater *ArgoStatusUpdater) Init(argo Argo, retryAttempts uint, retryDelay time.Duration, registryProxyUrl string, acceptSuspended bool, webhookConfig *config.WebhookConfig) {
	updater.argo = argo
	updater.registryProxyUrl = registryProxyUrl
	updater.acceptSuspended = acceptSuspended
	updater.webhookService = notifications.NewWebhookService(webhookConfig)
	updater.retryOptions = []retry.Option{
		retry.DelayType(retry.FixedDelay),
		retry.Attempts(retryAttempts),
		retry.Delay(retryDelay),
		retry.LastErrorOnly(true),
	}
	updater.rolloutMonitor = NewDefaultRolloutMonitor(registryProxyUrl, acceptSuspended)
	updater.gitOps = NewDefaultGitOperations()
	updater.notifier = NewDefaultStatusNotifier(webhookConfig)
}

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

func (updater *ArgoStatusUpdater) WaitForRollout(task models.Task) {
	// increment in progress task counter
	updater.argo.metrics.AddInProgressTask()

	// notify about the deployment start
	sendWebhookEvent(task, updater.webhookService)

	// wait for application to get into deployed status or timeout
	application, err := updater.waitForApplicationDeployment(task)

	updater.handleDeploymentResult(application, err, task)
}

func (updater *ArgoStatusUpdater) waitForApplicationDeployment(task models.Task) (*models.Application, error) {
	var application *models.Application
	var err error

	app, err := updater.argo.api.GetApplication(task.App)
	if err != nil {
		return nil, err
	}

	// save the initial application status to compare with the final one
	if err := updater.collectInitialAppStatus(&task); err != nil {
		return nil, err
	}

	if app.IsManagedByWatcher() && task.Validated {
		err = updater.gitOps.UpdateImageTags(app, &task)
	} else {
		log.Debug().Str("id", task.Id).Msg("Skipping git repository update: Application does not have the necessary annotations or token is missing.")
	}

	if err != nil {
		return nil, err
	}

	// wait for application to get into deployed status or timeout
	application, err = updater.waitRollout(task)

	// return application and latest error
	return application, err
}

func (updater *ArgoStatusUpdater) waitRollout(task models.Task) (*models.Application, error) {
	var application *models.Application
	var err error
	var retryOpts []retry.Option

	// Copy base retry options
	retryOpts = append(retryOpts, updater.retryOptions...)

	if task.Timeout > 0 {
		log.Debug().Str("id", task.Id).Msgf("Overriding task timeout to %ds", task.Timeout)
		calculatedAttempts := task.Timeout/15 + 1

		if calculatedAttempts < 0 {
			log.Error().Msg("Calculated attempts resulted in a negative number, defaulting to 15 attempts.")
			calculatedAttempts = 15
		}
		retryOpts = append(retryOpts, retry.Attempts(uint(calculatedAttempts)))
	}

	log.Debug().Str("id", task.Id).Msg("Waiting for rollout")
	err = retry.Do(func() error {
		application, err = updater.argo.api.GetApplication(task.App)
		if err != nil {
			return handleApplicationError(err, task)
		}

		if application.IsFireAndForgetModeActive() {
			log.Debug().Str("id", task.Id).Msg("Fire and forge mode is active, skipping checks...")
			return nil
		}

		return checkApplicationStatus(application, task, updater.registryProxyUrl, updater.acceptSuspended)
	}, retryOpts...)

	// return application and latest error
	return application, err
}

func (updater *ArgoStatusUpdater) handleArgoAPIFailure(task models.Task, err error) {
	var apiFailureStatus = models.StatusFailedMessage

	// check if ArgoCD didn't have the app
	if task.IsAppNotFoundError(err) {
		apiFailureStatus = models.StatusAppNotFoundMessage
	}
	// check if ArgoCD was unavailable
	if strings.Contains(err.Error(), argoUnavailableErrorMessage) {
		apiFailureStatus = models.StatusAborted
	}

	// write debug reason
	reason := fmt.Sprintf(ArgoAPIErrorTemplate, err.Error())
	log.Warn().Str("id", task.Id).Msgf("Deployment failed with status \"%s\". Aborting with error: %s", apiFailureStatus, reason)

	if err := updater.argo.State.SetTaskStatus(task.Id, apiFailureStatus, reason); err != nil {
		log.Error().Str("id", task.Id).Msgf(failedToUpdateTaskStatusTemplate, err)
	}
}

func sendWebhookEvent(task models.Task, webhookService *notifications.WebhookService) {
	if webhookService.Enabled {
		if err := webhookService.SendWebhook(task); err != nil {
			log.Error().Str("id", task.Id).Msgf("Failed to send webhook. Error: %s", err.Error())
		}
	}
}

func handleApplicationError(err error, task models.Task) error {
	if task.IsAppNotFoundError(err) {
		return retry.Unrecoverable(err)
	}
	log.Debug().Str("id", task.Id).Msgf("Failed fetching application status. Error: %s", err.Error())
	return err
}

func checkApplicationStatus(app *models.Application, task models.Task, registryProxyUrl string, acceptSuspended bool) error {
	status := app.GetRolloutStatus(task.ListImages(), registryProxyUrl, acceptSuspended)
	
	switch status {
	case models.ArgoRolloutAppDegraded:
		log.Debug().Str("id", task.Id).Msg("Application is degraded")
		hash := helpers.GenerateHash(strings.Join(app.Status.Summary.Images, ","))
		if !bytes.Equal(task.SavedAppStatus.ImagesHash, hash) {
			return retry.Unrecoverable(errors.New("application has degraded"))
		}
	case models.ArgoRolloutAppSuccess:
		log.Debug().Str("id", task.Id).Msg("Application rollout finished")
		return nil
	default:
		log.Debug().Str("id", task.Id).Msgf("Application status is not final. Status received \"%s\"", status)
	}
	return errors.New("force retry")
}

func (updater *ArgoStatusUpdater) handleDeploymentResult(application *models.Application, err error, task models.Task) {
	if err != nil {
		updater.argo.metrics.AddFailedDeployment(task.App)
		updater.handleArgoAPIFailure(task, err)
		updater.argo.metrics.RemoveInProgressTask()
		return
	}

	status := application.GetRolloutStatus(task.ListImages(), updater.registryProxyUrl, updater.acceptSuspended)
	if application.IsFireAndForgetModeActive() {
		status = models.ArgoRolloutAppSuccess
	}

	if status == models.ArgoRolloutAppSuccess {
		updater.handleSuccessfulDeployment(task)
	} else {
		updater.handleFailedDeployment(application, task, status)
	}

	updater.argo.metrics.RemoveInProgressTask()
	sendWebhookEvent(task, updater.webhookService)
}

func (updater *ArgoStatusUpdater) handleSuccessfulDeployment(task models.Task) {
	log.Info().Str("id", task.Id).Msg("App is running on the expected version.")
	updater.argo.metrics.ResetFailedDeployment(task.App)
	
	if err := updater.argo.State.SetTaskStatus(task.Id, models.StatusDeployedMessage, ""); err != nil {
		log.Error().Str("id", task.Id).Msgf(failedToUpdateTaskStatusTemplate, err)
	}
	task.Status = models.StatusDeployedMessage
}

func (updater *ArgoStatusUpdater) handleFailedDeployment(application *models.Application, task models.Task, status string) {
	log.Info().Str("id", task.Id).Msg("App deployment failed.")
	updater.argo.metrics.AddFailedDeployment(task.App)
	
	reason := fmt.Sprintf("Application deployment failed. Rollout status \"%s\"\n\n%s",
		status, application.GetRolloutMessage(status, task.ListImages()))
	
	if err := updater.argo.State.SetTaskStatus(task.Id, models.StatusFailedMessage, reason); err != nil {
		log.Error().Str("id", task.Id).Msgf(failedToUpdateTaskStatusTemplate, err)
	}
	task.Status = models.StatusFailedMessage
}
