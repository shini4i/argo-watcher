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

func TestSendWebhook(t *testing.T) {
	// Create a test server that checks the received payload
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {
				t.Errorf("Failed to close response body: %v", err)
			}
		}(r.Body)

		var payload TestWebhookPayload
		err := json.Unmarshal(body, &payload)
		assert.NoError(t, err)
		assert.Equal(t, mockTask.Id, payload.Id)
		assert.Equal(t, mockTask.App, payload.App)
		assert.Equal(t, mockTask.Status, payload.Status)
	}))
	defer testServer.Close()

	// Create a mock ServerConfig with the test server's URL
	serverConfig := &config.ServerConfig{
		Webhook: config.WebhookConfig{
			Url:    testServer.URL,
			Format: `{"id": "{{.Id}}","app": "{{.App}}","status": "{{.Status}}"}`,
		},
	}

	// Create a new WebhookService with the mock ServerConfig
	service := NewWebhookService(serverConfig)

	// Call the SendWebhook method with the mock Task
	err := service.SendWebhook(mockTask)

	// Check the returned error
	assert.NoError(t, err)
}
