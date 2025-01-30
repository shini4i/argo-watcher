package argocd

import (
    "github.com/rs/zerolog/log"
    "github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
    "github.com/shini4i/argo-watcher/cmd/argo-watcher/notifications"
    "github.com/shini4i/argo-watcher/internal/models"
)

type DefaultStatusNotifier struct {
    webhookService *notifications.WebhookService
}

func NewDefaultStatusNotifier(webhookConfig *config.WebhookConfig) StatusNotifier {
    return &DefaultStatusNotifier{
        webhookService: notifications.NewWebhookService(webhookConfig),
    }
}

func (n *DefaultStatusNotifier) NotifyStatus(task models.Task, status string, message string) error {
    if !n.webhookService.Enabled {
        return nil
    }

    task.Status = status
    if err := n.webhookService.SendWebhook(task); err != nil {
        log.Error().Str("id", task.Id).Msgf("Failed to send webhook notification: %v", err)
        return err
    }

    return nil
}
