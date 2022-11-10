package state

import (
	m "github.com/shini4i/argo-watcher/internal/models"
)

type State interface {
	Connect()
	Add(task m.Task)
	GetTasks(startTime float64, endTime float64, app string) []m.Task
	GetTask(id string) (*m.Task, error)
	SetTaskStatus(id string, status string, reason string)
	GetAppList() []string
	Check() bool
	ProcessObsoleteTasks()
}
