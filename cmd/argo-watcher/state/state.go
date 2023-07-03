package state

import (
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/models"
)

var desiredRetryError = errors.New("desired retry error")

type State interface {
	Connect(serverConfig *config.ServerConfig)
	Add(task models.Task) error
	GetTasks(startTime float64, endTime float64, app string) []models.Task
	GetTask(id string) (*models.Task, error)
	SetTaskStatus(id string, status string, reason string)
	GetAppList() []string
	Check() bool
	ProcessObsoleteTasks(retryTimes uint)
}

// NewState creates a new instance of the state based on the provided server configuration.
// It initializes the appropriate state based on the StateType field in the server configuration.
// Currently, it supports "postgres" and "in-memory" state types.
// It returns the created state instance and an error if the state type is not recognized or if there was an error connecting to the state.
// The created state instance is already connected to the state storage based on the provided server configuration.
func NewState(serverConfig *config.ServerConfig) (State, error) {
	log.Debug().Msg("Initializing argo-watcher state...")
	var state State
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

	state.Connect(serverConfig)
	return state, nil
}
