package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/avast/retry-go/v4"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/internal/helpers"
	"github.com/shini4i/argo-watcher/internal/models"
)

type ArgoStatusUpdater struct {
	argo       Argo
	retryAttempts uint
}

func (updater *ArgoStatusUpdater) Init(argo Argo, retryAttempts uint) {
	updater.argo = argo
	updater.retryAttempts = retryAttempts
}

func (updater *ArgoStatusUpdater) WaitForRollout(task models.Task) {
	// continuously check for application status change
	status, err := updater.checkWithRetry(task)

	// application synced successfully
	if status == ArgoAppSuccess {
		updater.handleDeploymentSuccess(task)
		return
	}

	// we had some unexpected error with ArgoCD API
	if status == ArgoAppFailed {
		updater.handleArgoAPIFailure(task, err)
		return
	}

	// fetch application details
	app, err := updater.argo.api.GetApplication(task.App)

	// define default message
	const defaultErrorMessage string = "could not retrieve details"
	// handle application sync failure
	switch status {
	// not all images were deployed to the application
	case ArgoAppNotAvailable:
		// show list of missing images
		var message string
		// define details
		if err != nil {
			message = defaultErrorMessage
		} else {
			message = "List of current images (last app check):\n"
			message += "\t" + strings.Join(app.Status.Summary.Images, "\n\t") + "\n\n"
			message += "List of expected images:\n"
			message += "\t" + strings.Join(task.ListImages(), "\n\t")
		}
		// handle error
		updater.handleAppNotAvailable(task, errors.New(message))
	// application sync status wasn't valid
	case ArgoAppNotSynced:
		// display sync status and last sync message
		var message string
		// define details
		if err != nil {
			message = defaultErrorMessage
		} else {
			message = "App status \"" + app.Status.OperationState.Phase + "\"\n"
			message += "App message \"" + app.Status.OperationState.Message + "\"\n"
			message += "Resources:\n"
			message += "\t" + strings.Join(app.ListSyncResultResources(), "\n\t")
		}
		// handle error
		updater.handleAppOutOfSync(task, errors.New(message))
	// application is not in a healthy status
	case ArgoAppNotHealthy:
		// display current health of pods
		var message string
		// define details
		if err != nil {
			message = defaultErrorMessage
		} else {
			message = "App sync status \"" + app.Status.Sync.Status + "\"\n"
			message += "App health status \"" + app.Status.Health.Status + "\"\n"
			message += "Resources:\n"
			message += "\t" + strings.Join(app.ListUnhealthyResources(), "\n\t")
		}
		// handle error
		updater.handleAppNotHealthy(task, errors.New(message))
	// handle unexpected status
	default:
		updater.handleDeploymentUnexpectedStatus(task, fmt.Errorf("received unexpected status \"%d\"", status))
	}
}

func (updater *ArgoStatusUpdater) checkWithRetry(task models.Task) (int, error) {
	var lastStatus int

	err := retry.Do(
		func() error {
			app, err := updater.argo.api.GetApplication(task.App)

			if err != nil {
				log.Warn().Str("app", task.App).Msg(err.Error())
				lastStatus = ArgoAppFailed
				return err
			}

			for _, image := range task.Images {
				expected := fmt.Sprintf("%s:%s", image.Image, image.Tag)
				if !helpers.Contains(app.Status.Summary.Images, expected) {
					log.Debug().Str("app", task.App).Str("id", task.Id).Msgf("%s is not available yet", expected)
					lastStatus = ArgoAppNotAvailable
					return errorArgoPlannedRetry
				}
			}

			if app.Status.Sync.Status != "Synced" {
				log.Debug().Str("id", task.Id).Msgf("%s is not synced yet", task.App)
				lastStatus = ArgoAppNotSynced
				return errorArgoPlannedRetry
			}

			if app.Status.Health.Status != "Healthy" {
				log.Debug().Str("id", task.Id).Msgf("%s is not healthy yet", task.App)
				lastStatus = ArgoAppNotHealthy
				return errorArgoPlannedRetry
			}

			lastStatus = ArgoAppSuccess
			return nil
		},
		retry.DelayType(retry.FixedDelay),
		retry.Delay(argoSyncRetryDelay),
		retry.Attempts(updater.retryAttempts),
		retry.RetryIf(func(err error) bool {
			return errors.Is(err, errorArgoPlannedRetry)
		}),
		retry.LastErrorOnly(true),
	)

	return lastStatus, err
}


func (updater *ArgoStatusUpdater) handleArgoAPIFailure(task models.Task, err error) {
	// notify user that app wasn't found
	appNotFoundError := fmt.Sprintf("applications.argoproj.io \"%s\" not found", task.App)
	if strings.Contains(err.Error(), appNotFoundError) {
		updater.handleAppNotFound(task, err)
		return
	}
	// notify user that ArgoCD API isn't available
	if strings.Contains(err.Error(), argoUnavailableErrorMessage) {
		updater.handleArgoUnavailable(task, err)
		return
	}

	// notify of unexpected error
	updater.handleDeploymentFailed(task, err)
}

func (updater *ArgoStatusUpdater) handleAppNotFound(task models.Task, err error) {
	log.Info().Str("id", task.Id).Msgf("Application %s does not exist.", task.App)
	reason := fmt.Sprintf(ArgoAPIErrorTemplate, err.Error())
	updater.argo.state.SetTaskStatus(task.Id, models.StatusAppNotFoundMessage, reason)
}

func (updater *ArgoStatusUpdater) handleArgoUnavailable(task models.Task, err error) {
	log.Error().Str("id", task.Id).Msg("ArgoCD is not available. Aborting.")
	reason := fmt.Sprintf(ArgoAPIErrorTemplate, err.Error())
	updater.argo.state.SetTaskStatus(task.Id, "aborted", reason)
}

func (updater *ArgoStatusUpdater) handleDeploymentFailed(task models.Task, err error) {
	log.Warn().Str("id", task.Id).Msgf("Deployment failed. Aborting with error: %s", err)
	updater.argo.metrics.AddFailedDeployment(task.App)
	reason := fmt.Sprintf(ArgoAPIErrorTemplate, err.Error())
	updater.argo.state.SetTaskStatus(task.Id, models.StatusFailedMessage, reason)
}

func (updater *ArgoStatusUpdater) handleDeploymentSuccess(task models.Task) {
	log.Info().Str("id", task.Id).Msg("App is running on the excepted version.")
	updater.argo.metrics.ResetFailedDeployment(task.App)
	updater.argo.state.SetTaskStatus(task.Id, "deployed", "")
}

func (updater *ArgoStatusUpdater) handleAppNotAvailable(task models.Task, err error) {
	log.Warn().Str("id", task.Id).Msgf("Deployment failed. Application not available\n%s", err.Error())
	updater.argo.metrics.AddFailedDeployment(task.App)
	reason := fmt.Sprintf("Application not available\n\n%s", err.Error())
	updater.argo.state.SetTaskStatus(task.Id, models.StatusFailedMessage, reason)
}

func (updater *ArgoStatusUpdater) handleAppNotHealthy(task models.Task, err error) {
	log.Warn().Str("id", task.Id).Msgf("Deployment failed. Application not healthy\n%s", err.Error())
	updater.argo.metrics.AddFailedDeployment(task.App)
	reason := fmt.Sprintf("Application not healthy\n\n%s", err.Error())
	updater.argo.state.SetTaskStatus(task.Id, models.StatusFailedMessage, reason)
}

func (updater *ArgoStatusUpdater) handleAppOutOfSync(task models.Task, err error) {
	log.Warn().Str("id", task.Id).Msgf("Deployment failed. Application out of sync\n%s", err.Error())
	updater.argo.metrics.AddFailedDeployment(task.App)
	reason := fmt.Sprintf("Application out of sync\n\n%s", err.Error())
	updater.argo.state.SetTaskStatus(task.Id, models.StatusFailedMessage, reason)
}

func (updater *ArgoStatusUpdater) handleDeploymentUnexpectedStatus(task models.Task, err error) {
	log.Error().Str("id", task.Id).Msg("Deployment timed out with unexpected status. Aborting.")
	log.Error().Str("id", task.Id).Msgf("Deployment error\n%s", err.Error())
	updater.argo.metrics.AddFailedDeployment(task.App)
	reason := fmt.Sprintf("Deployment timeout\n\n%s", err.Error())
	updater.argo.state.SetTaskStatus(task.Id, models.StatusFailedMessage, reason)
}