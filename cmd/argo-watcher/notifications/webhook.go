package notifications

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"text/template"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/helpers"
	"github.com/shini4i/argo-watcher/internal/models"
)

const (
	maxErrorBodySize = 2 * 1024 // 2 KB
)

// HTTPClient defines the interface for a client that can perform HTTP requests.
// This allows for mocking in unit tests.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// WebhookService holds the configuration and a pre-compiled template for sending webhooks.
type WebhookService struct {
	Enabled              bool
	url                  string
	token                string
	authorizationHeader  string
	contentType          string
	allowedResponseCodes []int
	client               HTTPClient
	template             *template.Template
}

// NewWebhookService creates and initializes the service.
// It requires an HTTPClient, making the service testable.
func NewWebhookService(cfg *config.WebhookConfig, client HTTPClient) (*WebhookService, error) {
	if client == nil {
		return nil, errors.New("HTTPClient cannot be nil")
	}

	tmpl, err := template.New("webhook").Parse(cfg.Format)
	if err != nil {
		return nil, fmt.Errorf("failed to parse webhook template: %w", err)
	}

	return &WebhookService{
		Enabled:              cfg.Enabled,
		url:                  cfg.Url,
		token:                cfg.Token,
		authorizationHeader:  cfg.AuthorizationHeader,
		contentType:          cfg.ContentType,
		allowedResponseCodes: cfg.AllowedResponseCodes,
		client:               client,
		template:             tmpl,
	}, nil
}

// SendWebhook sends a notification for a given task.
func (s *WebhookService) SendWebhook(task models.Task) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var payload bytes.Buffer
	if err := s.template.Execute(&payload, &task); err != nil {
		return fmt.Errorf("failed to execute webhook template: %w", err)
	}

	log.Debug().Str("id", task.Id).Msgf("Sending webhook payload: %s", payload.String())

	req, err := http.NewRequestWithContext(ctx, "POST", s.url, &payload)
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", s.contentType)
	if s.token != "" {
		req.Header.Set(s.authorizationHeader, s.token)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Warn().Err(err).Str("id", task.Id).Msg("Failed to close response body")
		}
	}()

	if !helpers.Contains(s.allowedResponseCodes, resp.StatusCode) {
		lr := io.LimitReader(resp.Body, maxErrorBodySize)
		body, readErr := io.ReadAll(lr)
		if readErr != nil {
			return fmt.Errorf("received non-allowed status code %d, and failed to read response body: %w", resp.StatusCode, readErr)
		}
		return fmt.Errorf("received non-allowed status code %d: %s", resp.StatusCode, string(body))
	}

	_, err = io.Copy(io.Discard, resp.Body)
	if err != nil {
		log.Warn().Err(err).Str("id", task.Id).Msg("Failed to discard response body on success")
	}

	return nil
}
