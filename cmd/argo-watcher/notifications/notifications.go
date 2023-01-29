package notifications

import m "github.com/shini4i/argo-watcher/internal/models"

type Notification interface {
	Init(channel string)
	Send(task m.Task, status string) (bool, error)
}
