package server

import "github.com/shini4i/argo-watcher/internal/models"

// Updater defines the interface for deployment status tracking.
// This abstraction allows for easier testing and decouples the server
// from the specific ArgoStatusUpdater implementation.
type Updater interface {
	WaitForRollout(task models.Task)
}
