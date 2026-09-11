package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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

	// Pins the one incompatibility: a template that quotes the value itself now
	// double-escapes, because the value reaches it already escaped. Such a format
	// has to drop its own quoting — see the migration note in the notifications
	// guide.
	t.Run("a template that quotes the value itself double-escapes", func(t *testing.T) {
		body := renderBody(t,
			`{"text": {{printf "%q" .StatusReason}}}`,
			"application/json",
			models.Task{StatusReason: "line one\nline two"},
		)

		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(body), &parsed))
		assert.Equal(t, `line one\nline two`, parsed["text"],
			"the value arrives escaped, so the template's own quoting escapes it again")
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

func TestIsJSONBody(t *testing.T) {
	tests := map[string]bool{
		"application/json":                  true,
		"application/json; charset=utf-8":   true,
		"APPLICATION/JSON":                  true,
		"application/vnd.api+json":          true,
		"application/problem+json":          true,
		"text/plain":                        false,
		"text/plain; profile=json":          false,
		"application/x-www-form-urlencoded": false,
		"":                                  false,
		"not a media type":                  false,
	}

	for contentType, want := range tests {
		assert.Equal(t, want, isJSONBody(contentType), "content type %q", contentType)
	}
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// The webhook URL is the credential (config.WebhookConfig keeps it out of
// GET /api/v1/config for that reason), and the transport reports a failure as a
// *url.Error quoting the whole URL. One timeout would otherwise log the secret.
func TestSendDoesNotLeakTheWebhookURL(t *testing.T) {
	const secretURL = "https://hooks.example.com/services/T000/B000/SuPerSecreT"

	tmpl := template.Must(template.New("webhook").Parse(`{"id":"{{.Id}}"}`))
	task := models.Task{Id: "task-id", App: "demo"}

	// Every shape the transport actually returns: the inner cause differs, the
	// *url.Error wrapper carrying the URL does not.
	testCases := []struct {
		name   string
		cause  error
		wantIs error
	}{
		{name: "timeout", cause: context.DeadlineExceeded, wantIs: context.DeadlineExceeded},
		{name: "connection refused", cause: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}},
		{name: "dns", cause: &net.DNSError{Err: "no such host", Name: "hooks.example.com"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockClient := mocks.NewMockHTTPClient(ctrl)
			mockClient.EXPECT().Do(gomock.Any()).
				Return(nil, &url.Error{Op: "Post", URL: secretURL, Err: tc.cause})

			service := &WebhookStrategy{url: secretURL, client: mockClient, template: tmpl}

			err := service.Send(task)

			require.Error(t, err)
			assert.NotContains(t, err.Error(), "SuPerSecreT", "the secret path must never reach the error")
			assert.NotContains(t, err.Error(), secretURL)
			// Still diagnosable: the operator must be able to tell these apart.
			assert.Contains(t, err.Error(), tc.cause.Error())
			// The chain must survive the redaction, or a caller can no longer classify it.
			if tc.wantIs != nil {
				assert.ErrorIs(t, err, tc.wantIs)
			}
		})
	}
}

// A malformed URL fails in http.NewRequest, whose *url.Error quotes the value it
// rejected — the same leak by a different route.
func TestSendDoesNotLeakTheWebhookURLOnAMalformedURL(t *testing.T) {
	tmpl := template.Must(template.New("webhook").Parse(`{"id":"{{.Id}}"}`))
	service := &WebhookStrategy{
		url:      "https://hooks.example.com/services/SuPerSecreT\n",
		client:   unusedHTTPClient(t),
		template: tmpl,
	}

	err := service.Send(models.Task{Id: "task-id", App: "demo"})

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "SuPerSecreT")
}

// A cause may itself be a *url.Error — an HTTPClient that retries and wraps, say. Stripping
// only the outermost one leaves the nested URL to be quoted when the cause is formatted, so
// every layer is stripped and only the operations survive.
func TestSendDoesNotLeakANestedURLError(t *testing.T) {
	tmpl := template.Must(template.New("webhook").Parse(`{"id":"{{.Id}}"}`))
	root := errors.New("connect: connection refused")
	inner := &url.Error{Op: "Get", URL: "https://hooks.example.com/services/InnerSecreT", Err: root}
	outer := &url.Error{Op: "Post", URL: "https://hooks.example.com/services/OuterSecreT", Err: inner}

	ctrl := gomock.NewController(t)
	mockClient := mocks.NewMockHTTPClient(ctrl)
	mockClient.EXPECT().Do(gomock.Any()).Return(nil, outer)

	service := &WebhookStrategy{url: outer.URL, client: mockClient, template: tmpl}

	err := service.Send(models.Task{Id: "task-id", App: "demo"})

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "OuterSecreT")
	assert.NotContains(t, err.Error(), "InnerSecreT")
	// Both operations and the root cause are still reported.
	assert.Contains(t, err.Error(), "Post")
	assert.Contains(t, err.Error(), "Get")
	assert.ErrorIs(t, err, root)
}

// A receiver that answers with a redirect must not be able to harvest the credential.
// Go strips Authorization on a cross-host hop but not a header the operator named itself,
// so the notification client refuses redirects outright.
func TestWebhookClientDoesNotFollowRedirects(t *testing.T) {
	var attackerSaw string
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attackerSaw = r.Header.Get("X-Hook-Secret")
	}))
	defer attacker.Close()

	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, attacker.URL, http.StatusFound)
	}))
	defer receiver.Close()

	strategy, err := NewWebhookStrategy(&config.WebhookConfig{
		Enabled:              true,
		Url:                  receiver.URL,
		Format:               `{"id":"{{.Id}}"}`,
		ContentType:          "application/json",
		AuthorizationHeader:  "X-Hook-Secret",
		Token:                "SuPerSecreT",
		AllowedResponseCodes: []int{200},
	}, NewNotificationHTTPClient())
	require.NoError(t, err)

	// The 302 is not in AllowedResponseCodes, so delivery fails — which is the point:
	// the redirect is reported, not followed.
	err = strategy.Send(models.Task{Id: "task-id", App: "demo"})

	require.Error(t, err)
	// The operator-facing signal the troubleshooting guide promises: the redirect code itself.
	assert.ErrorContains(t, err, "received non-allowed status code 302")
	assert.Empty(t, attackerSaw, "the redirect target must never receive the credential")
}
