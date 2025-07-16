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

// TestNewWebhookService tests the constructor for WebhookService.
func TestNewWebhookService(t *testing.T) {
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
		service, err := NewWebhookService(cfg, client)

		// assert
		require.NoError(t, err)
		assert.NotNil(t, service)
		assert.True(t, service.Enabled)
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
		cfg := &config.WebhookConfig{}

		// act
		service, err := NewWebhookService(cfg, nil)

		// assert
		require.Error(t, err)
		assert.Nil(t, service)
		assert.Equal(t, "HTTPClient cannot be nil", err.Error())
	})
}

// TestSendWebhook tests the SendWebhook method of the WebhookService.
func TestSendWebhook(t *testing.T) {
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

		service := &WebhookService{
			url:                  "http://testhost/hook",
			token:                "secret-token",
			authorizationHeader:  "X-Auth",
			contentType:          "application/json",
			allowedResponseCodes: []int{200},
			client:               mockClient,
			template:             tmpl,
		}

		// act
		err := service.SendWebhook(task)

		// assert
		assert.NoError(t, err)
	})

	t.Run("Failed Template Execution", func(t *testing.T) {
		// arrange
		// Use a template that requires a field not present in the task model
		invalidTmpl, err := template.New("webhook").Parse(`{"missing_field":"{{.Missing}}>"}`)
		require.NoError(t, err)

		service := &WebhookService{
			template: invalidTmpl, // a template that will fail
		}

		// act
		err = service.SendWebhook(task)

		// assert
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to execute webhook template")
	})

	t.Run("Failed Request Creation", func(t *testing.T) {
		// arrange
		service := &WebhookService{
			url:      ":invalid-url:", // This will cause http.NewRequestWithContext to fail
			template: tmpl,
		}

		// act
		err := service.SendWebhook(task)

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

		service := &WebhookService{
			url:      "http://testhost/hook",
			client:   mockClient,
			template: tmpl,
		}

		// act
		err := service.SendWebhook(task)

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

		service := &WebhookService{
			url:                  "http://testhost/hook",
			allowedResponseCodes: []int{200},
			client:               mockClient,
			template:             tmpl,
		}

		// act
		err := service.SendWebhook(task)

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

		service := &WebhookService{
			url:                  "http://testhost/hook",
			allowedResponseCodes: []int{200},
			client:               mockClient,
			template:             tmpl,
		}

		// act
		err := service.SendWebhook(task)

		// assert
		require.Error(t, err)
		assert.Contains(t, err.Error(), "received non-allowed status code 403, and failed to read response body: read error")
	})
}

// errorReader is a helper struct that implements io.Reader and always returns an error.
type errorReader struct {
	err error
}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, r.err
}
