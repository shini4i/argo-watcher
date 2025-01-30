package argocd

import (
    "github.com/shini4i/argo-watcher/internal/models"
    "time"
)

// RolloutMonitor handles the monitoring of application rollouts
type RolloutMonitor interface {
    // WaitForRollout waits for an application to reach the desired state
    WaitForRollout(app *models.Application, images []string, timeout time.Duration) (*models.Application, error)
    // GetRolloutStatus returns the current status of the rollout
    GetRolloutStatus(app *models.Application, images []string) (string, error)
}

// GitOperations handles git repository operations
type GitOperations interface {
    // UpdateImageTags updates the image tags in the git repository
    UpdateImageTags(app *models.Application, task *models.Task) error
    // ValidateConfig validates the git configuration
    ValidateConfig(app *models.Application) error
}

// StatusNotifier handles deployment status notifications
type StatusNotifier interface {
    // NotifyStatus sends a notification about the deployment status
    NotifyStatus(task models.Task, status string, message string) error
}
