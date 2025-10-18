package notifications

import "github.com/shini4i/argo-watcher/internal/models"

// NotificationStrategyFunc allows defining inline notification strategies for tests.
type NotificationStrategyFunc func(models.Task) error

// Send executes the wrapped function.
func (f NotificationStrategyFunc) Send(task models.Task) error {
	return f(task)
}
