package notifications

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"text/template"

	"github.com/rs/zerolog/log"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/models"
)

type WebhookService struct {
	Enabled bool
	config  *config.WebhookConfig
}

func NewWebhookService(webhookConfig *config.WebhookConfig) *WebhookService {
	if webhookConfig == nil {
		return &WebhookService{
			Enabled: false,
		}
	}
	return &WebhookService{
		Enabled: true,
		config:  webhookConfig,
	}
}

func (service *WebhookService) SendWebhook(task models.Task) error {
	tmpl, err := template.New("webhook").Parse(service.config.Format)
	if err != nil {
		return err
	}

	var payload bytes.Buffer
	if err := tmpl.Execute(&payload, task); err != nil {
		return err
	}

	log.Debug().Str("id", task.Id).Msgf("Sending webhook payload: %s", payload.String())

	resp, err := http.Post(service.config.Url, "application/json", &payload)
	if err != nil {
		return err
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Err(err).Msg("Failed to close response body")
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("received non-OK status code: %v", resp.StatusCode)
	}

	return nil
}
