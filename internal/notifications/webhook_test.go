package notifications

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/models"
)

// MockHTTPClient is a mock implementation of the HTTPClient interface for testing.
type MockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

// Do calls the underlying DoFunc.
func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.DoFunc != nil {
		return m.DoFunc(req)
	}
	// Default behavior if DoFunc is not set
	return nil, errors.New("DoFunc is not implemented")
}

// TestNewWebhookStrategy tests the constructor for WebhookStrategy.
func TestNewWebhookStrategy(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		// arrange
		cfg := &config.WebhookConfig{
			Enabled:              true,
			Url:                  "http://localhost/webhook",
			Format:               `{"id":"{{.Id}}"}`,
			ContentType:          "application/json",
			AuthorizationHeader:  "X-Token",
			Token:                "secret",
			AllowedResponseCodes: []int{200, 201},
		}
		client := &MockHTTPClient{}

		// act
		service, err := NewWebhookStrategy(cfg, client)

		// assert
		require.NoError(t, err)
		assert.NotNil(t, service)
		assert.Equal(t, cfg.Url, service.url)
		assert.Equal(t, cfg.Token, service.token)
		assert.Equal(t, cfg.AuthorizationHeader, service.authorizationHeader)
		assert.Equal(t, cfg.ContentType, service.contentType)
		assert.Equal(t, cfg.AllowedResponseCodes, service.allowedResponseCodes)
		assert.NotNil(t, service.template)
		assert.Same(t, client, service.client)
	})

	t.Run("Nil HTTPClient", func(t *testing.T) {
		// arrange
		cfg := &config.WebhookConfig{
			Enabled: true,
			Format:  `{"id":"{{.Id}}"}`,
		}

		// act
		service, err := NewWebhookStrategy(cfg, nil)

		// assert
		require.Error(t, err)
		assert.Nil(t, service)
		assert.Equal(t, "HTTPClient cannot be nil", err.Error())
	})

	t.Run("Empty Format", func(t *testing.T) {
		cfg := &config.WebhookConfig{
			Enabled: true,
			Format:  "   ",
		}
		client := &MockHTTPClient{}

		service, err := NewWebhookStrategy(cfg, client)

		require.Error(t, err)
		assert.Nil(t, service)
		assert.Equal(t, "webhook format cannot be empty", err.Error())
	})

	t.Run("Disabled Config", func(t *testing.T) {
		cfg := &config.WebhookConfig{Enabled: false}
		client := &MockHTTPClient{}

		service, err := NewWebhookStrategy(cfg, client)

		require.Error(t, err)
		assert.Nil(t, service)
		assert.Equal(t, "webhook strategy disabled", err.Error())
	})

	t.Run("Nil Config", func(t *testing.T) {
		client := &MockHTTPClient{}

		service, err := NewWebhookStrategy(nil, client)

		require.Error(t, err)
		assert.Nil(t, service)
		assert.Equal(t, "webhook configuration cannot be nil", err.Error())
	})
}

// TestSend tests the Send method of the WebhookStrategy.
func TestSend(t *testing.T) {
	task := models.Task{Id: "test-task-123"}

	// Pre-compile a valid template for reuse in tests
	tmpl, err := template.New("webhook").Parse(`{"id":"{{.Id}}"}`)
	require.NoError(t, err)

	t.Run("Successful Webhook", func(t *testing.T) {
		// arrange
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				// Assert request details
				assert.Equal(t, http.MethodPost, req.Method)
				assert.Equal(t, "http://testhost/hook", req.URL.String())
				assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
				assert.Equal(t, "secret-token", req.Header.Get("X-Auth"))

				body, _ := io.ReadAll(req.Body)
				assert.JSONEq(t, `{"id":"test-task-123"}`, string(body))

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("")),
				}, nil
			},
		}

		service := &WebhookStrategy{
			url:                  "http://testhost/hook",
			token:                "secret-token",
			authorizationHeader:  "X-Auth",
			contentType:          "application/json",
			allowedResponseCodes: []int{200},
			client:               mockClient,
			template:             tmpl,
		}

		// act
		err := service.Send(task)

		// assert
		assert.NoError(t, err)
	})

	t.Run("Failed Template Execution", func(t *testing.T) {
		// arrange
		// Use a template that requires a field not present in the task model
		invalidTmpl, err := template.New("webhook").Parse(`{"missing_field":"{{.Missing}}>"}`)
		require.NoError(t, err)

		service := &WebhookStrategy{
			template: invalidTmpl, // a template that will fail
		}

		// act
		err = service.Send(task)

		// assert
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to execute webhook template")
	})

	t.Run("Failed Request Creation", func(t *testing.T) {
		// arrange
		service := &WebhookStrategy{
			url:      ":invalid-url:", // This will cause http.NewRequestWithContext to fail
			template: tmpl,
		}

		// act
		err := service.Send(task)

		// assert
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create webhook request")
	})

	t.Run("Client Throws Error", func(t *testing.T) {
		// arrange
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("network error")
			},
		}

		service := &WebhookStrategy{
			url:      "http://testhost/hook",
			client:   mockClient,
			template: tmpl,
		}

		// act
		err := service.Send(task)

		// assert
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to send webhook: network error")
	})

	t.Run("Non-Allowed Status Code", func(t *testing.T) {
		// arrange
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(strings.NewReader(`{"error":"internal server error"}`)),
				}, nil
			},
		}

		service := &WebhookStrategy{
			url:                  "http://testhost/hook",
			allowedResponseCodes: []int{200},
			client:               mockClient,
			template:             tmpl,
		}

		// act
		err := service.Send(task)

		// assert
		require.Error(t, err)
		assert.Equal(t, "received non-allowed status code 500: {\"error\":\"internal server error\"}", err.Error())
	})

	t.Run("Non-Allowed Status Code with Body Read Error", func(t *testing.T) {
		// arrange
		// Custom reader that returns an error on Read
		errorReader := &errorReader{err: errors.New("read error")}

		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Body:       io.NopCloser(errorReader),
				}, nil
			},
		}

		service := &WebhookStrategy{
			url:                  "http://testhost/hook",
			allowedResponseCodes: []int{200},
			client:               mockClient,
			template:             tmpl,
		}

		// act
		err := service.Send(task)

		// assert
		require.Error(t, err)
		assert.Contains(t, err.Error(), "received non-allowed status code 403, and failed to read response body: read error")
	})
}

func TestNotifierSend(t *testing.T) {
	task := models.Task{Id: "aggregate-errors"}

	t.Run("NilNotifier", func(t *testing.T) {
		var notifier *Notifier
		assert.NoError(t, notifier.Send(task))
	})

	t.Run("SkipsNilStrategies", func(t *testing.T) {
		notifier := NewNotifier(nil)
		assert.NoError(t, notifier.Send(task))
	})

	t.Run("AggregatesErrors", func(t *testing.T) {
		notifier := NewNotifier(NotificationStrategyFunc(func(models.Task) error {
			return errors.New("first")
		}), NotificationStrategyFunc(func(models.Task) error {
			return errors.New("second")
		}))

		err := notifier.Send(task)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "first")
		assert.Contains(t, err.Error(), "second")
	})
}

// NotificationStrategyFunc allows defining inline notification strategies for tests.
type NotificationStrategyFunc func(models.Task) error

// Send executes the wrapped function.
func (f NotificationStrategyFunc) Send(task models.Task) error {
	return f(task)
}

// errorReader is a helper struct that implements io.Reader and always returns an error.
type errorReader struct {
	err error
}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, r.err
}
