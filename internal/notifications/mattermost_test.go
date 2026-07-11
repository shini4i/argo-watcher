package notifications

import (
	"encoding/json"
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

func validMattermostConfig() *config.MattermostConfig {
	return &config.MattermostConfig{
		Enabled:   true,
		Url:       "http://mattermost.local",
		Token:     "bot-token",
		ChannelId: "channel123",
		Format:    `{{.App}}: {{.Status}}`,

		MentionAuthor: true,
	}
}

// TestNewMattermostStrategy tests the constructor for MattermostStrategy.
func TestNewMattermostStrategy(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		cfg := validMattermostConfig()
		cfg.Url = "http://mattermost.local/"
		client := &MockHTTPClient{}

		service, err := NewMattermostStrategy(cfg, client)

		require.NoError(t, err)
		assert.NotNil(t, service)
		assert.Equal(t, "http://mattermost.local", service.baseURL)
		assert.Equal(t, cfg.Token, service.token)
		assert.Equal(t, cfg.ChannelId, service.channelID)
		assert.True(t, service.mentionAuthor)
		assert.NotNil(t, service.template)
		assert.NotNil(t, service.rootPosts)
		assert.Same(t, client, service.client)
	})

	t.Run("Nil Config", func(t *testing.T) {
		service, err := NewMattermostStrategy(nil, &MockHTTPClient{})

		require.Error(t, err)
		assert.Nil(t, service)
		assert.Equal(t, "mattermost configuration cannot be nil", err.Error())
	})

	t.Run("Disabled Config", func(t *testing.T) {
		cfg := validMattermostConfig()
		cfg.Enabled = false

		service, err := NewMattermostStrategy(cfg, &MockHTTPClient{})

		require.Error(t, err)
		assert.Nil(t, service)
		assert.Equal(t, "mattermost strategy disabled", err.Error())
	})

	t.Run("Empty Fields", func(t *testing.T) {
		testCases := []struct {
			name     string
			mutate   func(*config.MattermostConfig)
			expected string
		}{
			{"Url", func(c *config.MattermostConfig) { c.Url = " " }, "mattermost url cannot be empty"},
			{"Token", func(c *config.MattermostConfig) { c.Token = "" }, "mattermost token cannot be empty"},
			{"ChannelId", func(c *config.MattermostConfig) { c.ChannelId = "" }, "mattermost channel id cannot be empty"},
			{"Format", func(c *config.MattermostConfig) { c.Format = "  " }, "mattermost format cannot be empty"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				cfg := validMattermostConfig()
				tc.mutate(cfg)

				service, err := NewMattermostStrategy(cfg, &MockHTTPClient{})

				require.Error(t, err)
				assert.Nil(t, service)
				assert.Equal(t, tc.expected, err.Error())
			})
		}
	})

	t.Run("Nil HTTPClient", func(t *testing.T) {
		service, err := NewMattermostStrategy(validMattermostConfig(), nil)

		require.Error(t, err)
		assert.Nil(t, service)
		assert.Equal(t, "HTTPClient cannot be nil", err.Error())
	})

	t.Run("Invalid Template", func(t *testing.T) {
		cfg := validMattermostConfig()
		cfg.Format = "{{.App"

		service, err := NewMattermostStrategy(cfg, &MockHTTPClient{})

		require.Error(t, err)
		assert.Nil(t, service)
		assert.Contains(t, err.Error(), "failed to parse mattermost template")
	})
}

func newTestMattermostStrategy(t *testing.T, client HTTPClient) *MattermostStrategy {
	t.Helper()

	tmpl, err := template.New("mattermost").Parse(`{{.App}}: {{.Status}}`)
	require.NoError(t, err)

	return &MattermostStrategy{
		baseURL:       "http://mattermost.local",
		token:         "bot-token",
		channelID:     "channel123",
		mentionAuthor: true,
		client:        client,
		template:      tmpl,
		rootPosts:     map[string]string{},
	}
}

func decodePostBody(t *testing.T, req *http.Request) map[string]any {
	t.Helper()

	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	return decoded
}

// TestMattermostSend tests the Send method of the MattermostStrategy.
func TestMattermostSend(t *testing.T) {
	t.Run("Start Creates Root Post And Stores Post Id", func(t *testing.T) {
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				assert.Equal(t, http.MethodPost, req.Method)
				assert.Equal(t, "http://mattermost.local/api/v4/posts", req.URL.String())
				assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
				assert.Equal(t, "Bearer bot-token", req.Header.Get("Authorization"))

				body := decodePostBody(t, req)
				assert.Equal(t, "channel123", body["channel_id"])
				assert.Equal(t, "@jdoe app1: in progress", body["message"])
				assert.NotContains(t, body, "root_id")

				return &http.Response{
					StatusCode: http.StatusCreated,
					Body:       io.NopCloser(strings.NewReader(`{"id":"post123"}`)),
				}, nil
			},
		}
		service := newTestMattermostStrategy(t, mockClient)

		err := service.Send(models.Task{Id: "task-1", App: "app1", Status: models.StatusInProgressMessage, Author: "jdoe"})

		require.NoError(t, err)
		assert.Equal(t, "post123", service.rootPosts["task-1"])
	})

	t.Run("Result Replies In Thread With Mention", func(t *testing.T) {
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				body := decodePostBody(t, req)
				assert.Equal(t, "post123", body["root_id"])
				assert.Equal(t, "@jdoe app1: deployed", body["message"])

				return &http.Response{
					StatusCode: http.StatusCreated,
					Body:       io.NopCloser(strings.NewReader(`{"id":"post456"}`)),
				}, nil
			},
		}
		service := newTestMattermostStrategy(t, mockClient)
		service.rootPosts["task-1"] = "post123"

		err := service.Send(models.Task{Id: "task-1", App: "app1", Status: models.StatusDeployedMessage, Author: "jdoe"})

		require.NoError(t, err)
		assert.NotContains(t, service.rootPosts, "task-1")
	})

	t.Run("Mention Disabled Leaves Message As Is", func(t *testing.T) {
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				body := decodePostBody(t, req)
				assert.Equal(t, "app1: deployed", body["message"])

				return &http.Response{
					StatusCode: http.StatusCreated,
					Body:       io.NopCloser(strings.NewReader(`{"id":"post456"}`)),
				}, nil
			},
		}
		service := newTestMattermostStrategy(t, mockClient)
		service.mentionAuthor = false

		err := service.Send(models.Task{Id: "task-1", App: "app1", Status: models.StatusDeployedMessage, Author: "jdoe"})

		require.NoError(t, err)
	})

	t.Run("Result Without Known Root Posts To Channel", func(t *testing.T) {
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				body := decodePostBody(t, req)
				assert.NotContains(t, body, "root_id")
				assert.Equal(t, "app1: failed", body["message"])

				return &http.Response{
					StatusCode: http.StatusCreated,
					Body:       io.NopCloser(strings.NewReader(`{"id":"post456"}`)),
				}, nil
			},
		}
		service := newTestMattermostStrategy(t, mockClient)

		err := service.Send(models.Task{Id: "task-1", App: "app1", Status: models.StatusFailedMessage})

		require.NoError(t, err)
	})

	t.Run("Root Entry Deleted Even If Result Post Fails", func(t *testing.T) {
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("network error")
			},
		}
		service := newTestMattermostStrategy(t, mockClient)
		service.rootPosts["task-1"] = "post123"

		err := service.Send(models.Task{Id: "task-1", App: "app1", Status: models.StatusFailedMessage})

		require.Error(t, err)
		assert.NotContains(t, service.rootPosts, "task-1")
	})

	t.Run("Failed Start Does Not Store Post Id", func(t *testing.T) {
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Body:       io.NopCloser(strings.NewReader(`{"message":"no access"}`)),
				}, nil
			},
		}
		service := newTestMattermostStrategy(t, mockClient)

		err := service.Send(models.Task{Id: "task-1", App: "app1", Status: models.StatusInProgressMessage})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "mattermost returned status code 403")
		assert.Contains(t, err.Error(), "no access")
		assert.Empty(t, service.rootPosts)
	})

	t.Run("Missing Post Id In Response", func(t *testing.T) {
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusCreated,
					Body:       io.NopCloser(strings.NewReader(`{}`)),
				}, nil
			},
		}
		service := newTestMattermostStrategy(t, mockClient)

		err := service.Send(models.Task{Id: "task-1", App: "app1", Status: models.StatusInProgressMessage})

		require.Error(t, err)
		assert.Equal(t, "mattermost response is missing post id", err.Error())
	})

	t.Run("Invalid Response Body", func(t *testing.T) {
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusCreated,
					Body:       io.NopCloser(strings.NewReader(`not-json`)),
				}, nil
			},
		}
		service := newTestMattermostStrategy(t, mockClient)

		err := service.Send(models.Task{Id: "task-1", App: "app1", Status: models.StatusInProgressMessage})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode mattermost response")
	})

	t.Run("Failed Request Creation", func(t *testing.T) {
		service := newTestMattermostStrategy(t, &MockHTTPClient{})
		service.baseURL = ":invalid-url:"

		err := service.Send(models.Task{Id: "task-1", App: "app1", Status: models.StatusInProgressMessage})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create mattermost request")
	})

	t.Run("Non-201 Status With Body Read Error", func(t *testing.T) {
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Body:       io.NopCloser(&errorReader{err: errors.New("read error")}),
				}, nil
			},
		}
		service := newTestMattermostStrategy(t, mockClient)

		err := service.Send(models.Task{Id: "task-1", App: "app1", Status: models.StatusInProgressMessage})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "mattermost returned status code 403, and failed to read response body: read error")
	})

	t.Run("Failed Template Execution", func(t *testing.T) {
		invalidTmpl, err := template.New("mattermost").Parse(`{{.Missing}}`)
		require.NoError(t, err)

		service := newTestMattermostStrategy(t, &MockHTTPClient{})
		service.template = invalidTmpl

		err = service.Send(models.Task{Id: "task-1", Status: models.StatusInProgressMessage})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to execute mattermost template")
	})
}
