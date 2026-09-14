package notifications

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/mocks"
)

func TestValidateReceiverURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantErr string
	}{
		{name: "absolute http", rawURL: "http://example.com/hook"},
		{name: "absolute https", rawURL: "https://example.com/hook"},
		{name: "empty", rawURL: "", wantErr: "webhook url cannot be empty"},
		{name: "blank", rawURL: "   ", wantErr: "webhook url cannot be empty"},
		{name: "no scheme", rawURL: "example.com/hook", wantErr: "webhook url must be an http:// or https:// URL"},
		{name: "wrong scheme", rawURL: "ftp://example.com/hook", wantErr: "webhook url must be an http:// or https:// URL"},
		{name: "no host", rawURL: "http:///hook", wantErr: "webhook url is missing a host"},
		{name: "unparsable", rawURL: "http://exa mple.com", wantErr: "webhook url is not a valid URL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateReceiverURL("webhook", tt.rawURL)
			if tt.wantErr == "" {
				require.NoError(t, err)
				assert.Equal(t, strings.TrimSpace(tt.rawURL), got)
				return
			}
			require.Error(t, err)
			assert.Equal(t, tt.wantErr, err.Error())
		})
	}
}

// The receiver URL is itself the credential, so a rejection must describe the fault
// without reprinting the value: config errors are logged.
func TestValidateReceiverURL_KeepsTheURLOutOfTheError(t *testing.T) {
	_, err := validateReceiverURL("webhook", "ftp://user:hunter2@example.com/hook?token=s3cr3t")

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "hunter2")
	assert.NotContains(t, err.Error(), "s3cr3t")
	assert.NotContains(t, err.Error(), "example.com")
}

func TestDeliverPost(t *testing.T) {
	t.Run("returns the response body on an allowed status", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		client := mocks.NewMockHTTPClient(ctrl)
		client.EXPECT().Do(gomock.Any()).DoAndReturn(func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, http.MethodPost, req.Method)
			assert.Equal(t, "https://example.com/hook", req.URL.String())
			assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
			assert.Equal(t, "Bearer t", req.Header.Get("Authorization"))
			// The only thing stopping an unresponsive receiver holding a rollout open.
			deadline, bounded := req.Context().Deadline()
			assert.True(t, bounded, "a delivery must carry a deadline")
			assert.InDelta(t, notificationTimeout.Seconds(), time.Until(deadline).Seconds(), 5)
			body, _ := io.ReadAll(req.Body)
			assert.Equal(t, `{"a":1}`, string(body))
			return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(`{"id":"p1"}`))}, nil
		})

		body, err := deliverPost(client, "mattermost", "https://example.com/hook",
			map[string]string{"Content-Type": "application/json", "Authorization": "Bearer t"},
			[]byte(`{"a":1}`), []int{http.StatusCreated})

		require.NoError(t, err)
		assert.JSONEq(t, `{"id":"p1"}`, string(body))
	})

	t.Run("reports a disallowed status with the response body", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		client := mocks.NewMockHTTPClient(ctrl)
		client.EXPECT().Do(gomock.Any()).Return(&http.Response{
			StatusCode: http.StatusTeapot,
			Body:       io.NopCloser(strings.NewReader("nope")),
		}, nil)

		_, err := deliverPost(client, "webhook", "https://example.com/hook", nil, nil, []int{http.StatusOK})

		require.Error(t, err)
		assert.Equal(t, "webhook returned status code 418: nope", err.Error())
	})

	t.Run("truncates an oversized error body", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		client := mocks.NewMockHTTPClient(ctrl)
		client.EXPECT().Do(gomock.Any()).Return(&http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", maxErrorBodySize*2))),
		}, nil)

		_, err := deliverPost(client, "webhook", "https://example.com/hook", nil, nil, []int{http.StatusOK})

		require.Error(t, err)
		assert.Len(t, err.Error(), len("webhook returned status code 500: ")+maxErrorBodySize)
	})

	// The only thing stopping an untrusted receiver making the watcher read an
	// unbounded body into memory on a delivery it accepted.
	t.Run("caps an oversized accepted body", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		client := mocks.NewMockHTTPClient(ctrl)
		client.EXPECT().Do(gomock.Any()).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", maxResponseBodySize*2))),
		}, nil)

		body, err := deliverPost(client, "webhook", "https://example.com/hook", nil, nil, []int{http.StatusOK})

		require.NoError(t, err)
		assert.Len(t, body, maxResponseBodySize)
	})

	// net/http reports a transport failure as a *url.Error quoting the whole URL.
	t.Run("keeps the URL out of a transport error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		client := mocks.NewMockHTTPClient(ctrl)
		client.EXPECT().Do(gomock.Any()).Return(nil, &url.Error{
			Op:  "Post",
			URL: "https://example.com/hook?token=s3cr3t",
			Err: errors.New("connection refused"),
		})

		_, err := deliverPost(client, "webhook", "https://example.com/hook?token=s3cr3t", nil, nil, []int{http.StatusOK})

		require.Error(t, err)
		assert.NotContains(t, err.Error(), "s3cr3t")
		assert.Contains(t, err.Error(), "connection refused")
	})
}

// A URL with stray whitespace must be corrected at construction, not left to fail
// http.NewRequestWithContext on every single send.
func TestNewStrategies_StoreTheTrimmedURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := mocks.NewMockHTTPClient(ctrl)

	webhook, err := NewWebhookStrategy(&config.WebhookConfig{
		Enabled: true,
		Url:     "  https://example.com/hook  ",
		Format:  `{"id":"{{.Id}}"}`,
	}, client)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/hook", webhook.url)

	mattermost, err := NewMattermostStrategy(&config.MattermostConfig{
		Enabled:   true,
		Url:       "  https://mm.example.com/  ",
		Token:     "t",
		ChannelId: "c",
		Format:    "{{.Id}}",
	}, client)
	require.NoError(t, err)
	assert.Equal(t, "https://mm.example.com", mattermost.baseURL)
}
