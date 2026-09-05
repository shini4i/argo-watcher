package notifications

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/mocks"
	"github.com/shini4i/argo-watcher/internal/models"
)

func TestNewWebhookStrategy(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		cfg := &config.WebhookConfig{
			Enabled:              true,
			Url:                  "http://localhost/webhook",
			Format:               `{"id":"{{.Id}}"}`,
			ContentType:          "application/json",
			AuthorizationHeader:  "X-Token",
			Token:                "secret",
			AllowedResponseCodes: []int{200, 201},
		}
		client := mocks.NewMockHTTPClient(ctrl)

		service, err := NewWebhookStrategy(cfg, client)

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
		cfg := &config.WebhookConfig{
			Enabled: true,
			Format:  `{"id":"{{.Id}}"}`,
		}

		service, err := NewWebhookStrategy(cfg, nil)

		require.Error(t, err)
		assert.Nil(t, service)
		assert.Equal(t, "HTTPClient cannot be nil", err.Error())
	})

	t.Run("Empty Format", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		cfg := &config.WebhookConfig{
			Enabled: true,
			Format:  "   ",
		}
		client := mocks.NewMockHTTPClient(ctrl)

		service, err := NewWebhookStrategy(cfg, client)

		require.Error(t, err)
		assert.Nil(t, service)
		assert.Equal(t, "webhook format cannot be empty", err.Error())
	})

	t.Run("Disabled Config", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		cfg := &config.WebhookConfig{Enabled: false}
		client := mocks.NewMockHTTPClient(ctrl)

		service, err := NewWebhookStrategy(cfg, client)

		require.Error(t, err)
		assert.Nil(t, service)
		assert.Equal(t, "webhook strategy disabled", err.Error())
	})

	t.Run("Nil Config", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		client := mocks.NewMockHTTPClient(ctrl)

		service, err := NewWebhookStrategy(nil, client)

		require.Error(t, err)
		assert.Nil(t, service)
		assert.Equal(t, "webhook configuration cannot be nil", err.Error())
	})
}

func TestSend(t *testing.T) {
	task := models.Task{Id: "test-task-123"}

	tmpl, err := template.New("webhook").Parse(`{"id":"{{.Id}}"}`)
	require.NoError(t, err)

	t.Run("Successful Webhook", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockClient := mocks.NewMockHTTPClient(ctrl)
		mockClient.EXPECT().Do(gomock.Any()).DoAndReturn(func(req *http.Request) (*http.Response, error) {
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
		})

		service := &WebhookStrategy{
			url:                  "http://testhost/hook",
			token:                "secret-token",
			authorizationHeader:  "X-Auth",
			contentType:          "application/json",
			allowedResponseCodes: []int{200},
			client:               mockClient,
			template:             tmpl,
		}

		err := service.Send(task)

		assert.NoError(t, err)
	})

	t.Run("Failed Template Execution", func(t *testing.T) {
		invalidTmpl, err := template.New("webhook").Parse(`{"missing_field":"{{.Missing}}>"}`)
		require.NoError(t, err)

		service := &WebhookStrategy{
			template: invalidTmpl,
		}

		err = service.Send(task)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to execute webhook template")
	})

	t.Run("Failed Request Creation", func(t *testing.T) {
		service := &WebhookStrategy{
			url:      ":invalid-url:", // This will cause http.NewRequestWithContext to fail
			template: tmpl,
		}

		err := service.Send(task)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create webhook request")
	})

	t.Run("Client Throws Error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockClient := mocks.NewMockHTTPClient(ctrl)
		mockClient.EXPECT().Do(gomock.Any()).Return(nil, errors.New("network error"))

		service := &WebhookStrategy{
			url:      "http://testhost/hook",
			client:   mockClient,
			template: tmpl,
		}

		err := service.Send(task)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to send webhook: network error")
	})

	t.Run("Non-Allowed Status Code", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockClient := mocks.NewMockHTTPClient(ctrl)
		mockClient.EXPECT().Do(gomock.Any()).Return(&http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader(`{"error":"internal server error"}`)),
		}, nil)

		service := &WebhookStrategy{
			url:                  "http://testhost/hook",
			allowedResponseCodes: []int{200},
			client:               mockClient,
			template:             tmpl,
		}

		err := service.Send(task)

		require.Error(t, err)
		assert.Equal(t, "received non-allowed status code 500: {\"error\":\"internal server error\"}", err.Error())
	})

	t.Run("Non-Allowed Status Code with Body Read Error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		errorReader := &errorReader{err: errors.New("read error")}

		mockClient := mocks.NewMockHTTPClient(ctrl)
		mockClient.EXPECT().Do(gomock.Any()).Return(&http.Response{
			StatusCode: http.StatusForbidden,
			Body:       io.NopCloser(errorReader),
		}, nil)

		service := &WebhookStrategy{
			url:                  "http://testhost/hook",
			allowedResponseCodes: []int{200},
			client:               mockClient,
			template:             tmpl,
		}

		err := service.Send(task)

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

type NotificationStrategyFunc func(models.Task) error

func (f NotificationStrategyFunc) Send(task models.Task) error {
	return f(task)
}

type errorReader struct {
	err error
}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, r.err
}

// A JSON body is assembled by a text/template, which does no escaping of its
// own, so the values are escaped before rendering. Author, App and Project come
// from an unauthenticated submission, and StatusReason carries newlines and
// quoted operator messages built by internal/models.
func TestSendEscapesValuesForAJSONBody(t *testing.T) {
	renderBody := func(t *testing.T, format, contentType string, task models.Task) string {
		t.Helper()

		tmpl, err := template.New("webhook").Parse(format)
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		mockClient := mocks.NewMockHTTPClient(ctrl)

		var sent string
		mockClient.EXPECT().Do(gomock.Any()).DoAndReturn(func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			sent = string(body)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
		})

		service := &WebhookStrategy{
			url:                  "http://testhost/hook",
			contentType:          contentType,
			allowedResponseCodes: []int{200},
			client:               mockClient,
			template:             tmpl,
		}
		require.NoError(t, service.Send(task))

		return sent
	}

	t.Run("an author that closes the string cannot add keys", func(t *testing.T) {
		body := renderBody(t,
			`{"app": "{{.App}}", "author": "{{.Author}}"}`,
			"application/json",
			models.Task{App: "demo", Author: `x", "channel": "#alerts", "z": "`},
		)

		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(body), &parsed))
		assert.Equal(t, []string{"app", "author"}, sortedKeys(parsed))
		assert.Equal(t, `x", "channel": "#alerts", "z": "`, parsed["author"])
	})

	t.Run("a multi-line status reason stays valid JSON", func(t *testing.T) {
		reason := "Out-of-sync resources:\n\tDeployment/demo\n\nLast sync operation: Failed, message: \"one or more objects failed to apply\""
		body := renderBody(t,
			`{"text": "{{.App}}: {{.Status}}{{with .StatusReason}} — {{.}}{{end}}"}`,
			"application/json",
			models.Task{App: "demo", Status: "app not available", StatusReason: reason},
		)

		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(body), &parsed))
		assert.Contains(t, parsed["text"], reason)
	})

	t.Run("images are escaped too", func(t *testing.T) {
		body := renderBody(t,
			`{"images": [{{range $i, $img := .Images}}{{if $i}},{{end}}{"image": "{{$img.Image}}", "tag": "{{$img.Tag}}"}{{end}}]}`,
			"application/json",
			models.Task{Images: []models.Image{{Image: `evil", "x": "`, Tag: "v1"}}},
		)

		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(body), &parsed))
		images := parsed["images"].([]any)
		require.Len(t, images, 1)
		assert.Equal(t, `evil", "x": "`, images[0].(map[string]any)["image"])
	})

	t.Run("ordinary values render exactly as before", func(t *testing.T) {
		body := renderBody(t,
			`{"app": "{{.App}}", "author": "{{.Author}}"}`,
			"application/json",
			models.Task{App: "demo", Author: "ci-bot"},
		)

		assert.Equal(t, `{"app": "demo", "author": "ci-bot"}`, body)
	})

	// A receiver expecting something other than JSON must keep receiving the
	// literal text it always did.
	t.Run("a non-JSON body is left alone", func(t *testing.T) {
		body := renderBody(t,
			`{{.App}} deployed by {{.Author}}`,
			"text/plain",
			models.Task{App: "demo", Author: `someone "quoted"`},
		)

		assert.Equal(t, `demo deployed by someone "quoted"`, body)
	})
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
