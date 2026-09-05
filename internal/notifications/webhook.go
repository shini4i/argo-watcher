package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/models"
)

const (
	maxErrorBodySize = 2 * 1024 // 2 KB
)

// NotificationStrategy defines the contract for delivering task notifications.
type NotificationStrategy interface {
	Send(task models.Task) error
}

// HTTPClient defines the interface for a client that can perform HTTP requests.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Notifier orchestrates the configured notification strategies.
type Notifier struct {
	strategies []NotificationStrategy
}

// NewNotifier constructs a Notifier with the supplied strategies.
func NewNotifier(strategies ...NotificationStrategy) *Notifier {
	return &Notifier{strategies: strategies}
}

// Send dispatches the task notification using all registered strategies and joins encountered errors.
func (n *Notifier) Send(task models.Task) error {
	if n == nil {
		return nil
	}

	var errs []error
	for _, strategy := range n.strategies {
		if strategy == nil {
			continue
		}

		if err := strategy.Send(task); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// isJSONBody reports whether the configured content type describes JSON, and so
// whether the values rendered into the body have to be escaped as JSON strings.
// The media type is parsed rather than searched: a parameter may contain "json"
// while the body is something else entirely ("text/plain; profile=json").
func isJSONBody(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}

	_, subtype, found := strings.Cut(mediaType, "/")

	return found && (subtype == "json" || strings.HasSuffix(subtype, "+json"))
}

// escapeForJSON returns a copy of task whose every string field is escaped for
// use inside a JSON string literal. A value carrying a quote or a newline —
// an author from an unauthenticated submission, a StatusReason quoting Argo CD —
// would otherwise end the string it sits in and break the body.
func escapeForJSON(task models.Task) models.Task {
	task.Id = jsonStringBody(task.Id)
	task.App = jsonStringBody(task.App)
	task.Author = jsonStringBody(task.Author)
	task.Project = jsonStringBody(task.Project)
	task.Status = jsonStringBody(task.Status)
	task.StatusReason = jsonStringBody(task.StatusReason)
	task.RollbackTargetId = jsonStringBody(task.RollbackTargetId)

	images := make([]models.Image, len(task.Images))
	for i, image := range task.Images {
		images[i] = models.Image{
			Image: jsonStringBody(image.Image),
			Tag:   jsonStringBody(image.Tag),
		}
	}
	task.Images = images

	return task
}

// jsonStringBody renders s as a JSON string and returns what belongs between the
// quotes. HTML escaping is off so a value keeps the bytes it arrived with
// wherever JSON does not require otherwise.
func jsonStringBody(s string) string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)

	// Encoding a string cannot fail: bytes.Buffer never errors, string is always
	// a supported type, and invalid UTF-8 is replaced rather than rejected.
	_ = encoder.Encode(s)

	// Encode appends a newline after the quoted string.
	quoted := strings.TrimRight(buf.String(), "\n")

	return strings.TrimSuffix(strings.TrimPrefix(quoted, `"`), `"`)
}

// WebhookStrategy holds the configuration and a pre-compiled template for sending webhooks.
type WebhookStrategy struct {
	url                  string
	token                string
	authorizationHeader  string
	contentType          string
	allowedResponseCodes []int
	client               HTTPClient
	template             *template.Template
}

// NewWebhookStrategy requires an HTTPClient and a non-empty format template.
func NewWebhookStrategy(cfg *config.WebhookConfig, client HTTPClient) (*WebhookStrategy, error) {
	if cfg == nil {
		return nil, errors.New("webhook configuration cannot be nil")
	}
	if !cfg.Enabled {
		return nil, errors.New("webhook strategy disabled")
	}
	if strings.TrimSpace(cfg.Format) == "" {
		return nil, errors.New("webhook format cannot be empty")
	}
	if client == nil {
		return nil, errors.New("HTTPClient cannot be nil")
	}

	tmpl, err := template.New("webhook").Parse(cfg.Format)
	if err != nil {
		return nil, fmt.Errorf("failed to parse webhook template: %w", err)
	}

	return &WebhookStrategy{
		url:                  cfg.Url,
		token:                cfg.Token,
		authorizationHeader:  cfg.AuthorizationHeader,
		contentType:          cfg.ContentType,
		allowedResponseCodes: cfg.AllowedResponseCodes,
		client:               client,
		template:             tmpl,
	}, nil
}

// Send delivers the webhook notification for the provided task.
func (s *WebhookStrategy) Send(task models.Task) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// The template assembles the body by hand and text/template escapes nothing,
	// so the values are escaped instead. Left alone for a receiver that is not
	// expecting JSON, which wants the literal text it has always been sent.
	data := task
	if isJSONBody(s.contentType) {
		data = escapeForJSON(task)
	}

	var payload bytes.Buffer
	if err := s.template.Execute(&payload, data); err != nil {
		return fmt.Errorf("failed to execute webhook template: %w", err)
	}

	slog.Debug("Sending webhook payload", "payload", payload.String(), "id", task.Id)

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
			slog.Warn("Failed to close response body", "error", err, "id", task.Id)
		}
	}()

	if !slices.Contains(s.allowedResponseCodes, resp.StatusCode) {
		lr := io.LimitReader(resp.Body, maxErrorBodySize)
		body, readErr := io.ReadAll(lr)
		if readErr != nil {
			return fmt.Errorf("received non-allowed status code %d, and failed to read response body: %w", resp.StatusCode, readErr)
		}
		return fmt.Errorf("received non-allowed status code %d: %s", resp.StatusCode, string(body))
	}

	_, err = io.Copy(io.Discard, resp.Body)
	if err != nil {
		slog.Warn("Failed to discard response body on success", "error", err, "id", task.Id)
	}

	return nil
}
