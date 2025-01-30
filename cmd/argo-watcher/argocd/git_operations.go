package argocd

import (
    "fmt"
    "github.com/rs/zerolog/log"
    "github.com/shini4i/argo-watcher/internal/models"
)

type DefaultGitOperations struct{}

func NewDefaultGitOperations() GitOperations {
    return &DefaultGitOperations{}
}

func (g *DefaultGitOperations) UpdateImageTags(app *models.Application, task *models.Task) error {
    if err := g.ValidateConfig(app); err != nil {
        return fmt.Errorf("git configuration validation failed: %w", err)
    }

    log.Debug().Str("id", task.Id).Msg("Updating image tags in git repository")
    
    if err := app.UpdateGitImageTag(task); err != nil {
        return fmt.Errorf("failed to update git image tags: %w", err)
    }

    return nil
}

func (g *DefaultGitOperations) ValidateConfig(app *models.Application) error {
    if app == nil {
        return fmt.Errorf("application is nil")
    }

    if !app.IsManagedByWatcher() {
        return fmt.Errorf("application is not managed by watcher")
    }

    return nil
}
