package state

import (
	"errors"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/rs/zerolog/log"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/helpers"
	"github.com/shini4i/argo-watcher/internal/models"
)

var taskIsObsolete = errors.New("task is obsolete")

type InMemoryState struct {
	tasks []models.Task
}

func (state *InMemoryState) Connect(serverConfig *config.ServerConfig) {
	log.Debug().Msg("InMemoryState does not connect to anything. Skipping.")
}

func (state *InMemoryState) Add(task models.Task) error {
	task.Created = float64(time.Now().Unix())
	task.Status = "in progress"
	state.tasks = append(state.tasks, task)
	return nil
}

func (state *InMemoryState) GetTasks(startTime float64, endTime float64, app string) []models.Task {
	if state.tasks == nil {
		return []models.Task{}
	}

	var tasks []models.Task

	for _, task := range state.tasks {
		if task.Created >= startTime && task.Created <= endTime {
			if app == "" {
				tasks = append(tasks, task)
			} else if app == task.App {
				tasks = append(tasks, task)
			}
		}
	}

	if tasks == nil {
		return []models.Task{}
	}

	return tasks
}

func (state *InMemoryState) GetTask(id string) (*models.Task, error) {
	for _, task := range state.tasks {
		if task.Id == id {
			return &task, nil
		}
	}
	return nil, errors.New("task not found")
}

func (state *InMemoryState) SetTaskStatus(id string, status string, reason string) {
	for idx, task := range state.tasks {
		if task.Id == id {
			state.tasks[idx].Status = status
			state.tasks[idx].StatusReason = reason
			state.tasks[idx].Updated = float64(time.Now().Unix())
		}
	}
}

func (state *InMemoryState) GetAppList() []string {
	var apps []string

	for _, app := range state.tasks {
		if !helpers.Contains(apps, app.App) {
			apps = append(apps, app.App)
		}
	}

	if apps == nil {
		return []string{}
	}

	return apps
}

func (state *InMemoryState) Check() bool {
	return true
}

func (state *InMemoryState) ProcessObsoleteTasks() {
	log.Debug().Msg("Starting watching for obsolete tasks...")
	err := retry.Do(
		func() error {
			state.tasks = processInMemoryObsoleteTasks(state.tasks)
			return desiredRetryError
		},
		retry.DelayType(retry.FixedDelay),
		retry.Delay(60*time.Minute),
		retry.Attempts(0),
	)
	if err != nil {
		log.Error().Msgf("Couldn't process obsolete tasks. Got the following error: %s", err)
	}
}

func processInMemoryObsoleteTasks(tasks []models.Task) []models.Task {
	var updatedTasks []models.Task
	for _, task := range tasks {
		if task.Status == "app not found" {
			continue
		}
		if task.Status == "in progress" && task.Updated+3600 < float64(time.Now().Unix()) {
			task.Status = "aborted"
		}
		updatedTasks = append(updatedTasks, task)
	}
	return updatedTasks
}
