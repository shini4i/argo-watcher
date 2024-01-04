package server

import (
	"errors"
	"fmt"
	"time"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/prometheus"

	"github.com/rs/zerolog/log"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/state"
	"github.com/shini4i/argo-watcher/internal/models"
)

var (
	argoSyncRetryDelay = 15 * time.Second
)

const (
	ArgoAPIErrorTemplate        = "ArgoCD API Error: %s"
	argoUnavailableErrorMessage = "connect: connection refused"
)

type Argo struct {
	metrics prometheus.MetricsInterface
	api     ArgoApiInterface
	state   state.State
}

func (argo *Argo) Init(state state.State, api ArgoApiInterface, metrics prometheus.MetricsInterface) {
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

	if userLoggedIn == nil || !userLoggedIn.LoggedIn {
		argo.metrics.SetArgoUnavailable(true)
		return "down", errors.New(models.StatusArgoCDFailedLogin)
	}

	argo.metrics.SetArgoUnavailable(false)
	return "up", nil
}

func (argo *Argo) AddTask(task models.Task) (*models.Task, error) {
	_, err := argo.Check()
	if err != nil {
		return nil, errors.New(err.Error())
	}

	if task.Images == nil || len(task.Images) == 0 {
		return nil, fmt.Errorf("trying to create task without images")
	}

	if task.App == "" {
		return nil, fmt.Errorf("trying to create task without app name")
	}

	newTask, err := argo.state.Add(task)
	if err != nil {
		return nil, err
	}

	log.Info().Str("id", newTask.Id).Msgf("A new task was triggered")
	for index, value := range newTask.Images {
		log.Info().Str("id", newTask.Id).Msgf("Task image [%d] expecting tag %s in app %s.",
			index,
			value.Tag,
			task.App,
		)
	}

	argo.metrics.AddProcessedDeployment()
	return newTask, nil
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
