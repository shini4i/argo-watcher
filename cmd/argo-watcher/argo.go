package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/state"
	"github.com/shini4i/argo-watcher/internal/helpers"
	"github.com/shini4i/argo-watcher/internal/models"
)

var (
	argoSyncRetryDelay    = 15 * time.Second
	errorArgoPlannedRetry = fmt.Errorf("planned retry")
)

const (
	ArgoAppSuccess = iota
	ArgoAppNotSynced
	ArgoAppNotAvailable
	ArgoAppNotHealthy
	ArgoAppFailed
)

const (
	ArgoAPIErrorTemplate        = "ArgoCD API Error: %s"
	argoUnavailableErrorMessage = "connect: connection refused"
)

type Argo struct {
	metrics       Metrics
	api           ArgoApi
	state         state.State
	retryAttempts uint
}

func (argo *Argo) Init(state *state.State, api *ArgoApi, metrics *Metrics, retryAttempts uint) {
	// setup dependencies
	argo.api = *api
	argo.state = *state
	argo.metrics = *metrics
	// setup configurations
	log.Debug().Msgf("Configured retry attempts per ArgoCD application status check: %d", retryAttempts)
	argo.retryAttempts = retryAttempts
}

func (argo *Argo) Check() (string, error) {
	connectionActive := argo.state.Check()
	userLoggedIn, loginError := argo.api.UserInfo()

	if !connectionActive {
		argo.metrics.argocdUnavailable.Set(1)
		return "down", errors.New(models.StatusArgoCDUnavailableMessage)
	}

	if loginError != nil {
		argo.metrics.argocdUnavailable.Set(1)
		return "down", errors.New(models.StatusArgoCDUnavailableMessage)
	}

	if userLoggedIn == nil {
		argo.metrics.argocdUnavailable.Set(1)
		return "down", errors.New(models.StatusArgoCDUnavailableMessage)
	}

	argo.metrics.argocdUnavailable.Set(0)
	return "up", nil
}

func (argo *Argo) AddTask(task models.Task) (string, error) {
	status, err := argo.Check()
	if err != nil {
		return status, errors.New(err.Error())
	}

	task.Id = uuid.New().String()

	log.Info().Str("id", task.Id).Msgf("A new task was triggered. Expecting tag %s in app %s.",
		task.Images[0].Tag,
		task.App,
	)

	argo.state.Add(task)
	argo.metrics.processedDeployments.Inc()
	
	go argo.waitForRollout(task)

	return task.Id, nil
}

func (argo *Argo) GetTasks(startTime float64, endTime float64, app string) models.TasksResponse {
	_, err := argo.Check()
	tasks := argo.state.GetTasks(startTime, endTime, app)

	if err != nil {
		return models.TasksResponse{
			Tasks: tasks,
			Error: err.Error(),
		}
	}

	return models.TasksResponse{
		Tasks: tasks,
	}
}

func (argo *Argo) checkWithRetry(task models.Task) (int, error) {
	var lastStatus int

	err := retry.Do(
		func() error {
			app, err := argo.api.ApplicationStatus(task.App)

			if err != nil {
				lastStatus = ArgoAppFailed
				return err
			}

			for _, image := range task.Images {
				expected := fmt.Sprintf("%s:%s", image.Image, image.Tag)
				if !helpers.Contains(app.Status.Summary.Images, expected) {
					log.Debug().Str("id", task.Id).Msgf("%s is not available yet", expected)
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
		retry.Attempts(argo.retryAttempts),
		retry.RetryIf(func(err error) bool {
			return errors.Is(err, errorArgoPlannedRetry)
		}),
		retry.LastErrorOnly(true),
	)

	return lastStatus, err
}

func (argo *Argo) waitForRollout(task models.Task) {
	// continuously check for application status change
	status, err := argo.checkWithRetry(task)

	// application synced successfully
	if status == ArgoAppSuccess {
		argo.handleDeploymentSuccess(task)
		return
	}

	// we had some unexpected error with ArgoCD API
	if status == ArgoAppFailed {
		argo.handleArgoAPIFailure(task, err)
		return
	}

	// fetch application details
	app, err := argo.api.ApplicationStatus(task.App)

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
		argo.handleAppNotAvailable(task, errors.New(message))
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
		argo.handleAppOutOfSync(task, errors.New(message))
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
		argo.handleAppNotHealthy(task, errors.New(message))
	// handle unexpected status
	default:
		argo.handleDeploymentUnexpectedStatus(task, fmt.Errorf("received unexpected status \"%d\"", status))
	}
}

func (argo *Argo) handleArgoAPIFailure(task models.Task, err error) {
	// notify user that app wasn't found
	appNotFoundError := fmt.Sprintf("applications.argoproj.io \"%s\" not found", task.App)
	if strings.Contains(err.Error(), appNotFoundError) {
		argo.handleAppNotFound(task, err)
		return
	}
	// notify user that ArgoCD API isn't available
	if strings.Contains(err.Error(), argoUnavailableErrorMessage) {
		argo.handleArgoUnavailable(task, err)
		return
	}

	// notify of unexpected error
	argo.handleDeploymentFailed(task, err)
}

func (argo *Argo) handleAppNotFound(task models.Task, err error) {
	log.Info().Str("id", task.Id).Msgf("Application %s does not exist.", task.App)
	reason := fmt.Sprintf(ArgoAPIErrorTemplate, err.Error())
	argo.state.SetTaskStatus(task.Id, models.StatusAppNotFoundMessage, reason)
}

func (argo *Argo) handleArgoUnavailable(task models.Task, err error) {
	log.Error().Str("id", task.Id).Msg("ArgoCD is not available. Aborting.")
	reason := fmt.Sprintf(ArgoAPIErrorTemplate, err.Error())
	argo.state.SetTaskStatus(task.Id, "aborted", reason)
}

func (argo *Argo) handleDeploymentFailed(task models.Task, err error) {
	log.Warn().Str("id", task.Id).Msgf("Deployment failed. Aborting with error: %s", err)
	argo.metrics.failedDeployment.With(prometheus.Labels{"app": task.App}).Inc()
	reason := fmt.Sprintf(ArgoAPIErrorTemplate, err.Error())
	argo.state.SetTaskStatus(task.Id, models.StatusFailedMessage, reason)
}

func (argo *Argo) handleDeploymentSuccess(task models.Task) {
	log.Info().Str("id", task.Id).Msg("App is running on the excepted version.")
	argo.metrics.failedDeployment.With(prometheus.Labels{"app": task.App}).Set(0)
	argo.state.SetTaskStatus(task.Id, "deployed", "")
}

func (argo *Argo) handleAppNotAvailable(task models.Task, err error) {
	log.Warn().Str("id", task.Id).Msgf("Deployment failed. Application not available\n%s", err.Error())
	argo.metrics.failedDeployment.With(prometheus.Labels{"app": task.App}).Inc()
	reason := fmt.Sprintf("Application not available\n\n%s", err.Error())
	argo.state.SetTaskStatus(task.Id, models.StatusFailedMessage, reason)
}

func (argo *Argo) handleAppNotHealthy(task models.Task, err error) {
	log.Warn().Str("id", task.Id).Msgf("Deployment failed. Application not healthy\n%s", err.Error())
	argo.metrics.failedDeployment.With(prometheus.Labels{"app": task.App}).Inc()
	reason := fmt.Sprintf("Application not healthy\n\n%s", err.Error())
	argo.state.SetTaskStatus(task.Id, models.StatusFailedMessage, reason)
}

func (argo *Argo) handleAppOutOfSync(task models.Task, err error) {
	log.Warn().Str("id", task.Id).Msgf("Deployment failed. Application out of sync\n%s", err.Error())
	argo.metrics.failedDeployment.With(prometheus.Labels{"app": task.App}).Inc()
	reason := fmt.Sprintf("Application out of sync\n\n%s", err.Error())
	argo.state.SetTaskStatus(task.Id, models.StatusFailedMessage, reason)
}

func (argo *Argo) handleDeploymentUnexpectedStatus(task models.Task, err error) {
	log.Error().Str("id", task.Id).Msg("Deployment timed out with unexpected status. Aborting.")
	log.Error().Str("id", task.Id).Msgf("Deployment error\n%s", err.Error())
	argo.metrics.failedDeployment.With(prometheus.Labels{"app": task.App}).Inc()
	reason := fmt.Sprintf("Deployment timeout\n\n%s", err.Error())
	argo.state.SetTaskStatus(task.Id, models.StatusFailedMessage, reason)
}

func (argo *Argo) GetAppList() []string {
	return argo.state.GetAppList()
}

func (argo *Argo) SimpleHealthCheck() bool {
	return argo.state.Check()
}
