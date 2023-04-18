package state

import (
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/conf"
	"github.com/shini4i/argo-watcher/internal/models"
)

type State interface {
	Connect(config *conf.ServerConfig)
	Add(task models.Task)
	GetTasks(startTime float64, endTime float64, app string) []models.Task
	GetTask(id string) (*models.Task, error)
	SetTaskStatus(id string, status string, reason string)
	GetAppList() []string
	Check() bool
	ProcessObsoleteTasks()
}


func NewState(config *conf.ServerConfig) (State, error) {
	log.Debug().Msg("Initializing argo-watcher state...")
	var state State
	switch name := config.StateType; name {
		case "postgres":
			state = &PostgresState{}
		case "in-memory":
			state = &InMemoryState{}
		default:
			return nil, fmt.Errorf("unexpected state type received: %s", name)
	}
	
	state.Connect(config)
	return state, nil
}