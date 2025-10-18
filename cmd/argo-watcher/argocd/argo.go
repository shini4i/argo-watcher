package argocd

import (
	"errors"
	"fmt"
	"time"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/prometheus"
	"github.com/shini4i/argo-watcher/internal/state"

	"github.com/rs/zerolog/log"

	"github.com/shini4i/argo-watcher/internal/models"
)

var (
	// ArgoSyncRetryDelay is the delay between ArgoCD sync status retries.
	ArgoSyncRetryDelay = 15 * time.Second
)

const (
	// ArgoAPIErrorTemplate is the template for ArgoCD API errors.
	ArgoAPIErrorTemplate = "ArgoCD API Error: %s"
	// argoUnavailableErrorMessage is the specific error message for a refused connection.
	argoUnavailableErrorMessage = "connect: connection refused"
)

// Argo is the primary controller for watcher operations.
type Argo struct {
	metrics prometheus.MetricsInterface
	api     ArgoApiInterface
	State   state.TaskRepository
}

// Init initializes the Argo controller with its dependencies.
func (argo *Argo) Init(state state.TaskRepository, api ArgoApiInterface, metrics prometheus.MetricsInterface) {
	argo.api = api
	argo.State = state
	argo.metrics = metrics
}

// Check performs a health check on ArgoCD and the state backend.
func (argo *Argo) Check() (string, error) {
	connectionActive := argo.State.Check()
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

// AddTask validates a new deployment task and adds it to the task repository.
func (argo *Argo) AddTask(task models.Task) (*models.Task, error) {
	if _, err := argo.Check(); err != nil {
		return nil, err
	}

	if task.Images == nil || len(task.Images) == 0 {
		return nil, fmt.Errorf("trying to create task without images")
	}

	if task.App == "" {
		return nil, fmt.Errorf("trying to create task without app name")
	}

	newTask, err := argo.State.AddTask(task)
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

	argo.metrics.AddProcessedDeployment(task.App)
	return newTask, nil
}

// GetTasks retrieves tasks from the state within a given time range.
func (argo *Argo) GetTasks(startTime float64, endTime float64, app string) models.TasksResponse {
	if _, err := argo.Check(); err != nil {
		return models.TasksResponse{
			Tasks: []models.Task{},
			Error: err.Error(),
		}
	}

	tasks := argo.State.GetTasks(startTime, endTime, app)

	return models.TasksResponse{
		Tasks: tasks,
	}
}

// SimpleHealthCheck checks only the state backend connection.
func (argo *Argo) SimpleHealthCheck() bool {
	return argo.State.Check()
}
