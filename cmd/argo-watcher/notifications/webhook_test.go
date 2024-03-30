package notifications

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/stretchr/testify/assert"
)

type TestWebhookPayload struct {
	Id     string `json:"id"`
	App    string `json:"app"`
	Status string `json:"status"`
}

var mockTask = models.Task{
	Id:     "test-id",
	App:    "test-app",
	Status: "test-status",
}

func setupTestServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {
				t.Error(err)
			}
		}(r.Body)

		var payload TestWebhookPayload
		err := json.Unmarshal(body, &payload)
		assert.NoError(t, err)
		checkPayload(t, payload)
	}))
}

func setupErrorTestServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
}

func checkPayload(t *testing.T, payload TestWebhookPayload) {
	assert.Equal(t, mockTask.Id, payload.Id)
	assert.Equal(t, mockTask.App, payload.App)
	assert.Equal(t, mockTask.Status, payload.Status)
}

func TestSendWebhook(t *testing.T) {
	t.Run("Test webhook payload", func(t *testing.T) {
		testServer := setupTestServer(t)
		defer testServer.Close()

		webhookConfig := config.WebhookConfig{
			Url:    testServer.URL,
			Format: `{"id": "{{.Id}}","app": "{{.App}}","status": "{{.Status}}"}`,
		}

		service := NewWebhookService(&webhookConfig)
		err := service.SendWebhook(mockTask)
		assert.NoError(t, err)
	})

	t.Run("Test error response", func(t *testing.T) {
		testServer := setupErrorTestServer()
		defer testServer.Close()

		webhookConfig := config.WebhookConfig{
			Url:    testServer.URL,
			Format: `{"id": "{{.Id}}","app": "{{.App}}","status": "{{.Status}}"}`,
		}

		service := NewWebhookService(&webhookConfig)
		err := service.SendWebhook(mockTask)
		assert.Error(t, err)
	})
}
