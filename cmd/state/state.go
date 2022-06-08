package state

import m "watcher/models"

type State interface {
	Connect()
	Add(task m.Task)
	GetTasks(startTime float64, endTime float64, app string) []m.Task
	GetTaskStatus(id string) string
	SetTaskStatus(id string, status string)
	GetAppList() []string
	Check() bool
}
