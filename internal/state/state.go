package state

import (
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/models"
)

var errDesiredRetry = errors.New("desired retry error")

// TaskState represents the application state layer that exposes task repositories.
// It currently embeds the TaskRepository interface to provide task persistence operations.
type TaskState interface {
	TaskRepository
}

// TaskRepository defines the contract for task persistence.
// Implementations are responsible for connecting to the underlying storage and
// offering CRUD-like operations for deployment tasks.
type TaskRepository interface {
	Connect(serverConfig *config.ServerConfig) error
	AddTask(task models.Task) (*models.Task, error)
	GetTasks(startTime float64, endTime float64, app string) []models.Task
	GetTask(id string) (*models.Task, error)
	SetTaskStatus(id string, status string, reason string) error
	Check() bool
	ProcessObsoleteTasks(retryTimes uint)
}

// NewState creates a new task repository based on the provided server configuration.
// It initializes the appropriate repository according to the StateType field and
// ensures that the returned implementation is already connected to the storage backend.
func NewState(serverConfig *config.ServerConfig) (TaskState, error) {
	log.Debug().Msg("Initializing argo-watcher state...")
	var state TaskState
	switch name := serverConfig.StateType; name {
	case "postgres":
		log.Debug().Msg("Created postgres state..")
		state = &PostgresState{}
	case "in-memory":
		log.Debug().Msg("Created in-memory state..")
		state = &InMemoryState{}
	default:
		return nil, fmt.Errorf("unexpected state type received: %s", name)
	}

	err := state.Connect(serverConfig)
	if err != nil {
		return nil, err
	}

	return state, nil
}
