package state

import (
	"errors"
	"github.com/avast/retry-go/v4"
	"github.com/rs/zerolog/log"
	"time"

	h "github.com/shini4i/argo-watcher/internal/helpers"
	m "github.com/shini4i/argo-watcher/internal/models"
)

type InMemoryState struct {
	tasks []m.Task
}

func (state *InMemoryState) Connect() {
	log.Debug().Msg("InMemoryState does not connect to anything. Skipping.")
}

func (state *InMemoryState) Add(task m.Task) {
	task.Created = float64(time.Now().Unix())
	task.Status = "in progress"
	state.tasks = append(state.tasks, task)
}

func (state *InMemoryState) GetTasks(startTime float64, endTime float64, app string) []m.Task {
	if state.tasks == nil {
		return []m.Task{}
	}

	var tasks []m.Task

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
		return []m.Task{}
	}

	return tasks
}

func (state *InMemoryState) GetTask(id string) (*m.Task, error) {
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
		if !h.Contains(apps, app.App) {
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
			for i := 0; i < len(state.tasks); i++ {
				if state.tasks[i].Status == "app not found" {
					state.tasks = append(state.tasks[:i], state.tasks[i+1:]...)
					i--
				}
			}

			for idx, task := range state.tasks {
				if task.Status == "app not found" {
					if task.Status == "in progress" && task.Updated+3600 < float64(time.Now().Unix()) {
						state.tasks[idx].Status = "aborted"
					}
				}
			}

			return errors.New("returning error to retry")
		},
		retry.DelayType(retry.FixedDelay),
		retry.Delay(60*time.Minute),
		retry.Attempts(0),
	)

	if err != nil {
		log.Error().Msgf("Couldn't process obsolete tasks. Got the following error: %s", err)
	}
}
