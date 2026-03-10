package state

import (
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/models"
)

var errDesiredRetry = errors.New("desired retry error")

// TaskRepository defines the contract for task persistence.
// Implementations are responsible for connecting to the underlying storage and
// offering CRUD-like operations for deployment tasks.
type TaskRepository interface {
	Connect(serverConfig *config.ServerConfig) error
	AddTask(task models.Task) (*models.Task, error)
	GetTasks(startTime float64, endTime float64, app string, limit int, offset int) ([]models.Task, int64)
	GetTask(id string) (*models.Task, error)
	SetTaskStatus(id string, status string, reason string) error
	Check() bool
	ProcessObsoleteTasks(retryTimes uint)
}

// NewState creates a new task repository based on the provided server configuration.
// It initializes the appropriate repository according to the StateType field and
// ensures that the returned implementation is already connected to the storage backend.
func NewState(serverConfig *config.ServerConfig) (TaskRepository, error) {
	log.Debug().Msg("Initializing argo-watcher state...")
	var state TaskRepository
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
