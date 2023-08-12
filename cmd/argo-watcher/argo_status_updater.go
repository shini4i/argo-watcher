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

	// wait for application to get into deployed status or timeout
	log.Debug().Str("id", task.Id).Msg("Waiting for rollout")
	retry.Do(func() error {
		application, err = updater.argo.api.GetApplication(task.App)
		if err != nil {
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
	errStatusChange := updater.argo.state.SetTaskStatus(task.Id, models.StatusAppNotFoundMessage, reason)
	if errStatusChange != nil {
		log.Error().Str("id", task.Id).Msgf(failedToUpdateTaskStatusTemplate, errStatusChange)
	}
}

func (updater *ArgoStatusUpdater) handleArgoUnavailable(task models.Task, err error) {
	log.Error().Str("id", task.Id).Msg("ArgoCD is not available. Aborting.")
	reason := fmt.Sprintf(ArgoAPIErrorTemplate, err.Error())
	errStatusChange := updater.argo.state.SetTaskStatus(task.Id, models.StatusAborted, reason)
	if errStatusChange != nil {
		log.Error().Str("id", task.Id).Msgf(failedToUpdateTaskStatusTemplate, errStatusChange)
	}
}

func (updater *ArgoStatusUpdater) handleDeploymentFailed(task models.Task, err error) {
	log.Warn().Str("id", task.Id).Msgf("Deployment failed. Aborting with error: %s", err)
	updater.argo.metrics.AddFailedDeployment(task.App)
	reason := fmt.Sprintf(ArgoAPIErrorTemplate, err.Error())
	errStatusChange := updater.argo.state.SetTaskStatus(task.Id, models.StatusFailedMessage, reason)
	if errStatusChange != nil {
		log.Error().Str("id", task.Id).Msgf(failedToUpdateTaskStatusTemplate, errStatusChange)
	}
}
