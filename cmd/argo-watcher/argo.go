package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/state"
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
	metrics       MetricsInterface
	api           ArgoApiInterface
	state         state.State
}

func (argo *Argo) Init(state state.State, api ArgoApiInterface, metrics MetricsInterface) {
	// setup dependencies
	argo.api = api
	argo.state = state
	argo.metrics = metrics
}

func (argo *Argo) Check() (string, error) {
	connectionActive := argo.state.Check()
	userLoggedIn, loginError := argo.api.GetUserInfo()

	if !connectionActive {
		argo.metrics.SetArgoUnavailable(true)
		return "down", errors.New(models.StatusConnectionUnavailable)
	}

	if loginError != nil {
		argo.metrics.SetArgoUnavailable(true)
		return "down", errors.New(models.StatusArgoCDUnavailableMessage)
	}

	if userLoggedIn == nil {
		argo.metrics.SetArgoUnavailable(true)
		return "down", errors.New(models.StatusArgoCDFailedLogin)
	}

	argo.metrics.SetArgoUnavailable(false)
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
	argo.metrics.AddProcessedDeployment()

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

func (argo *Argo) GetAppList() []string {
	return argo.state.GetAppList()
}

func (argo *Argo) SimpleHealthCheck() bool {
	return argo.state.Check()
}
