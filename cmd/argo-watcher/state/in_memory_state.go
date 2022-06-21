package state

import (
	"github.com/romana/rlog"
	"time"

	h "github.com/shini4i/argo-watcher/internal/helpers"
	m "github.com/shini4i/argo-watcher/internal/models"
)

type InMemoryState struct {
	tasks []m.Task
}

func (state *InMemoryState) Connect() {
	rlog.Debug("InMemoryState does not connect to anything. Skipping.")
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
		if app == "" {
			if task.Created >= startTime && task.Created <= endTime {
				tasks = append(tasks, task)
			}
		} else {
			if task.Created >= startTime && task.Created <= endTime && task.App == app {
				tasks = append(tasks, task)
			}
		}
	}

	if tasks == nil {
		return []m.Task{}
	}

	return tasks
}

func (state *InMemoryState) GetTaskStatus(id string) string {
	for _, task := range state.tasks {
		if task.Id == id {
			return task.Status
		}
	}
	return "task not found"
}

func (state *InMemoryState) SetTaskStatus(id string, status string) {
	for idx, task := range state.tasks {
		if task.Id == id {
			state.tasks[idx].Status = status
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

	return apps
}

func (state *InMemoryState) Check() bool {
	return true
}
