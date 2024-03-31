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
	client  *http.Client
}

func NewWebhookService(webhookConfig *config.WebhookConfig) *WebhookService {
	return &WebhookService{
		Enabled: webhookConfig.Enabled,
		config:  webhookConfig,
		client:  &http.Client{},
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

	req, err := http.NewRequest("POST", service.config.Url, &payload)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	if service.config.Token != "" {
		req.Header.Set(service.config.AuthorizationHeader, service.config.Token)
	}

	resp, err := service.client.Do(req)
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
