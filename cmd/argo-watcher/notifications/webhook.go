package notifications

import (
	"bytes"
	"io"
	"net/http"
	"text/template"

	"github.com/rs/zerolog/log"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/models"
)

type WebhookService struct {
	serverConfig *config.ServerConfig
}

func NewWebhookService(serverConfig *config.ServerConfig) *WebhookService {
	return &WebhookService{
		serverConfig: serverConfig,
	}
}

func (service *WebhookService) SendWebhook(task models.Task) error {
	tmpl, err := template.New("webhook").Parse(service.serverConfig.Webhook.Format)
	if err != nil {
		return err
	}

	var payload bytes.Buffer
	if err := tmpl.Execute(&payload, task); err != nil {
		return err
	}

	resp, err := http.Post(service.serverConfig.Webhook.URL, "application/json", &payload)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error().Err(err).Msg("Failed to close response body")
		}
	}(resp.Body)

	return nil
}
