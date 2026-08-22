package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/argocd"
	"github.com/shini4i/argo-watcher/internal/auth"
	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/lock"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/prometheus"
)

// submitTask posts body to the real router — the request-size middleware included —
// and returns the task the handler stored. The insert is failed on purpose: the
// success path would spawn the real rollout goroutine, and every field this file
// asserts on is already decided by then.
func submitTask(t *testing.T, body string, opts ...func(*config.ServerConfig)) (models.Task, *httptest.ResponseRecorder) {
	t.Helper()

	lockdown, err := NewLockdown("", lock.NewInMemoryDeployLockStore())
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	repo, _ := newRepo(ctrl)
	repo.EXPECT().Check().Return(true).AnyTimes()

	var stored models.Task
	repo.EXPECT().AddTask(gomock.Any()).DoAndReturn(func(task models.Task) (*models.Task, error) {
		stored = task
		return nil, fmt.Errorf("stop before the rollout goroutine")
	}).AnyTimes()

	argo := &argocd.Argo{}
	argo.Init(repo, newArgoAPI(ctrl), newMetrics(ctrl))

	serverConfig := &config.ServerConfig{DeploymentTimeout: 900, StaticFilePath: t.TempDir()}
	for _, opt := range opts {
		opt(serverConfig)
	}

	strategies := map[string]auth.AuthStrategy{}
	env := &Env{
		lockdown:      lockdown,
		strategies:    strategies,
		authenticator: auth.NewAuthenticator(strategies),
		argo:          argo,
		config:        serverConfig,
		metrics:       prometheus.NewMetrics(promclient.NewRegistry()),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.CreateRouter().ServeHTTP(w, req)

	return stored, w
}

// maxSizedPayload is the largest submission the bounds allow: every field at its cap.
// The body limit has to stay above it, or a legitimate deployment is refused.
func maxSizedPayload() string {
	name := strings.Repeat("i", models.MaxTaskFieldLength)
	images := make([]string, models.MaxTaskImages)
	for i := range images {
		images[i] = fmt.Sprintf(`{"image": %q, "tag": %q}`, name, name)
	}

	return fmt.Sprintf(`{"app": %q, "author": %q, "project": %q, "images": [%s]}`,
		name, name, name, strings.Join(images, ","))
}

func taskPayload(fields string) string {
	return `{"app": "test-app", "author": "test-author", "project": "test-project",` +
		` "images": [{"image": "test", "tag": "v1"}]` + fields + `}`
}

// TestAddTaskClampsTimeout pins the bound on a field nothing else limits: the
// submission endpoint takes no credential, and the timeout it accepts decides how
// long the watcher polls ArgoCD for that one request (issue #562).
func TestAddTaskClampsTimeout(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		expected  int
	}{
		{name: "an absurd timeout is clamped to the cap", requested: `, "timeout": 2000000000`, expected: maxTaskTimeout},
		{name: "exactly the cap is kept", requested: fmt.Sprintf(`, "timeout": %d`, maxTaskTimeout), expected: maxTaskTimeout},
		{name: "a timeout below the cap is kept", requested: `, "timeout": 1800`, expected: 1800},
		{name: "an omitted timeout falls back to the instance default", requested: "", expected: 900},
		{name: "a negative timeout falls back to the instance default", requested: `, "timeout": -1`, expected: 900},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored, _ := submitTask(t, taskPayload(tt.requested))
			assert.Equal(t, tt.expected, stored.Timeout)
		})
	}

	// The cap applies to the resolved window, not only to what the client asked for:
	// the instance default is what a submission carrying no timeout inherits.
	t.Run("an instance default above the cap is clamped too", func(t *testing.T) {
		stored, _ := submitTask(t, taskPayload(""), func(c *config.ServerConfig) {
			c.DeploymentTimeout = maxTaskTimeout + 1000
		})
		assert.Equal(t, maxTaskTimeout, stored.Timeout)
	})
}

// TestAddTaskRejectsTooManyImages covers the other unbounded field of the payload:
// every image is polled against the application's rollout status on every attempt.
func TestAddTaskRejectsTooManyImages(t *testing.T) {
	images := make([]string, models.MaxTaskImages+1)
	for i := range images {
		images[i] = fmt.Sprintf(`{"image": "test-%d", "tag": "v1"}`, i)
	}
	body := `{"app": "test-app", "author": "test-author", "project": "test-project", "images": [` +
		strings.Join(images, ",") + `]}`

	stored, w := submitTask(t, body)

	assert.Equal(t, http.StatusNotAcceptable, w.Code)
	assert.Empty(t, stored.App, "an over-long image list must not reach the state backend")
}

// TestAddTaskAcceptsThePayloadAtEveryCap is the other half of the bounds: a tightened
// tag would start refusing legitimate submissions, and only a test built from the
// constants catches a tag that drifts away from the constant it is documented by.
func TestAddTaskAcceptsThePayloadAtEveryCap(t *testing.T) {
	stored, w := submitTask(t, maxSizedPayload())

	require.Equal(t, http.StatusServiceUnavailable, w.Code, "the insert is failed on purpose, so anything else is a rejection")
	assert.Len(t, stored.Images, models.MaxTaskImages)
	assert.Len(t, stored.App, models.MaxTaskFieldLength)
	assert.Len(t, stored.Images[0].Image, models.MaxTaskFieldLength)
	assert.Len(t, stored.Images[0].Tag, models.MaxTaskFieldLength)
}

// TestAddTaskRejectsOverlongFields covers the fields the body cap alone leaves open:
// one 1 MiB app name is stored once and re-served to every reader of the task list.
func TestAddTaskRejectsOverlongFields(t *testing.T) {
	oversized := strings.Repeat("a", models.MaxTaskFieldLength+1)

	tests := []struct {
		name string
		body string
	}{
		{name: "app", body: fmt.Sprintf(`{"app": %q, "author": "a", "project": "p", "images": [{"image": "i", "tag": "v1"}]}`, oversized)},
		{name: "author", body: fmt.Sprintf(`{"app": "a", "author": %q, "project": "p", "images": [{"image": "i", "tag": "v1"}]}`, oversized)},
		{name: "project", body: fmt.Sprintf(`{"app": "a", "author": "a", "project": %q, "images": [{"image": "i", "tag": "v1"}]}`, oversized)},
		{name: "image name", body: fmt.Sprintf(`{"app": "a", "author": "a", "project": "p", "images": [{"image": %q, "tag": "v1"}]}`, oversized)},
		{name: "image tag", body: fmt.Sprintf(`{"app": "a", "author": "a", "project": "p", "images": [{"image": "i", "tag": %q}]}`, oversized)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored, w := submitTask(t, tt.body)

			assert.Equal(t, http.StatusNotAcceptable, w.Code)
			assert.Empty(t, stored.App, "an over-long field must not reach the state backend")
		})
	}
}

// TestApiRejectsOversizedBody pins the request-size cap on the API routes. It is what
// keeps an anonymous submission from being stored once and then re-served, amplified,
// to every reader of the task list.
func TestApiRejectsOversizedBody(t *testing.T) {
	// Valid JSON, only too much of it: the rejection must come from the size cap
	// rather than from a decoding error the payload would have caused anyway.
	body := `{"app": "` + strings.Repeat("a", maxRequestBodyBytes+1) + `"}`

	_, w := submitTask(t, body)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.Contains(t, w.Body.String(), "request body too large")
}

// TestApiAcceptsTheLargestLegitimateBody keeps the size cap above what the field
// bounds already allow — shrinking it would refuse a submission every other rule
// accepts.
func TestApiAcceptsTheLargestLegitimateBody(t *testing.T) {
	body := maxSizedPayload()
	require.Less(t, len(body), maxRequestBodyBytes)

	_, w := submitTask(t, body)

	assert.NotEqual(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.NotContains(t, w.Body.String(), "request body too large")
}

// TestWebSocketOutlivesServerWriteTimeout is the tripwire on StartRouter's read and
// write timeouts: a WebSocket is long-lived, and the connection it runs on is hijacked
// out of an ordinary request. It survives because the hijack clears the deadlines
// net/http set — which is not visible at the call site that adds a timeout.
func TestWebSocketOutlivesServerWriteTimeout(t *testing.T) {
	connectionsMutex.Lock()
	connections = nil
	closedConns = make(map[*websocket.Conn]bool)
	connectionsMutex.Unlock()

	env, _ := readAuthEnv(t, false, nil)
	env.config.DevEnvironment = true // accept the httptest origin

	server := httptest.NewUnstartedServer(env.CreateRouter())
	server.Config.ReadTimeout = 100 * time.Millisecond
	server.Config.WriteTimeout = 100 * time.Millisecond
	server.Start()

	t.Cleanup(func() {
		shutdownEnv(env)
		server.Close()
		connectionsMutex.Lock()
		connections = nil
		connectionsMutex.Unlock()
	})

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	ctx := t.Context()

	conn, _, err := websocket.Dial(ctx, url, nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "test done") }()

	// Past both deadlines the handshake inherited.
	time.Sleep(300 * time.Millisecond)

	notifyWebSocketClients("still alive")

	_, message, err := conn.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, "still alive", string(message))
}
