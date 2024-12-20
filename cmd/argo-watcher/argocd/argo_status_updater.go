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

const failedToUpdateTaskStatusTemplate string = "Failed to change task status: %s"

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
	webhookService   *notifications.WebhookService
}

func (updater *ArgoStatusUpdater) Init(argo Argo, retryAttempts uint, retryDelay time.Duration, registryProxyUrl string, acceptSuspended bool, webhookConfig *config.WebhookConfig) {
	updater.argo = argo
	updater.registryProxyUrl = registryProxyUrl
	updater.retryOptions = []retry.Option{
		retry.DelayType(retry.FixedDelay),
		retry.Attempts(retryAttempts),
		retry.Delay(retryDelay),
		retry.LastErrorOnly(true),
	}
	updater.acceptSuspended = acceptSuspended
	updater.webhookService = notifications.NewWebhookService(webhookConfig)
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

	// handle application failure
	if err != nil {
		// deployment failed
		updater.argo.metrics.AddFailedDeployment(task.App)
		// update task status regarding failure
		updater.handleArgoAPIFailure(task, err)
		// decrement in progress task counter
		updater.argo.metrics.RemoveInProgressTask()
		return
	}

	// get application status
	status := application.GetRolloutStatus(task.ListImages(), updater.registryProxyUrl, updater.acceptSuspended)
	if application.IsFireAndForgetModeActive() {
		status = models.ArgoRolloutAppSuccess
	}
	if status == models.ArgoRolloutAppSuccess {
		log.Info().Str("id", task.Id).Msg("App is running on the expected version.")
		// deployment success
		updater.argo.metrics.ResetFailedDeployment(task.App)
		// update task status
		if err := updater.argo.State.SetTaskStatus(task.Id, models.StatusDeployedMessage, ""); err != nil {
			log.Error().Str("id", task.Id).Msgf(failedToUpdateTaskStatusTemplate, err)
		}
		// setting task status to handle further notifications
		task.Status = models.StatusDeployedMessage
	} else {
		log.Info().Str("id", task.Id).Msg("App deployment failed.")
		// deployment failed
		updater.argo.metrics.AddFailedDeployment(task.App)
		// generate failure reason
		reason := fmt.Sprintf(
			"Application deployment failed. Rollout status \"%s\"\n\n%s",
			status,
			application.GetRolloutMessage(status, task.ListImages()),
		)
		// update task status
		if err := updater.argo.State.SetTaskStatus(task.Id, models.StatusFailedMessage, reason); err != nil {
			log.Error().Str("id", task.Id).Msgf(failedToUpdateTaskStatusTemplate, err)
		}
		// setting task status to handle further notifications
		task.Status = models.StatusFailedMessage
	}

	// decrement in progress task counter
	updater.argo.metrics.RemoveInProgressTask()

	// send webhook event about the deployment result
	sendWebhookEvent(task, updater.webhookService)
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

	// This mutex is used only to avoid concurrent updates of the same application.
	mutex := updater.mutex.Get(task.App)

	// Locking the mutex here to unlock within the next if block without duplicating the code,
	// avoiding defer to unlock before the function's end. This approach may be revised later
	mutex.Lock()

	if app.IsManagedByWatcher() && task.Validated {
		err = updater.updateGitRepo(app, task, mutex)
	} else {
		mutex.Unlock()
		log.Debug().Str("id", task.Id).Msg("Skipping git repo update: Application does not have the necessary annotations or token is missing.")
	}

	if err != nil {
		return nil, err
	}

	// wait for application to get into deployed status or timeout
	application, err = updater.waitRollout(task)

	// return application and latest error
	return application, err
}

func (updater *ArgoStatusUpdater) updateGitRepo(app *models.Application, task models.Task, mutex *sync.Mutex) error {
	log.Debug().Str("id", task.Id).Msg("Application managed by watcher. Initiating git repo update.")

	// simplest way to deal with potential git conflicts
	// need to be replaced with a more sophisticated solution after PoC
	err := retry.Do(
		func() error {
			if err := app.UpdateGitImageTag(&task); err != nil {
				return err
			}
			return nil
		},
		retry.DelayType(retry.BackOffDelay),
		retry.Attempts(5),
		retry.OnRetry(func(n uint, err error) {
			log.Warn().Str("id", task.Id).Msgf("Failed to update git repo. Error: %s, retrying...", err.Error())
		}),
		retry.LastErrorOnly(true),
	)

	mutex.Unlock()
	if err != nil {
		log.Error().Str("id", task.Id).Msgf("Failed to update git repo. Error: %s", err.Error())
		return err
	}

	return nil
}

func (updater *ArgoStatusUpdater) waitRollout(task models.Task) (*models.Application, error) {
	var application *models.Application
	var err error

	retryOptions := updater.retryOptions

	if task.Timeout > 0 {
		log.Debug().Str("id", task.Id).Msgf("Overriding task timeout to %ds", task.Timeout)
		calculatedAttempts := task.Timeout/15 + 1

		if calculatedAttempts < 0 {
			log.Error().Msg("Calculated attempts resulted in a negative number, defaulting to 15 attempts.")
			calculatedAttempts = 15
		}
		retryOptions = append(retryOptions, retry.Attempts(uint(calculatedAttempts))) // #nosec G115
	}

	log.Debug().Str("id", task.Id).Msg("Waiting for rollout")
	_ = retry.Do(func() error {
		application, err = updater.argo.api.GetApplication(task.App)

		if application.IsFireAndForgetModeActive() {
			log.Debug().Str("id", task.Id).Msg("Fire and forge mode is active, skipping checks...")
			return nil
		}

		if err != nil {
			// check if ArgoCD didn't have the app
			if task.IsAppNotFoundError(err) {
				// no need to retry in such cases
				return retry.Unrecoverable(err)
			}
			// print application api failure here
			log.Debug().Str("id", task.Id).Msgf("Failed fetching application status. Error: %s", err.Error())
			return err
		}

		status := application.GetRolloutStatus(task.ListImages(), updater.registryProxyUrl, updater.acceptSuspended)

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
	}, retryOptions...)

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
