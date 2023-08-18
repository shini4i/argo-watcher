package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/internal/models"
)

const failedToUpdateTaskStatusTemplate string = "Failed to change task status: %s"

type ArgoStatusUpdater struct {
	argo             Argo
	registryProxyUrl string
	retryOptions     []retry.Option
}

func (updater *ArgoStatusUpdater) Init(argo Argo, retryAttempts uint, retryDelay time.Duration, registryProxyUrl string) {
	updater.argo = argo
	updater.registryProxyUrl = registryProxyUrl
	updater.retryOptions = []retry.Option{
		retry.DelayType(retry.FixedDelay),
		retry.Attempts(retryAttempts),
		retry.Delay(retryDelay),
		retry.LastErrorOnly(true),
	}
}

func (updater *ArgoStatusUpdater) WaitForRollout(task models.Task) {
	// wait for application to get into deployed status or timeout
	application, err := updater.waitForApplicationDeployment(task)

	// handle application failure
	if err != nil {
		// deployment failed
		updater.argo.metrics.AddFailedDeployment(task.App)
		// update task status regarding failure
		updater.handleArgoAPIFailure(task, err)
		return
	}

	// get application status
	status := application.GetRolloutStatus(task.ListImages(), updater.registryProxyUrl)
	if application.IsFinalRolloutStatus(status) {
		log.Info().Str("id", task.Id).Msg("App is running on the excepted version.")
		// deployment success
		updater.argo.metrics.ResetFailedDeployment(task.App)
		// update task status
		errStatusChange := updater.argo.state.SetTaskStatus(task.Id, models.StatusDeployedMessage, "")
		if errStatusChange != nil {
			log.Error().Str("id", task.Id).Msgf(failedToUpdateTaskStatusTemplate, errStatusChange)
		}
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
		errStatusChange := updater.argo.state.SetTaskStatus(task.Id, models.StatusFailedMessage, reason)
		if errStatusChange != nil {
			log.Error().Str("id", task.Id).Msgf(failedToUpdateTaskStatusTemplate, errStatusChange)
		}
	}
}

func (updater *ArgoStatusUpdater) waitForApplicationDeployment(task models.Task) (*models.Application, error) {
	var application *models.Application
	var err error

	app, err := updater.argo.api.GetApplication(task.App)
	if app.IsManagedByWatcher() {
		log.Debug().Str("id", task.Id).Msg("Application is managed by watcher. Updating git repository.")
		app.UpdateGitImageTag(&task)
	} else {
		log.Debug().Str("id", task.Id).Msg("Application is not managed by watcher. Skipping git repository update.")
	}

	// wait for application to get into deployed status or timeout
	log.Debug().Str("id", task.Id).Msg("Waiting for rollout")
	_ = retry.Do(func() error {
		application, err = updater.argo.api.GetApplication(task.App)
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
		// print application debug here
		status := application.GetRolloutStatus(task.ListImages(), updater.registryProxyUrl)
		if !application.IsFinalRolloutStatus(status) {
			// print status debug here
			log.Debug().Str("id", task.Id).Msgf("Application status is not final. Status received \"%s\"", status)
			return errors.New("force retry")
		}
		// all good
		log.Debug().Str("id", task.Id).Msgf("Application rollout finished")
		return nil
	}, updater.retryOptions...)

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

	errStatusChange := updater.argo.state.SetTaskStatus(task.Id, apiFailureStatus, reason)
	if errStatusChange != nil {
		log.Error().Str("id", task.Id).Msgf(failedToUpdateTaskStatusTemplate, errStatusChange)
	}
}
