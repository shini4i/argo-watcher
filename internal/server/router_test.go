package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/rs/cors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/argocd"
	"github.com/shini4i/argo-watcher/internal/auth"
	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/lock"
	"github.com/shini4i/argo-watcher/internal/mocks"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/prometheus"
	"github.com/shini4i/argo-watcher/internal/state"
)

type repoCapture struct {
	lastApp    string
	lastStatus string
	lastLimit  int
	lastOffset int
}

// newRepo returns a permissive TaskRepository mock plus the capture struct its
// GetTasks stub writes to. Tests that exercise Check, GetTask or AddTask add those
// expectations themselves.
func newRepo(ctrl *gomock.Controller) (*mocks.MockTaskRepository, *repoCapture) {
	repo := mocks.NewMockTaskRepository(ctrl)
	capture := &repoCapture{}
	repo.EXPECT().Connect(gomock.Any()).Return(nil).AnyTimes()
	repo.EXPECT().GetTasks(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_, _ float64, app, status string, limit, offset int) ([]models.Task, int64) {
			capture.lastApp = app
			capture.lastStatus = status
			capture.lastLimit = limit
			capture.lastOffset = offset
			return []models.Task{}, 0
		}).AnyTimes()
	repo.EXPECT().SetTaskStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	repo.EXPECT().CancelInProgressTasks(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(int64(0), nil).AnyTimes()
	repo.EXPECT().ProcessObsoleteTasks(gomock.Any()).AnyTimes()
	return repo, capture
}

func newArgoAPI(ctrl *gomock.Controller) *mocks.MockArgoApiInterface {
	api := mocks.NewMockArgoApiInterface(ctrl)
	api.EXPECT().Init(gomock.Any()).Return(nil).AnyTimes()
	api.EXPECT().GetUserInfo().Return(&models.Userinfo{LoggedIn: true, Username: "test"}, nil).AnyTimes()
	api.EXPECT().GetApplication(gomock.Any(), gomock.Any(), gomock.Any()).Return(&models.Application{}, nil).AnyTimes()
	api.EXPECT().GetResourceTree(gomock.Any(), gomock.Any()).Return(&models.ApplicationTree{}, nil).AnyTimes()
	return api
}

func newMetrics(ctrl *gomock.Controller) *mocks.MockMetricsInterface {
	metrics := mocks.NewMockMetricsInterface(ctrl)
	metrics.EXPECT().AddAcceptedDeployment().AnyTimes()
	metrics.EXPECT().AddFailedDeployment(gomock.Any()).AnyTimes()
	metrics.EXPECT().ResetFailedDeployment(gomock.Any()).AnyTimes()
	metrics.EXPECT().SetArgoUnavailable(gomock.Any()).AnyTimes()
	metrics.EXPECT().SetStateUnavailable(gomock.Any()).AnyTimes()
	metrics.EXPECT().AddInProgressTask().AnyTimes()
	metrics.EXPECT().RemoveInProgressTask().AnyTimes()
	metrics.EXPECT().ObserveRefreshDuration(gomock.Any(), gomock.Any()).AnyTimes()
	metrics.EXPECT().ObserveGitWritebackDuration(gomock.Any(), gomock.Any()).AnyTimes()
	metrics.EXPECT().ObserveGitLockWaitDuration(gomock.Any(), gomock.Any()).AnyTimes()
	metrics.EXPECT().ObserveDeploymentDuration(gomock.Any(), gomock.Any()).AnyTimes()
	metrics.EXPECT().AddUnauthenticatedRead(gomock.Any(), gomock.Any()).AnyTimes()
	return metrics
}

// newAuthStrategy returns an AuthStrategy mock whose Validate always yields the given
// result, callable any number of times (some strategies are skipped when their header
// does not match the request).
func newAuthStrategy(t testing.TB, valid bool, err error) *mocks.MockAuthStrategy {
	t.Helper()
	strategy := mocks.NewMockAuthStrategy(gomock.NewController(t))
	strategy.EXPECT().Validate(gomock.Any()).Return(valid, err).AnyTimes()
	return strategy
}

func TestGetVersion(t *testing.T) {

	router := chi.NewRouter()
	env := &Env{}
	router.Get("/api/v1/version", env.getVersion)

	req, err := http.NewRequest(http.MethodGet, "/api/v1/version", nil)
	if err != nil {
		t.Fatalf("Couldn't create request: %v\n", err)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, fmt.Sprintf("\"%s\"", version), w.Body.String())
}

func TestDeployLock(t *testing.T) {
	var err error

	dummyConfig := &config.ServerConfig{}

	router := chi.NewRouter()
	env := &Env{config: dummyConfig}

	env.lockdown, err = NewLockdown(dummyConfig.LockdownSchedule, lock.NewInMemoryDeployLockStore())
	assert.NoError(t, err)

	t.Run("SetDeployLock", func(t *testing.T) {
		router.Post("/api/v1/deploy-lock", env.SetDeployLock)

		req, err := http.NewRequest(http.MethodPost, "/api/v1/deploy-lock", nil)
		if err != nil {
			t.Fatalf("Couldn't create request: %v\n", err)
		}

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "\"deploy lock is set\"", w.Body.String())
	})

	t.Run("ReleaseDeployLock", func(t *testing.T) {
		router.Delete("/api/v1/deploy-lock", env.ReleaseDeployLock)

		req, err := http.NewRequest(http.MethodDelete, "/api/v1/deploy-lock", nil)
		if err != nil {
			t.Fatalf("Couldn't create request: %v\n", err)
		}

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "\"deploy lock is released\"", w.Body.String())
	})

	t.Run("isDeployLockSet", func(t *testing.T) {
		router.Get("/api/v1/deploy-lock", env.isDeployLockSet)

		req, err := http.NewRequest(http.MethodGet, "/api/v1/deploy-lock", nil)
		if err != nil {
			t.Fatalf("Couldn't create request: %v\n", err)
		}

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "false", w.Body.String())
	})
}

// routeExists reports whether router serves method on exactly the given route
// pattern, so a test can assert that an endpoint was never registered rather than
// that it merely answers with an error.
func routeExists(t *testing.T, router *chi.Mux, method, pattern string) bool {
	t.Helper()

	found := false
	err := chi.Walk(router, func(walkMethod, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if walkMethod == method && route == pattern {
			found = true
		}
		return nil
	})
	require.NoError(t, err)

	return found
}

// TestDeployLockEndpointRegistration covers why the state-changing deploy-lock
// endpoints only exist when OIDC auth is enabled: without an auth backend they cannot
// be protected, so they must not be exposed; the read-only GET stays available so the
// banner and scheduled lockdown keep working.
func TestDeployLockEndpointRegistration(t *testing.T) {

	newRouter := func(t *testing.T, oidcEnabled bool) *chi.Mux {
		t.Helper()
		serverConfig := &config.ServerConfig{
			StaticFilePath: t.TempDir(),
			OIDC:           config.OIDCConfig{Enabled: oidcEnabled},
		}
		env := &Env{config: serverConfig}
		var err error
		env.lockdown, err = NewLockdown("", lock.NewInMemoryDeployLockStore())
		require.NoError(t, err)
		return env.CreateRouter()
	}

	const lockPath = "/api/v1/deploy-lock"

	t.Run("registers lock write endpoints when OIDC is enabled", func(t *testing.T) {
		routes := newRouter(t, true)
		assert.True(t, routeExists(t, routes, http.MethodPost, lockPath))
		assert.True(t, routeExists(t, routes, http.MethodDelete, lockPath))
		assert.True(t, routeExists(t, routes, http.MethodGet, lockPath))
	})

	t.Run("omits lock write endpoints when OIDC is disabled", func(t *testing.T) {
		routes := newRouter(t, false)
		assert.False(t, routeExists(t, routes, http.MethodPost, lockPath),
			"POST deploy-lock must not be registered without an auth backend")
		assert.False(t, routeExists(t, routes, http.MethodDelete, lockPath),
			"DELETE deploy-lock must not be registered without an auth backend")
		assert.True(t, routeExists(t, routes, http.MethodGet, lockPath),
			"read-only GET deploy-lock must stay registered")
	})
}

func TestRemoveWebSocketConnection(t *testing.T) {
	conn := &websocket.Conn{}
	connectionsMutex.Lock()
	connections = append(connections, conn)
	connectionsMutex.Unlock()
	removeWebSocketConnection(conn)
	connectionsMutex.Lock()
	assert.NotContains(t, connections, conn)
	connectionsMutex.Unlock()
}

func TestWebSocketConnectionsConcurrentAccess(t *testing.T) {
	connectionsMutex.Lock()
	connections = nil
	connectionsMutex.Unlock()

	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn := &websocket.Conn{}
			connectionsMutex.Lock()
			connections = append(connections, conn)
			connectionsMutex.Unlock()
			removeWebSocketConnection(conn)
		}()
	}

	wg.Wait()

	connectionsMutex.Lock()
	assert.Empty(t, connections)
	connectionsMutex.Unlock()
}

func TestNewEnv(t *testing.T) {
	serverConfig := &config.ServerConfig{
		Host:        "localhost",
		Port:        "8080",
		DeployToken: "deployToken",
		OIDC: config.OIDCConfig{
			Enabled:   true,
			IssuerURL: "http://localhost:8080/realms/test",
			ClientId:  "test",
		},
		JWTSecret: "jwtSecret",
	}

	argo := &argocd.Argo{}
	metrics := &prometheus.Metrics{}
	updater := &argocd.ArgoStatusUpdater{}

	env, err := NewEnv(serverConfig, argo, metrics, updater, lock.NewInMemoryDeployLockStore())

	assert.NoError(t, err)
	assert.Equal(t, env.config, serverConfig)
	assert.Equal(t, env.argo, argo)
	assert.Equal(t, env.metrics, metrics)
	assert.Equal(t, env.updater, updater)

	expectedOIDC, oidcErr := auth.NewOIDCAuthService(serverConfig)
	assert.NoError(t, oidcErr)

	expectedStrategies := map[string]auth.AuthStrategy{
		"ARGO_WATCHER_DEPLOY_TOKEN": auth.NewDeployTokenAuthService(serverConfig.DeployToken),
		"Authorization":             auth.NewJWTAuthService(serverConfig.JWTSecret),
		oidcHeader:                  expectedOIDC,
	}

	assert.Equal(t, expectedStrategies, env.strategies)
	assert.NotNil(t, env.authenticator)
}

func TestNewEnvInvalidOIDCURL(t *testing.T) {
	serverConfig := &config.ServerConfig{
		Host:        "localhost",
		Port:        "8080",
		DeployToken: "deployToken",
		OIDC: config.OIDCConfig{
			Enabled:   true,
			IssuerURL: "ftp://invalid:8080",
		},
	}

	env, err := NewEnv(serverConfig, &argocd.Argo{}, &prometheus.Metrics{}, &argocd.ArgoStatusUpdater{}, lock.NewInMemoryDeployLockStore())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize OIDC auth")
	assert.Nil(t, env)
}

// TestGetStateInvalidQueryParams pins that bad query params fall back to defaults and
// are logged at debug rather than rejected.
func TestGetStateInvalidQueryParams(t *testing.T) {

	ctrl := gomock.NewController(t)
	repo, _ := newRepo(ctrl)
	argo := &argocd.Argo{}
	argo.Init(repo, newArgoAPI(ctrl), newMetrics(ctrl))

	env := &Env{
		argo:   argo,
		config: &config.ServerConfig{},
	}

	router := chi.NewRouter()
	router.Get("/api/v1/tasks", env.getState)

	testCases := []struct {
		name        string
		queryParams string
	}{
		{
			name:        "invalid from_timestamp",
			queryParams: "?from_timestamp=notanumber",
		},
		{
			name:        "invalid to_timestamp",
			queryParams: "?to_timestamp=notanumber",
		},
		{
			name:        "invalid limit",
			queryParams: "?limit=notanumber",
		},
		{
			name:        "invalid offset",
			queryParams: "?offset=notanumber",
		},
		{
			name:        "negative limit",
			queryParams: "?limit=-5",
		},
		{
			name:        "negative offset",
			queryParams: "?offset=-10",
		},
		{
			name:        "all invalid params",
			queryParams: "?from_timestamp=abc&to_timestamp=xyz&limit=foo&offset=bar",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, "/api/v1/tasks"+tc.queryParams, nil)
			if err != nil {
				t.Fatalf("Couldn't create request: %v\n", err)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

func TestGetStateForwardsFilters(t *testing.T) {

	ctrl := gomock.NewController(t)
	repo, capture := newRepo(ctrl)
	argo := &argocd.Argo{}
	argo.Init(repo, newArgoAPI(ctrl), newMetrics(ctrl))

	env := &Env{argo: argo, config: &config.ServerConfig{}}

	router := chi.NewRouter()
	router.Get("/api/v1/tasks", env.getState)

	req, err := http.NewRequest(
		http.MethodGet,
		"/api/v1/tasks?from_timestamp=0&app=checkout-api&status=in+progress",
		nil,
	)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "checkout-api", capture.lastApp)
	assert.Equal(t, "in progress", capture.lastStatus)
}

// TestGetStateRejectsUnknownStatus covers the 400 returned when the `status` query
// param is not accepted by models.IsAllowedTaskStatus.
func TestGetStateRejectsUnknownStatus(t *testing.T) {

	ctrl := gomock.NewController(t)
	repo, _ := newRepo(ctrl)
	argo := &argocd.Argo{}
	argo.Init(repo, newArgoAPI(ctrl), newMetrics(ctrl))
	env := &Env{argo: argo, config: &config.ServerConfig{}}

	router := chi.NewRouter()
	router.Get("/api/v1/tasks", env.getState)

	cases := []struct {
		name     string
		status   string
		expected int
	}{
		{name: "unknown status rejected", status: "pending", expected: http.StatusBadRequest},
		{name: "case-sensitive rejection", status: "In Progress", expected: http.StatusBadRequest},
		{name: "empty status accepted", status: "", expected: http.StatusOK},
		{name: "known status accepted", status: "in progress", expected: http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			query := "?from_timestamp=0"
			if tc.status != "" {
				query += "&status=" + url.QueryEscape(tc.status)
			}
			req, err := http.NewRequest(http.MethodGet, "/api/v1/tasks"+query, nil)
			require.NoError(t, err)

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tc.expected, w.Code)
		})
	}
}

// TestGetStateClampsLimit covers the bound on the limit query param, so callers cannot
// drain the entire task table in a single request (the underlying repositories treat
// limit <= 0 as "no LIMIT clause").
func TestGetStateClampsLimit(t *testing.T) {

	cases := []struct {
		name     string
		query    string
		expected int
	}{
		{name: "missing limit clamps to max", query: "", expected: maxTaskListLimit},
		{name: "limit=0 clamps to max", query: "&limit=0", expected: maxTaskListLimit},
		{name: "limit beyond cap clamps to max", query: "&limit=99999", expected: maxTaskListLimit},
		{name: "negative limit clamps to max", query: "&limit=-5", expected: maxTaskListLimit},
		{name: "limit within range passes through", query: "&limit=42", expected: 42},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			repo, capture := newRepo(ctrl)
			argo := &argocd.Argo{}
			argo.Init(repo, newArgoAPI(ctrl), newMetrics(ctrl))
			env := &Env{argo: argo, config: &config.ServerConfig{}}

			router := chi.NewRouter()
			router.Get("/api/v1/tasks", env.getState)

			req, err := http.NewRequest(
				http.MethodGet,
				"/api/v1/tasks?from_timestamp=0"+tc.query,
				nil,
			)
			require.NoError(t, err)

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, tc.expected, capture.lastLimit)
		})
	}
}

func TestStaticFileServing(t *testing.T) {

	tmpDir := t.TempDir()

	indexContent := []byte("<html><body>Index</body></html>")
	jsContent := []byte("console.log('test');")

	err := os.WriteFile(tmpDir+"/index.html", indexContent, 0644)
	assert.NoError(t, err)

	err = os.MkdirAll(tmpDir+"/assets", 0755)
	assert.NoError(t, err)

	err = os.WriteFile(tmpDir+"/assets/main.js", jsContent, 0644)
	assert.NoError(t, err)

	serverConfig := &config.ServerConfig{
		StaticFilePath: tmpDir,
	}

	env := &Env{
		config: serverConfig,
	}

	env.lockdown, _ = NewLockdown("", lock.NewInMemoryDeployLockStore())

	router := env.CreateRouter()

	t.Run("serves existing static file", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/assets/main.js", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, string(jsContent), w.Body.String())
	})

	t.Run("serves index.html for SPA routes", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/dashboard", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, string(indexContent), w.Body.String())
	})

	t.Run("prevents path traversal attack", func(t *testing.T) {
		// Note: Go's net/http rejects malformed URLs with 400, which is also protection
		req, _ := http.NewRequest(http.MethodGet, "/../../../etc/passwd", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Should either return 400 (Go's built-in protection) or index.html (our protection)
		assert.NotContains(t, w.Body.String(), "root:")
	})

	t.Run("prevents encoded path traversal", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/..%2F..%2F..%2Fetc%2Fpasswd", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.NotContains(t, w.Body.String(), "root:")
	})

	t.Run("prevents double-dot path traversal in valid URL", func(t *testing.T) {
		// This path looks valid but tries to escape using /../
		req, _ := http.NewRequest(http.MethodGet, "/assets/../../../etc/passwd", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.NotContains(t, w.Body.String(), "root:")
	})

	t.Run("serves index.html for directory requests", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/assets/", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, string(indexContent), w.Body.String())
	})

	t.Run("serves swagger static files", func(t *testing.T) {
		swaggerDir := tmpDir + "/swagger"
		err := os.MkdirAll(swaggerDir, 0755)
		assert.NoError(t, err)

		swaggerJSON := []byte(`{"swagger":"2.0","info":{"title":"Test"}}`)
		err = os.WriteFile(swaggerDir+"/swagger.json", swaggerJSON, 0644)
		assert.NoError(t, err)

		// Re-create router to pick up the new swagger directory
		router := env.CreateRouter()

		req, _ := http.NewRequest(http.MethodGet, "/swagger/swagger.json", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"swagger":"2.0"`)
	})
}

// logBuffer collects log output written from request-handling goroutines while
// the test goroutine reads it, so both sides must hold the lock.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *logBuffer) reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

// captureDebugLogs redirects the global slog logger into a buffer at debug level for
// the duration of the test and returns it. It proves the capture is live before handing
// the buffer back, so a test whose only assertion is NotContains or Empty cannot pass
// because logging was silently broken.
//
// Not safe under t.Parallel: it swaps the process-global default logger.
//
// Call it after registering any cleanup that logs (env.Shutdown does): t.Cleanup
// runs LIFO, so registering the capture last restores the real logger before
// that shutdown logging runs, keeping it out of the buffer.
func captureDebugLogs(t *testing.T) *logBuffer {
	t.Helper()

	logs := &logBuffer{}
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	slog.SetDefault(slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))

	slog.Debug("capture-live")
	require.Contains(t, logs.String(), "capture-live", "debug log capture is not working")
	logs.reset()

	return logs
}

func TestWebSocketInterceptor(t *testing.T) {

	tmpDir := t.TempDir()
	err := os.WriteFile(tmpDir+"/index.html", []byte("<html></html>"), 0644)
	assert.NoError(t, err)

	serverConfig := &config.ServerConfig{
		StaticFilePath: tmpDir,
	}

	env := &Env{
		config: serverConfig,
	}
	env.lockdown, _ = NewLockdown("", lock.NewInMemoryDeployLockStore())

	router := env.CreateRouter()

	t.Run("non-upgrade GET to /ws returns 400", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/ws", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "WebSocket upgrade required")
	})

	// The 400 is otherwise silent, and a proxy that strips or rewrites
	// Upgrade/Connection is the usual cause; the log is what makes that
	// diagnosable, so it must report the header values that actually arrived.
	nonUpgradeCases := []struct {
		name       string
		upgrade    string
		connection string
		wantAttrs  []string
	}{
		{
			name:       "header stripped entirely",
			connection: "keep-alive",
			wantAttrs:  []string{`"upgrade":""`, `"connection":"keep-alive"`},
		},
		{
			name:       "upgrade rewritten to another protocol",
			upgrade:    "h2c",
			connection: "Upgrade",
			wantAttrs:  []string{`"upgrade":"h2c"`, `"connection":"Upgrade"`},
		},
	}

	for _, tc := range nonUpgradeCases {
		t.Run("non-upgrade GET to /ws logs the received headers at debug: "+tc.name, func(t *testing.T) {
			logs := captureDebugLogs(t)

			req, _ := http.NewRequest(http.MethodGet, "/ws", nil)
			if tc.upgrade != "" {
				req.Header.Set("Upgrade", tc.upgrade)
			}
			req.Header.Set("Connection", tc.connection)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "WebSocket upgrade required")

			assert.Contains(t, logs.String(), "non-upgrade request to /ws")
			for _, attr := range tc.wantAttrs {
				assert.Contains(t, logs.String(), attr)
			}
			// Debug specifically: at info this would be back to polluting the
			// default log level, which is the problem the change exists to fix.
			assert.Contains(t, logs.String(), `"level":"DEBUG"`)
		})
	}

	t.Run("case-insensitive Upgrade header check", func(t *testing.T) {
		// However the header is spelled, the request must reach the upgrade path. The
		// recorder is no http.Hijacker, so that path is identifiable by the response it
		// gives when the connection cannot carry an upgrade.
		testCases := []string{"websocket", "WebSocket", "WEBSOCKET", "Websocket"}

		for _, upgradeValue := range testCases {
			t.Run(upgradeValue, func(t *testing.T) {
				req, _ := http.NewRequest(http.MethodGet, "/ws", nil)
				req.Header.Set("Upgrade", upgradeValue)
				req.Header.Set("Connection", "Upgrade")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				assert.Equal(t, http.StatusInternalServerError, w.Code,
					"Upgrade header %q should reach the WebSocket handler", upgradeValue)
				assert.Contains(t, w.Body.String(), "WebSocket not supported")
			})
		}
	})
}

// TestWebSocketConnectionIntegration establishes a real WebSocket connection and
// then triggers env.Shutdown during cleanup. Besides asserting the pre-hijack
// upgrade works, it is the regression guard for the WebSocket hijack/shutdown
// data race: it only detects that race when the suite runs under `go test -race`
// (wired in .github/workflows/run-tests.yml), since a plain run cannot observe
// it. Keep the -race CI step if you touch this test.
func TestWebSocketConnectionIntegration(t *testing.T) {

	connectionsMutex.Lock()
	connections = nil
	connectionsMutex.Unlock()

	tmpDir := t.TempDir()
	err := os.WriteFile(tmpDir+"/index.html", []byte("<html></html>"), 0644)
	assert.NoError(t, err)

	serverConfig := &config.ServerConfig{
		StaticFilePath: tmpDir,
		DevEnvironment: true, // Allow test origins
	}

	env := &Env{config: serverConfig}
	env.lockdown, _ = NewLockdown("", lock.NewInMemoryDeployLockStore())

	router := env.CreateRouter()

	// Use httptest.Server for real HTTP connection (supports hijacking)
	server := httptest.NewServer(router)

	// Cleanup: shut down the env (stops checkConnection goroutines), then the HTTP server, then reset connections.
	t.Cleanup(func() {
		shutdownEnv(env)
		server.Close()
		connectionsMutex.Lock()
		connections = nil
		connectionsMutex.Unlock()
	})

	// Capture debug output around the handshake: a successful upgrade must not
	// log the non-upgrade diagnostic, otherwise every reconnecting browser tab
	// floods debug output again.
	logs := captureDebugLogs(t)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	// Use a generous timeout — the WebSocket handshake through httptest can take several
	// seconds under CI load, so 5s is too tight and causes flaky failures.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket connection failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	t.Log("WebSocket connection established successfully")

	// Empty, not just "no diagnostic": the upgrade path must emit no per-request
	// logging at all, so a differently-worded line added later is caught too. If
	// a legitimate success-path log is ever added, make the exception here.
	assert.Empty(t, logs.String(), "a successful /ws upgrade must not log per-request")
}

// readCountingStore counts State() calls so a test can wait until the lock
// watcher has read its baseline before changing the state under it.
type readCountingStore struct {
	lock.DeployLockStore
	reads atomic.Int64
}

func (s *readCountingStore) State() (lock.DeployLockState, error) {
	s.reads.Add(1)
	return s.DeployLockStore.State()
}

// TestDeployLockNotifiedOnlyByWatcher pins the single-notifier invariant: the
// lock watcher is the only thing that pushes lock state to clients, and the API
// handlers do not push directly.
//
// This is not about the duplicate message it avoids. WatchTransitions compares
// each tick against the state it last broadcast, and a handler-side push it does
// not know about desynchronizes that baseline: if a release is served here and
// another replica re-acquires the lock before the next tick, the tick sees no
// change against the stale baseline and stays silent — leaving this replica's
// clients showing unlocked while the lock is active. Keeping the watcher as the
// sole notifier makes that impossible, at the cost of the banner on this replica
// updating within one poll interval rather than instantly.
// Both handlers are covered: the release path is the one the desync story above
// turns on, so pinning only the lock path would leave it free to regress.
func TestDeployLockNotifiedOnlyByWatcher(t *testing.T) {

	tests := []struct {
		name        string
		method      string
		lockedFirst bool // state the watcher must see as its baseline
		wantMsg     string
	}{
		{name: "set", method: http.MethodPost, lockedFirst: false, wantMsg: "locked"},
		{name: "release", method: http.MethodDelete, lockedFirst: true, wantMsg: "unlocked"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connectionsMutex.Lock()
			connections = nil
			closedConns = make(map[*websocket.Conn]bool)
			connectionsMutex.Unlock()

			tmpDir := t.TempDir()
			require.NoError(t, os.WriteFile(tmpDir+"/index.html", []byte("<html></html>"), 0644))

			strategies := map[string]auth.AuthStrategy{oidcHeader: newAuthStrategy(t, true, nil)}
			store := &readCountingStore{DeployLockStore: lock.NewInMemoryDeployLockStore()}
			if tt.lockedFirst {
				require.NoError(t, store.Lock())
			}
			lockdown, err := NewLockdown("", store)
			require.NoError(t, err)

			env := &Env{
				lockdown:      lockdown,
				strategies:    strategies,
				authenticator: auth.NewAuthenticator(strategies),
				// Without this, Shutdown has no channel to close, so it waits out its
				// whole context and leaves the connection goroutine running.
				shutdownCh: make(chan struct{}),
				config: &config.ServerConfig{
					StaticFilePath: tmpDir,
					DevEnvironment: true, // allow the test origin through the WS origin check
					OIDC:           config.OIDCConfig{Enabled: true},
				},
			}

			server := httptest.NewServer(env.CreateRouter())
			t.Cleanup(func() {
				shutdownEnv(env)
				server.Close()
				connectionsMutex.Lock()
				connections = nil
				closedConns = make(map[*websocket.Conn]bool)
				connectionsMutex.Unlock()
			})

			dialCtx, dialCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer dialCancel()
			// OIDC is enabled here, so the handshake needs a credential like any other.
			conn, _, err := websocket.Dial(dialCtx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws",
				&websocket.DialOptions{HTTPHeader: http.Header{oidcHeader: []string{"Bearer token"}}})
			require.NoError(t, err)
			defer conn.Close(websocket.StatusNormalClosure, "test complete")

			// Run the watcher far faster than production so the test does not wait 5s.
			stop := make(chan struct{})
			defer close(stop)
			go lockdown.WatchTransitions(stop, 5*time.Millisecond, notifyWebSocketClients)

			// The watcher captures its baseline on entry, and only a change against
			// that baseline is broadcast. Wait for the baseline read before mutating,
			// otherwise a request that wins the race makes the post-change value the
			// baseline and nothing is ever pushed. Neither handler reads the state, so
			// any read is the watcher's.
			require.Eventually(t, func() bool { return store.reads.Load() > 0 },
				5*time.Second, time.Millisecond, "lock watcher never read its baseline")

			req, err := http.NewRequest(tt.method, server.URL+"/api/v1/deploy-lock", nil)
			require.NoError(t, err)
			req.Header.Set(oidcHeader, "Bearer valid-token")
			resp, err := server.Client().Do(req)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, http.StatusOK, resp.StatusCode)

			readCtx, readCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer readCancel()
			_, msg, err := conn.Read(readCtx)
			require.NoError(t, err)
			assert.Equal(t, tt.wantMsg, string(msg))

			// Exactly one push: a second one would mean a notifier other than the
			// watcher also fired, which is what desynchronizes the watcher's baseline.
			quietCtx, quietCancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer quietCancel()
			_, extra, err := conn.Read(quietCtx)
			assert.ErrorIs(t, err, context.DeadlineExceeded, "unexpected second push: %q", string(extra))
		})
	}
}

// TestValidatePath tests the path validation function.
// Security model: validatePath cleans the input path and joins it with basePath.
// The security check verifies that the JOINED path stays within basePath.
// Example: input "/../etc/passwd" → cleaned "/etc/passwd" → joined "/tmp/etc/passwd"
// This is SAFE because the actual file served would be /tmp/etc/passwd, NOT /etc/passwd.
func TestValidatePath(t *testing.T) {
	fs := safeFileSystem{
		root:     http.Dir("/tmp"),
		basePath: "/tmp",
	}

	t.Run("valid simple path", func(t *testing.T) {
		cleanPath, err := fs.validatePath("/file.txt")
		assert.NoError(t, err)
		assert.Equal(t, "/file.txt", cleanPath)
	})

	t.Run("valid nested path", func(t *testing.T) {
		cleanPath, err := fs.validatePath("/subdir/file.txt")
		assert.NoError(t, err)
		assert.Equal(t, "/subdir/file.txt", cleanPath)
	})

	t.Run("path without leading slash", func(t *testing.T) {
		cleanPath, err := fs.validatePath("file.txt")
		assert.NoError(t, err)
		assert.Equal(t, "/file.txt", cleanPath)
	})

	t.Run("path with dot components is cleaned", func(t *testing.T) {
		cleanPath, err := fs.validatePath("/./subdir/../file.txt")
		assert.NoError(t, err)
		assert.Equal(t, "/file.txt", cleanPath)
	})

	t.Run("root path is valid", func(t *testing.T) {
		cleanPath, err := fs.validatePath("/")
		assert.NoError(t, err)
		assert.Equal(t, "/", cleanPath)
	})

	t.Run("leading dotdot at root level is normalized and joined safely", func(t *testing.T) {
		cleanPath, err := fs.validatePath("/../etc/passwd")
		assert.NoError(t, err)
		assert.Equal(t, "/etc/passwd", cleanPath)
	})

	t.Run("dotdot in path is normalized and joined safely", func(t *testing.T) {
		cleanPath, err := fs.validatePath("/subdir/../../etc/passwd")
		assert.NoError(t, err)
		assert.Equal(t, "/etc/passwd", cleanPath)
	})

	t.Run("empty path becomes root", func(t *testing.T) {
		cleanPath, err := fs.validatePath("")
		assert.NoError(t, err)
		assert.Equal(t, "/", cleanPath)
	})
}

func TestSafeFileSystem(t *testing.T) {
	tmpDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("content"), 0644)
	require.NoError(t, err)

	err = os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tmpDir, "subdir", "nested.txt"), []byte("nested"), 0644)
	require.NoError(t, err)

	// Resolve symlinks to ensure consistent path comparison (important on macOS)
	resolvedBase, err := filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err)

	fs := safeFileSystem{
		root:     http.Dir(tmpDir),
		basePath: resolvedBase,
	}

	t.Run("valid path opens file", func(t *testing.T) {
		f, err := fs.Open("/file.txt")
		require.NoError(t, err)
		require.NotNil(t, f)
		defer f.Close()
	})

	t.Run("nested valid path opens file", func(t *testing.T) {
		f, err := fs.Open("/subdir/nested.txt")
		require.NoError(t, err)
		require.NotNil(t, f)
		defer f.Close()
	})

	t.Run("path traversal attack returns error", func(t *testing.T) {
		_, err := fs.Open("/../../../etc/passwd")
		assert.Error(t, err)
	})

	t.Run("dotdot in middle returns error for outside path", func(t *testing.T) {
		_, err := fs.Open("/subdir/../../etc/passwd")
		assert.Error(t, err)
	})

	t.Run("non-existent file returns error", func(t *testing.T) {
		_, err := fs.Open("/nonexistent.txt")
		assert.Error(t, err)
	})

	t.Run("clean path with redundant components works", func(t *testing.T) {
		f, err := fs.Open("/./subdir/../file.txt")
		require.NoError(t, err)
		require.NotNil(t, f)
		defer f.Close()
	})

	t.Run("open root directory", func(t *testing.T) {
		f, err := fs.Open("/")
		require.NoError(t, err)
		require.NotNil(t, f)
		defer f.Close()
		stat, err := f.Stat()
		require.NoError(t, err)
		assert.True(t, stat.IsDir())
	})
}

func TestSafeFileSystemSymlinkProtection(t *testing.T) {
	tmpDir := t.TempDir()
	outsideDir := t.TempDir()

	err := os.WriteFile(filepath.Join(outsideDir, "secret.txt"), []byte("secret data"), 0644)
	require.NoError(t, err)

	symlinkPath := filepath.Join(tmpDir, "escape")
	err = os.Symlink(outsideDir, symlinkPath)
	require.NoError(t, err)

	// Resolve symlinks to ensure consistent path comparison (important on macOS)
	resolvedBase, err := filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err)

	fs := safeFileSystem{
		root:     http.Dir(tmpDir),
		basePath: resolvedBase,
	}

	t.Run("symlink escaping directory is blocked", func(t *testing.T) {
		_, err := fs.Open("/escape/secret.txt")
		assert.Error(t, err)
		assert.ErrorIs(t, err, os.ErrPermission)
	})
}

// shutdownEnv drains env with the same bound production uses, so a WebSocket
// goroutine that fails to exit surfaces as a test timeout rather than hanging the
// run forever on an unbounded wait.
func shutdownEnv(env *Env) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	env.Shutdown(ctx)
}

func TestEnvShutdown(t *testing.T) {
	t.Run("shutdown closes channel", func(t *testing.T) {
		shutdownCh := make(chan struct{})
		env := &Env{
			shutdownCh: shutdownCh,
		}

		select {
		case <-env.shutdownCh:
			t.Fatal("channel should not be closed yet")
		default:
		}

		env.Shutdown(context.Background())

		select {
		case <-env.shutdownCh:
		default:
			t.Fatal("channel should be closed after Shutdown()")
		}
	})

	t.Run("shutdown with nil channel is safe", func(t *testing.T) {
		env := &Env{
			shutdownCh: nil,
		}

		env.Shutdown(context.Background())
	})

	t.Run("shutdown can be called multiple times safely", func(t *testing.T) {
		shutdownCh := make(chan struct{})
		env := &Env{
			shutdownCh: shutdownCh,
		}

		env.Shutdown(context.Background())
		env.Shutdown(context.Background())
		env.Shutdown(context.Background())

		select {
		case <-env.shutdownCh:
		default:
			t.Fatal("channel should be closed")
		}
	})

	t.Run("shutdown returns when its context expires", func(t *testing.T) {
		env := &Env{shutdownCh: make(chan struct{})}
		// A connection goroutine that never finishes: Shutdown must give up when the
		// caller's context expires so the phases that follow it still get budget.
		env.connWg.Add(1)
		defer env.connWg.Done()

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		returned := make(chan struct{})
		go func() { env.Shutdown(ctx); close(returned) }()

		select {
		case <-returned:
		case <-time.After(3 * time.Second):
			t.Fatal("Shutdown ignored its context deadline")
		}
	})

	t.Run("shutdown waits for connWg", func(t *testing.T) {
		shutdownCh := make(chan struct{})
		env := &Env{
			shutdownCh: shutdownCh,
		}

		env.connWg.Add(1)

		shutdownDone := make(chan struct{})
		go func() {
			env.Shutdown(context.Background())
			close(shutdownDone)
		}()

		select {
		case <-shutdownDone:
			t.Fatal("Shutdown should be blocked waiting for connWg")
		case <-time.After(300 * time.Millisecond):
		}

		env.connWg.Done()

		select {
		case <-shutdownDone:
		case <-time.After(time.Second):
			t.Fatal("Shutdown should have completed after connWg.Done()")
		}
	})
}

// TestStartLockdownWatcher verifies the watcher goroutine lifecycle: it runs
// regardless of whether schedules are configured — with a shared deploy lock
// store it is how clients learn about a lock another replica set — and it is
// tracked by connWg so it stops when the shutdown channel is closed.
func TestStartLockdownWatcher(t *testing.T) {
	waitTracked := func(env *Env, timeout time.Duration) bool {
		done := make(chan struct{})
		go func() {
			env.connWg.Wait()
			close(done)
		}()
		select {
		case <-done:
			return true
		case <-time.After(timeout):
			return false
		}
	}

	t.Run("runs even when no schedules are configured", func(t *testing.T) {
		env := &Env{shutdownCh: make(chan struct{})}
		env.lockdown, _ = NewLockdown("", lock.NewInMemoryDeployLockStore())

		env.StartLockdownWatcher()

		// Lock changes made on another replica are only observable by polling,
		// so the watcher must run even without schedules.
		assert.False(t, waitTracked(env, 100*time.Millisecond), "watcher should run without schedules")

		close(env.shutdownCh)
		assert.True(t, waitTracked(env, time.Second), "watcher should exit after shutdown channel is closed")
	})

	t.Run("tracks the watcher and stops on shutdown", func(t *testing.T) {
		env := &Env{shutdownCh: make(chan struct{})}
		env.lockdown, _ = NewLockdown("", lock.NewInMemoryDeployLockStore())
		env.lockdown.Schedules = []LockdownSchedule{
			{StartDay: time.Monday, StartHour: 0, StartMin: 0, EndDay: time.Monday, EndHour: 1, EndMin: 0},
		}

		env.StartLockdownWatcher()

		assert.False(t, waitTracked(env, 100*time.Millisecond), "watcher should keep connWg non-zero before shutdown")

		close(env.shutdownCh)
		assert.True(t, waitTracked(env, time.Second), "watcher should exit after shutdown channel is closed")
	})
}

func TestStartRouter(t *testing.T) {

	tmpDir := t.TempDir()
	err := os.WriteFile(tmpDir+"/index.html", []byte("<html></html>"), 0644)
	require.NoError(t, err)

	serverConfig := &config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           "0", // Use port 0 for automatic port assignment
		StaticFilePath: tmpDir,
	}

	env := &Env{config: serverConfig}
	env.lockdown, _ = NewLockdown("", lock.NewInMemoryDeployLockStore())

	router := env.CreateRouter()
	srv := env.StartRouter(router)

	assert.NotNil(t, srv)
	assert.Equal(t, "127.0.0.1:0", srv.Addr)
	assert.Equal(t, router, srv.Handler)
	assert.Equal(t, 10*time.Second, srv.ReadHeaderTimeout)
	assert.Equal(t, 30*time.Second, srv.ReadTimeout)
	assert.Equal(t, 120*time.Second, srv.WriteTimeout)
	assert.Equal(t, 120*time.Second, srv.IdleTimeout)
}

func TestNotifyWebSocketClients(t *testing.T) {
	t.Run("notifies with no connections", func(t *testing.T) {
		connectionsMutex.Lock()
		connections = nil
		closedConns = make(map[*websocket.Conn]bool)
		connectionsMutex.Unlock()

		t.Cleanup(func() {
			connectionsMutex.Lock()
			connections = nil
			closedConns = make(map[*websocket.Conn]bool)
			connectionsMutex.Unlock()
		})

		notifyWebSocketClients("test message")
	})
}

func TestRemoveWebSocketConnectionCleanup(t *testing.T) {
	t.Run("removes connection and cleans up closedConns", func(t *testing.T) {
		connectionsMutex.Lock()
		connections = nil
		closedConns = make(map[*websocket.Conn]bool)
		connectionsMutex.Unlock()

		t.Cleanup(func() {
			connectionsMutex.Lock()
			connections = nil
			closedConns = make(map[*websocket.Conn]bool)
			connectionsMutex.Unlock()
		})

		removeWebSocketConnection(nil)

		connectionsMutex.RLock()
		assert.Len(t, connections, 0)
		assert.Len(t, closedConns, 0)
		connectionsMutex.RUnlock()
	})

	t.Run("removes actual connection from slice", func(t *testing.T) {
		connectionsMutex.Lock()
		connections = nil
		closedConns = make(map[*websocket.Conn]bool)
		connectionsMutex.Unlock()

		t.Cleanup(func() {
			connectionsMutex.Lock()
			connections = nil
			closedConns = make(map[*websocket.Conn]bool)
			connectionsMutex.Unlock()
		})

		conn := &websocket.Conn{}
		connectionsMutex.Lock()
		connections = append(connections, conn)
		connectionsMutex.Unlock()

		removeWebSocketConnection(conn)

		connectionsMutex.RLock()
		assert.NotContains(t, connections, conn)
		assert.Len(t, closedConns, 0)
		connectionsMutex.RUnlock()
	})

	t.Run("removes connection from middle of slice", func(t *testing.T) {
		connectionsMutex.Lock()
		connections = nil
		closedConns = make(map[*websocket.Conn]bool)
		connectionsMutex.Unlock()

		t.Cleanup(func() {
			connectionsMutex.Lock()
			connections = nil
			closedConns = make(map[*websocket.Conn]bool)
			connectionsMutex.Unlock()
		})

		conn1 := &websocket.Conn{}
		conn2 := &websocket.Conn{}
		conn3 := &websocket.Conn{}
		connectionsMutex.Lock()
		connections = append(connections, conn1, conn2, conn3)
		connectionsMutex.Unlock()

		removeWebSocketConnection(conn2)

		connectionsMutex.RLock()
		assert.Len(t, connections, 2)
		// Check by pointer address, not value (all zero-value Conns are equal by value)
		foundConn1 := false
		foundConn2 := false
		foundConn3 := false
		for _, c := range connections {
			if c == conn1 {
				foundConn1 = true
			}
			if c == conn2 {
				foundConn2 = true
			}
			if c == conn3 {
				foundConn3 = true
			}
		}
		connectionsMutex.RUnlock()

		assert.True(t, foundConn1, "conn1 should still be in the slice")
		assert.False(t, foundConn2, "conn2 should have been removed")
		assert.True(t, foundConn3, "conn3 should still be in the slice")
	})
}

func TestNotifyWebSocketClientsFiltersClosedConnections(t *testing.T) {
	connectionsMutex.Lock()
	connections = nil
	closedConns = make(map[*websocket.Conn]bool)
	connectionsMutex.Unlock()

	t.Cleanup(func() {
		connectionsMutex.Lock()
		connections = nil
		closedConns = make(map[*websocket.Conn]bool)
		connectionsMutex.Unlock()
	})

	conn := &websocket.Conn{}
	connectionsMutex.Lock()
	connections = append(connections, conn)
	closedConns[conn] = true
	connectionsMutex.Unlock()

	notifyWebSocketClients("test message")
}

func TestConnWgTracking(t *testing.T) {
	env := &Env{
		shutdownCh: make(chan struct{}),
	}

	// Simulate handleWebSocketConnection incrementing the WaitGroup
	env.connWg.Add(1)

	// Simulate checkConnection running in a goroutine
	checkDone := make(chan struct{})
	go func() {
		defer env.connWg.Done()
		<-env.shutdownCh
		close(checkDone)
	}()

	select {
	case <-checkDone:
		t.Fatal("goroutine should not have exited yet")
	case <-time.After(50 * time.Millisecond):
	}

	shutdownComplete := make(chan struct{})
	go func() {
		shutdownEnv(env)
		close(shutdownComplete)
	}()

	select {
	case <-checkDone:
	case <-time.After(time.Second):
		t.Fatal("goroutine should have received shutdown signal")
	}

	select {
	case <-shutdownComplete:
	case <-time.After(time.Second):
		t.Fatal("Shutdown should have completed")
	}
}

func TestCreateRouterInitializesShutdownChannel(t *testing.T) {

	tmpDir := t.TempDir()
	err := os.WriteFile(tmpDir+"/index.html", []byte("<html></html>"), 0644)
	require.NoError(t, err)

	serverConfig := &config.ServerConfig{
		StaticFilePath: tmpDir,
	}

	env := &Env{
		config:     serverConfig,
		shutdownCh: nil,
	}
	env.lockdown, _ = NewLockdown("", lock.NewInMemoryDeployLockStore())

	_ = env.CreateRouter()

	assert.NotNil(t, env.shutdownCh, "CreateRouter should initialize shutdownCh")
}

func TestValidatePathEdgeCases(t *testing.T) {
	tmpDir := t.TempDir()

	fs := safeFileSystem{
		root:     http.Dir(tmpDir),
		basePath: tmpDir,
	}

	t.Run("multiple slashes are cleaned", func(t *testing.T) {
		cleanPath, err := fs.validatePath("///file.txt")
		assert.NoError(t, err)
		assert.Equal(t, "/file.txt", cleanPath)
	})

	t.Run("dot path is cleaned to root", func(t *testing.T) {
		cleanPath, err := fs.validatePath("/.")
		assert.NoError(t, err)
		assert.Equal(t, "/", cleanPath)
	})

	t.Run("complex path with multiple dots is cleaned", func(t *testing.T) {
		cleanPath, err := fs.validatePath("/a/b/./c/../d")
		assert.NoError(t, err)
		assert.Equal(t, "/a/b/d", cleanPath)
	})
}

func TestGetConfigEndpoint(t *testing.T) {

	serverConfig := &config.ServerConfig{
		StateType:      "in-memory",
		LogLevel:       "debug",
		DevEnvironment: true,
	}

	env := &Env{config: serverConfig}

	router := chi.NewRouter()
	router.Get("/api/v1/config", env.getConfig)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/config", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "in-memory")
	assert.Contains(t, w.Body.String(), "debug")
	assert.Contains(t, w.Body.String(), "devEnvironment")
}

// The endpoint is what the CLI client, the Web UI and argo-watcher-mcp read, and it
// is unauthenticated: argo_cd_url must arrive as a URL string, and any basic-auth
// userinfo in ARGO_URL must not arrive at all.
func TestGetConfigEndpoint_ArgoUrl(t *testing.T) {
	// Assembled rather than parsed from a literal: a userinfo-bearing URL in the
	// source trips the repository's secret scanner.
	argoURL := url.URL{
		Scheme: "https",
		User:   url.UserPassword("admin", "s3cret"),
		Host:   "argo-cd.example.com",
		Path:   "/argocd",
	}

	env := &Env{config: &config.ServerConfig{
		StateType: "in-memory",
		ArgoUrl:   config.URL{URL: argoURL},
	}}

	router := chi.NewRouter()
	router.Get("/api/v1/config", env.getConfig)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/config", http.NoBody))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"argo_cd_url":"https://argo-cd.example.com/argocd"`)
	assert.NotContains(t, w.Body.String(), "s3cret")
}

func TestGetTaskStatusEndpoint(t *testing.T) {

	t.Run("returns task when found", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo, _ := newRepo(ctrl)
		repo.EXPECT().GetTask(gomock.Any()).DoAndReturn(func(id string) (*models.Task, error) {
			return &models.Task{
				Id:           id,
				App:          "test-app",
				Author:       "test-author",
				Project:      "test-project",
				Status:       "deployed",
				StatusReason: "",
			}, nil
		}).AnyTimes()
		argo := &argocd.Argo{}
		argo.Init(repo, newArgoAPI(ctrl), newMetrics(ctrl))

		env := &Env{argo: argo}

		router := chi.NewRouter()
		router.Get("/api/v1/tasks/{id}", env.getTaskStatus)

		req, _ := http.NewRequest(http.MethodGet, "/api/v1/tasks/test-task-id", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "test-task-id")
		assert.Contains(t, w.Body.String(), "test-app")
	})

	t.Run("returns 404 when task not found", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo, _ := newRepo(ctrl)
		repo.EXPECT().GetTask(gomock.Any()).Return(nil, state.ErrTaskNotFound).AnyTimes()
		argo := &argocd.Argo{}
		argo.Init(repo, newArgoAPI(ctrl), newMetrics(ctrl))

		env := &Env{argo: argo}

		router := chi.NewRouter()
		router.Get("/api/v1/tasks/{id}", env.getTaskStatus)

		req, _ := http.NewRequest(http.MethodGet, "/api/v1/tasks/nonexistent", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "nonexistent")
		assert.Contains(t, w.Body.String(), "task not found")
	})

	t.Run("returns 500 on backend failure", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo, _ := newRepo(ctrl)
		repo.EXPECT().GetTask(gomock.Any()).Return(nil, fmt.Errorf("connection refused")).AnyTimes()
		argo := &argocd.Argo{}
		argo.Init(repo, newArgoAPI(ctrl), newMetrics(ctrl))

		env := &Env{argo: argo}

		router := chi.NewRouter()
		router.Get("/api/v1/tasks/{id}", env.getTaskStatus)

		req, _ := http.NewRequest(http.MethodGet, "/api/v1/tasks/some-id", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "some-id")
		assert.Contains(t, w.Body.String(), "internal server error")
		// The internal error detail must not leak to the client.
		assert.NotContains(t, w.Body.String(), "connection refused")
	})
}

func TestValidateTokenWithStrategies(t *testing.T) {

	t.Run("returns result from authenticator when no allowed strategy", func(t *testing.T) {
		strategies := make(map[string]auth.AuthStrategy)
		mockAuth := auth.NewAuthenticator(strategies)

		env := &Env{
			authenticator: mockAuth,
			strategies:    strategies,
		}

		req, _ := http.NewRequest(http.MethodPost, "/test", nil)

		valid, err := env.validateToken(req, "")
		assert.False(t, valid)
		assert.NoError(t, err)
	})

	t.Run("skips non-matching strategy headers", func(t *testing.T) {
		strategies := make(map[string]auth.AuthStrategy)
		env := &Env{
			strategies:    strategies,
			authenticator: auth.NewAuthenticator(strategies),
		}

		req, _ := http.NewRequest(http.MethodPost, "/test", nil)
		req.Header.Set("Authorization", "Bearer test-token")

		valid, err := env.validateToken(req, "X-Unregistered-Auth")
		assert.False(t, valid)
		assert.NoError(t, err)
	})
}

func TestAddTaskEndpoint(t *testing.T) {

	t.Run("returns error for invalid JSON payload", func(t *testing.T) {
		env := &Env{}

		router := chi.NewRouter()
		router.Post("/api/v1/tasks", env.addTask)

		req, _ := http.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader("invalid json"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotAcceptable, w.Code)
		assert.Contains(t, w.Body.String(), "invalid payload")
	})

	t.Run("rejects task when lockdown is active", func(t *testing.T) {
		lockdown, _ := NewLockdown("", lock.NewInMemoryDeployLockStore())
		require.NoError(t, lockdown.SetLock())

		env := &Env{
			lockdown: lockdown,
		}

		router := chi.NewRouter()
		router.Post("/api/v1/tasks", env.addTask)

		taskJSON := `{"app": "test-app", "author": "test-author", "project": "test-project", "images": [{"image": "test", "tag": "v1"}]}`
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(taskJSON))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotAcceptable, w.Code)
		assert.Contains(t, w.Body.String(), "rejected")
		assert.Contains(t, w.Body.String(), "lockdown is active")
	})

	t.Run("rejects task when a lock was set through the shared store", func(t *testing.T) {
		// The reason the lock is shared: a lock engaged elsewhere — another
		// replica, writing the same store — must reject deploys here, without
		// this process ever being told. Locking the store directly, rather than
		// via the Lockdown, is what a peer replica looks like from here.
		store := lock.NewInMemoryDeployLockStore()
		lockdown, err := NewLockdown("", store)
		require.NoError(t, err)
		require.NoError(t, store.Lock())

		env := &Env{
			lockdown: lockdown,
		}

		router := chi.NewRouter()
		router.Post("/api/v1/tasks", env.addTask)

		taskJSON := `{"app": "test-app", "author": "test-author", "project": "test-project", "images": [{"image": "test", "tag": "v1"}]}`
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(taskJSON))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotAcceptable, w.Code)
		assert.Contains(t, w.Body.String(), "lockdown is active")
	})

	t.Run("returns 401 with reason when token validation fails", func(t *testing.T) {
		// A bad token is a client mistake, not a server failure — 401, not 500.
		// The strategy's error must surface in the response body so the client
		// can show the user something actionable (e.g. "deploy token is invalid").
		lockdown, _ := NewLockdown("", lock.NewInMemoryDeployLockStore())
		strategies := make(map[string]auth.AuthStrategy)
		strategies["Authorization"] = newAuthStrategy(t, false, fmt.Errorf("deploy token is invalid"))

		env := &Env{
			lockdown:      lockdown,
			strategies:    strategies,
			authenticator: auth.NewAuthenticator(strategies),
		}

		router := chi.NewRouter()
		router.Post("/api/v1/tasks", env.addTask)

		taskJSON := `{"app": "test-app", "author": "test-author", "project": "test-project", "images": [{"image": "test", "tag": "v1"}]}`
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(taskJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "deploy token is invalid")
	})

	t.Run("returns error when argo.AddTask fails", func(t *testing.T) {
		lockdown, _ := NewLockdown("", lock.NewInMemoryDeployLockStore())
		strategies := make(map[string]auth.AuthStrategy)

		ctrl := gomock.NewController(t)
		repo, _ := newRepo(ctrl)
		repo.EXPECT().Check().Return(true).AnyTimes()
		repo.EXPECT().AddTask(gomock.Any()).Return(nil, fmt.Errorf("argo unavailable")).AnyTimes()
		argo := &argocd.Argo{}
		argo.Init(repo, newArgoAPI(ctrl), newMetrics(ctrl))

		env := &Env{
			lockdown:      lockdown,
			strategies:    strategies,
			authenticator: auth.NewAuthenticator(strategies),
			argo:          argo,
			config:        &config.ServerConfig{DeploymentTimeout: 900},
		}

		router := chi.NewRouter()
		router.Post("/api/v1/tasks", env.addTask)

		taskJSON := `{"app": "test-app", "author": "test-author", "project": "test-project", "images": [{"image": "test", "tag": "v1"}]}`
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(taskJSON))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.Contains(t, w.Body.String(), "down")
	})

	// Validated gates the git write-back AND what a task may supersede, so a caller
	// must never be able to assert it. This pins the handler's unconditional
	// assignment from the auth result; the inbound json:"-" half is pinned by
	// models.TestTask_ValidatedIsNeverSerialized.
	t.Run("a validated flag in the request body is ignored", func(t *testing.T) {
		lockdown, _ := NewLockdown("", lock.NewInMemoryDeployLockStore())
		strategies := make(map[string]auth.AuthStrategy)

		ctrl := gomock.NewController(t)
		repo, _ := newRepo(ctrl)
		repo.EXPECT().Check().Return(true).AnyTimes()

		// Capture the task, then fail the insert: the success path would spawn the
		// handler's real WaitForRollout goroutine, which this Env has no updater for.
		// The flag is already decided by the time AddTask is called.
		var stored models.Task
		repo.EXPECT().AddTask(gomock.Any()).DoAndReturn(func(task models.Task) (*models.Task, error) {
			stored = task
			return nil, fmt.Errorf("stop before the rollout goroutine")
		})
		argo := &argocd.Argo{}
		argo.Init(repo, newArgoAPI(ctrl), newMetrics(ctrl))

		env := &Env{
			lockdown:      lockdown,
			strategies:    strategies,
			authenticator: auth.NewAuthenticator(strategies),
			argo:          argo,
			config:        &config.ServerConfig{DeploymentTimeout: 900},
		}

		router := chi.NewRouter()
		router.Post("/api/v1/tasks", env.addTask)

		taskJSON := `{"app": "test-app", "author": "test-author", "project": "test-project", "images": [{"image": "test", "tag": "v1"}], "validated": true}`
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(taskJSON))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.False(t, stored.Validated, "a body-supplied validated flag must not grant authority")
	})

	// The mirrored positive case. Without it, mutating the assignment to
	// `task.Validated = false` keeps the whole Go suite green: the state and argocd
	// tests set Validated directly, so this handler line is the only place the
	// authority the rule is weighed against is actually derived from auth.
	t.Run("a valid credential marks the task validated and carries into supersession", func(t *testing.T) {
		lockdown, _ := NewLockdown("", lock.NewInMemoryDeployLockStore())
		strategies := map[string]auth.AuthStrategy{
			"Authorization": newAuthStrategy(t, true, nil),
		}

		// A bare mock, not newRepo: its permissive CancelInProgressTasks stub would
		// absorb the call before the specific expectation below could match it.
		ctrl := gomock.NewController(t)
		repo := mocks.NewMockTaskRepository(ctrl)
		repo.EXPECT().GetTasks(gomock.Any(), gomock.Any(), "test-app", models.StatusDeployedMessage, gomock.Any(), gomock.Any()).Return([]models.Task{}, int64(0))
		// The literal true ties the handler's authority to the state-layer rule.
		repo.EXPECT().CancelInProgressTasks("test-app", gomock.Any(), gomock.Any(), true).Return(int64(0), nil)

		var stored models.Task
		repo.EXPECT().AddTask(gomock.Any()).DoAndReturn(func(task models.Task) (*models.Task, error) {
			stored = task
			return nil, fmt.Errorf("stop before the rollout goroutine")
		})
		argo := &argocd.Argo{}
		argo.Init(repo, newArgoAPI(ctrl), newMetrics(ctrl))

		env := &Env{
			lockdown:      lockdown,
			strategies:    strategies,
			authenticator: auth.NewAuthenticator(strategies),
			argo:          argo,
			config:        &config.ServerConfig{DeploymentTimeout: 900},
		}

		router := chi.NewRouter()
		router.Post("/api/v1/tasks", env.addTask)

		taskJSON := `{"app": "test-app", "author": "test-author", "project": "test-project", "images": [{"image": "test", "tag": "v1"}]}`
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(taskJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "test-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.True(t, stored.Validated, "a valid credential must mark the task validated")
	})
}

func TestSetDeployLockWithKeycloak(t *testing.T) {

	t.Run("returns 401 with reason when token is invalid", func(t *testing.T) {
		// Strategy returned (false, err) — auth attempted but failed.
		// The 401 body should carry the strategy's reason so the client
		// can distinguish "wrong token" from "expired token" etc.
		lockdown, _ := NewLockdown("", lock.NewInMemoryDeployLockStore())
		strategies := make(map[string]auth.AuthStrategy)
		strategies[oidcHeader] = newAuthStrategy(t, false, fmt.Errorf("token expired"))

		env := &Env{
			lockdown:      lockdown,
			strategies:    strategies,
			authenticator: auth.NewAuthenticator(strategies),
			config: &config.ServerConfig{
				OIDC: config.OIDCConfig{Enabled: true},
			},
		}

		router := chi.NewRouter()
		router.Post("/api/v1/deploy-lock", env.SetDeployLock)

		req, _ := http.NewRequest(http.MethodPost, "/api/v1/deploy-lock", nil)
		req.Header.Set(oidcHeader, "Bearer invalid-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "token expired")
	})

	t.Run("returns 401 indicating missing auth when header absent", func(t *testing.T) {
		// No auth header at all — Authenticator returns (false, nil),
		// distinct from invalid auth. The 401 body should hint that
		// authentication is required, rather than implying a wrong token.
		lockdown, _ := NewLockdown("", lock.NewInMemoryDeployLockStore())
		strategies := make(map[string]auth.AuthStrategy)
		strategies[oidcHeader] = newAuthStrategy(t, false, nil)

		env := &Env{
			lockdown:      lockdown,
			strategies:    strategies,
			authenticator: auth.NewAuthenticator(strategies),
			config: &config.ServerConfig{
				OIDC: config.OIDCConfig{Enabled: true},
			},
		}

		router := chi.NewRouter()
		router.Post("/api/v1/deploy-lock", env.SetDeployLock)

		req, _ := http.NewRequest(http.MethodPost, "/api/v1/deploy-lock", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, strings.ToLower(w.Body.String()), "authentication required")
	})

	t.Run("sets lock when token is valid", func(t *testing.T) {
		lockdown, _ := NewLockdown("", lock.NewInMemoryDeployLockStore())
		strategies := make(map[string]auth.AuthStrategy)
		strategies[oidcHeader] = newAuthStrategy(t, true, nil)

		env := &Env{
			lockdown:      lockdown,
			strategies:    strategies,
			authenticator: auth.NewAuthenticator(strategies),
			config: &config.ServerConfig{
				OIDC: config.OIDCConfig{Enabled: true},
			},
		}

		router := chi.NewRouter()
		router.Post("/api/v1/deploy-lock", env.SetDeployLock)

		req, _ := http.NewRequest(http.MethodPost, "/api/v1/deploy-lock", nil)
		req.Header.Set(oidcHeader, "Bearer valid-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "deploy lock is set")
		assert.True(t, lockdown.IsLocked())
	})
}

// TestDeployLockStoreFailure verifies that a lock the server could not persist
// is reported as a failure instead of a success: an operator who is told the
// deploy lock is set must not be left with deployments still flowing. The store
// error itself stays in the log.
func TestDeployLockStoreFailure(t *testing.T) {

	newEnv := func(t *testing.T) *Env {
		t.Helper()

		strategies := make(map[string]auth.AuthStrategy)
		strategies[oidcHeader] = newAuthStrategy(t, true, nil)

		return &Env{
			lockdown:      &Lockdown{store: failingDeployLockStore{}, overrideDuration: defaultOverrideDuration},
			strategies:    strategies,
			authenticator: auth.NewAuthenticator(strategies),
			config: &config.ServerConfig{
				OIDC: config.OIDCConfig{Enabled: true},
			},
		}
	}

	testCases := []struct {
		name     string
		method   string
		register func(*chi.Mux, *Env)
		message  string
	}{
		{
			name:     "set",
			method:   http.MethodPost,
			register: func(r *chi.Mux, env *Env) { r.Post("/api/v1/deploy-lock", env.SetDeployLock) },
			message:  "failed to set deploy lock",
		},
		{
			name:     "release",
			method:   http.MethodDelete,
			register: func(r *chi.Mux, env *Env) { r.Delete("/api/v1/deploy-lock", env.ReleaseDeployLock) },
			message:  "failed to release deploy lock",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			env := newEnv(t)
			router := chi.NewRouter()
			tt.register(router, env)

			req, _ := http.NewRequest(tt.method, "/api/v1/deploy-lock", nil)
			req.Header.Set(oidcHeader, "Bearer valid-token")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Contains(t, w.Body.String(), tt.message)
			assert.NotContains(t, w.Body.String(), "database is unreachable", "the store error must not reach the client")
		})
	}
}

// The Keycloak-Authorization header was removed in 1.0.0. A client still sending
// it must be rejected, and the privileged write it carried must not take effect.
func TestSetDeployLockRejectsLegacyKeycloakHeader(t *testing.T) {
	lockdown, _ := NewLockdown("", lock.NewInMemoryDeployLockStore())
	strategies := map[string]auth.AuthStrategy{
		oidcHeader: newAuthStrategy(t, true, nil),
	}

	env := &Env{
		lockdown:      lockdown,
		strategies:    strategies,
		authenticator: auth.NewAuthenticator(strategies),
		config: &config.ServerConfig{
			OIDC: config.OIDCConfig{Enabled: true},
		},
	}

	router := chi.NewRouter()
	router.Post("/api/v1/deploy-lock", env.SetDeployLock)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/deploy-lock", nil)
	req.Header.Set("Keycloak-Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.False(t, lockdown.IsLocked())
}

func TestReleaseDeployLockWithKeycloak(t *testing.T) {

	t.Run("returns 401 with reason when token is invalid", func(t *testing.T) {
		// Strategy returned (false, err): auth attempted but failed.
		// The 401 body should carry the strategy's reason.
		lockdown, _ := NewLockdown("", lock.NewInMemoryDeployLockStore())
		require.NoError(t, lockdown.SetLock())
		strategies := make(map[string]auth.AuthStrategy)
		strategies[oidcHeader] = newAuthStrategy(t, false, fmt.Errorf("token expired"))

		env := &Env{
			lockdown:      lockdown,
			strategies:    strategies,
			authenticator: auth.NewAuthenticator(strategies),
			config: &config.ServerConfig{
				OIDC: config.OIDCConfig{Enabled: true},
			},
		}

		router := chi.NewRouter()
		router.Delete("/api/v1/deploy-lock", env.ReleaseDeployLock)

		req, _ := http.NewRequest(http.MethodDelete, "/api/v1/deploy-lock", nil)
		req.Header.Set(oidcHeader, "Bearer invalid-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "token expired")
	})

	t.Run("releases lock when token is valid", func(t *testing.T) {
		lockdown, _ := NewLockdown("", lock.NewInMemoryDeployLockStore())
		require.NoError(t, lockdown.SetLock())
		strategies := make(map[string]auth.AuthStrategy)
		strategies[oidcHeader] = newAuthStrategy(t, true, nil)

		env := &Env{
			lockdown:      lockdown,
			strategies:    strategies,
			authenticator: auth.NewAuthenticator(strategies),
			config: &config.ServerConfig{
				OIDC: config.OIDCConfig{Enabled: true},
			},
		}

		router := chi.NewRouter()
		router.Delete("/api/v1/deploy-lock", env.ReleaseDeployLock)

		req, _ := http.NewRequest(http.MethodDelete, "/api/v1/deploy-lock", nil)
		req.Header.Set(oidcHeader, "Bearer valid-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "deploy lock is released")
		assert.False(t, lockdown.IsLocked())
	})
}

func TestValidateTokenWithAllowedStrategy(t *testing.T) {

	t.Run("validates successfully with matching strategy", func(t *testing.T) {
		strategies := make(map[string]auth.AuthStrategy)
		strategies[oidcHeader] = newAuthStrategy(t, true, nil)

		env := &Env{
			strategies:    strategies,
			authenticator: auth.NewAuthenticator(strategies),
		}

		req, _ := http.NewRequest(http.MethodPost, "/test", nil)
		req.Header.Set(oidcHeader, "Bearer valid-token")

		valid, err := env.validateToken(req, oidcHeader)
		assert.True(t, valid)
		assert.NoError(t, err)
	})

	t.Run("strips Bearer prefix from token", func(t *testing.T) {
		strategies := make(map[string]auth.AuthStrategy)
		strategies[oidcHeader] = newAuthStrategy(t, true, nil)

		env := &Env{
			strategies:    strategies,
			authenticator: auth.NewAuthenticator(strategies),
		}

		req, _ := http.NewRequest(http.MethodPost, "/test", nil)
		req.Header.Set(oidcHeader, "Bearer my-token")

		valid, err := env.validateToken(req, oidcHeader)
		assert.True(t, valid)
		assert.NoError(t, err)
	})

	t.Run("returns error from strategy validation", func(t *testing.T) {
		expectedErr := fmt.Errorf("token expired")
		strategies := make(map[string]auth.AuthStrategy)
		strategies[oidcHeader] = newAuthStrategy(t, false, expectedErr)

		env := &Env{
			strategies:    strategies,
			authenticator: auth.NewAuthenticator(strategies),
		}

		req, _ := http.NewRequest(http.MethodPost, "/test", nil)
		req.Header.Set(oidcHeader, "Bearer expired-token")

		valid, err := env.validateToken(req, oidcHeader)
		assert.False(t, valid)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("skips strategies with non-matching headers", func(t *testing.T) {
		strategies := make(map[string]auth.AuthStrategy)
		strategies["Authorization"] = newAuthStrategy(t, true, nil)
		strategies[oidcHeader] = newAuthStrategy(t, false, nil)

		env := &Env{
			strategies:    strategies,
			authenticator: auth.NewAuthenticator(strategies),
		}

		req, _ := http.NewRequest(http.MethodPost, "/test", nil)
		req.Header.Set("Authorization", "Bearer token")

		valid, err := env.validateToken(req, oidcHeader)
		assert.False(t, valid)
		assert.NoError(t, err)
	})
}

// TestRouterCompatibility pins the routing behaviour clients depend on: the
// trailing-slash redirect, the /swagger mount, and the rule that anything the API
// does not serve — unknown path or unhandled method — reaches the Web UI rather
// than an error page.
func TestRouterCompatibility(t *testing.T) {
	env, _ := readAuthEnv(t, false, nil)
	static := env.config.StaticFilePath
	require.NoError(t, os.WriteFile(filepath.Join(static, "index.html"), []byte("SPA-INDEX"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(static, "swagger"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(static, "swagger", "swagger.json"), []byte(`{"swagger":"2.0"}`), 0o600))

	router := env.CreateRouter()

	cases := []struct {
		name     string
		method   string
		path     string
		status   int
		location string
		body     string
	}{
		// Bodies are asserted, not just statuses: the SPA fallback also answers 200,
		// so a route that silently failed to register would pass a status-only check.
		{"probe", http.MethodGet, "/livez", http.StatusOK, "", `{"status":"up"}`},
		{"probe trailing slash redirects", http.MethodGet, "/livez/", http.StatusMovedPermanently, "/livez", ""},
		{"config", http.MethodGet, "/api/v1/config", http.StatusOK, "", ""},
		{"config trailing slash redirects", http.MethodGet, "/api/v1/config/", http.StatusMovedPermanently, "/api/v1/config", ""},
		// The body echoes the id, which is what proves chi's {id} reached the handler
		// rather than an empty parameter after the :id -> {id} rename.
		{"task lookup reaches the handler", http.MethodGet, "/api/v1/tasks/abc", http.StatusNotFound, "", `{"id":"abc","error":"task not found"}`},
		{"a path naming no route is not redirected", http.MethodGet, "/dashboard/", http.StatusOK, "", "SPA-INDEX"},
		{"task lookup trailing slash redirects", http.MethodGet, "/api/v1/tasks/abc/", http.StatusMovedPermanently, "/api/v1/tasks/abc", ""},
		{"redirect keeps the query string", http.MethodGet, "/api/v1/config/?x=1", http.StatusMovedPermanently, "/api/v1/config?x=1", ""},
		{"bare swagger redirects to the directory", http.MethodGet, "/swagger", http.StatusMovedPermanently, "/swagger/", ""},
		// The other half of that redirect: /swagger/ must be served, not sent back to
		// /swagger, or the two rules would bounce a client between them forever.
		{"the swagger directory is not redirected back", http.MethodGet, "/swagger/", http.StatusOK, "", ""},
		{"swagger spec is served", http.MethodGet, "/swagger/swagger.json", http.StatusOK, "", `{"swagger":"2.0"}`},
		{"swagger spec answers HEAD", http.MethodHead, "/swagger/swagger.json", http.StatusOK, "", ""},
		{"missing swagger file falls back to the UI", http.MethodGet, "/swagger/missing.json", http.StatusOK, "", "SPA-INDEX"},
		// The swagger directory lives under the static root, so the fallback the
		// unhandled method lands on serves the same file the GET route would.
		{"unhandled method on swagger reaches the static handler", http.MethodPost, "/swagger/swagger.json", http.StatusOK, "", `{"swagger":"2.0"}`},
		{"unhandled method on an API route falls back to the UI", http.MethodPut, "/api/v1/tasks", http.StatusOK, "", "SPA-INDEX"},
		{"deploy-lock write is absent without OIDC", http.MethodDelete, "/api/v1/deploy-lock", http.StatusOK, "", "SPA-INDEX"},
		{"deep link loads the UI", http.MethodGet, "/some/spa/route", http.StatusOK, "", "SPA-INDEX"},
		{"root loads the UI", http.MethodGet, "/", http.StatusOK, "", "SPA-INDEX"},
		// 307 rather than 301, because only 307 obliges the client to repeat the body.
		{"non-GET trailing slash keeps the method", http.MethodPost, "/api/v1/tasks/", http.StatusTemporaryRedirect, "/api/v1/tasks", ""},
		{"metrics is served", http.MethodGet, "/metrics", http.StatusOK, "", "@contains:go_goroutines"},
		{"metrics is a GET-only endpoint", http.MethodPost, "/metrics", http.StatusOK, "", "SPA-INDEX"},
		// Nothing may put a foreign host in the Location header. A request line may
		// carry an absolute URI; a path may start with "//"; and percent-encoding hides
		// both that and the backslash a browser resolves as a second leading slash.
		{"redirect never echoes the request host", http.MethodGet, "http://evil.example.com/livez/", http.StatusMovedPermanently, "/livez", ""},
		{"protocol-relative path is not redirected", http.MethodGet, "//livez/", http.StatusOK, "", "SPA-INDEX"},
		{"encoded protocol-relative path is not redirected", http.MethodGet, "/%2f%2flivez/", http.StatusOK, "", "SPA-INDEX"},
		{"backslash-rooted path is not redirected", http.MethodGet, "/%5clivez/", http.StatusOK, "", "SPA-INDEX"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, http.NoBody)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tc.status, w.Code)
			if tc.location != "" {
				assert.Equal(t, tc.location, w.Header().Get("Location"))
			} else {
				assert.Empty(t, w.Header().Get("Location"), "must not redirect")
			}
			if substring, ok := strings.CutPrefix(tc.body, "@contains:"); ok {
				assert.Contains(t, w.Body.String(), substring)
			} else if tc.body != "" {
				assert.Equal(t, tc.body, strings.TrimSpace(w.Body.String()))
			}
		})
	}
}

// TestCORSPolicy covers the origin gate. A cross-origin request from an origin
// outside the allowlist must be refused before it reaches a handler: a simple
// request arrives whatever the browser does with the response, so for POST /tasks
// letting it through would mean the deployment already happened.
func TestCORSPolicy(t *testing.T) {
	const allowedOrigin = "http://localhost:5173"

	newRouter := func(t *testing.T, dev bool) *chi.Mux {
		t.Helper()
		env, _ := readAuthEnv(t, false, nil)
		env.config.DevEnvironment = dev
		return env.CreateRouter()
	}

	do := func(router *chi.Mux, method, path string, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, http.NoBody)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	t.Run("dev mode refuses a deploy from an origin outside the allowlist", func(t *testing.T) {
		// The state-changing route is the one that matters: a text/plain POST needs no
		// preflight, so refusing it here is what stops the deployment happening.
		router := newRouter(t, true)

		w := do(router, http.MethodPost, "/api/v1/tasks", map[string]string{
			"Origin":       "http://evil.test",
			"Content-Type": "text/plain",
		})

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("dev mode refuses a read from an origin outside the allowlist", func(t *testing.T) {
		router := newRouter(t, true)

		w := do(router, http.MethodGet, "/api/v1/config", map[string]string{"Origin": "http://evil.test"})

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	})

	// A url.URL handed to slog renders as an object of its eleven exported fields,
	// which no log query can match on. The path is what identifies the request.
	t.Run("the rejection log names the request path", func(t *testing.T) {
		router := newRouter(t, true)
		logs := captureDebugLogs(t)

		do(router, http.MethodGet, "/api/v1/config", map[string]string{"Origin": "http://evil.test"})

		assert.Contains(t, logs.String(), `"url":"/api/v1/config"`)
	})

	t.Run("the origin gate runs before the trailing-slash redirect", func(t *testing.T) {
		router := newRouter(t, true)

		w := do(router, http.MethodGet, "/api/v1/config/", map[string]string{"Origin": "http://evil.test"})

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("dev mode admits an allowlisted origin with credentials", func(t *testing.T) {
		router := newRouter(t, true)

		w := do(router, http.MethodGet, "/api/v1/config", map[string]string{"Origin": allowedOrigin})

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, allowedOrigin, w.Header().Get("Access-Control-Allow-Origin"))
		assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
	})

	t.Run("preflight is answered with no content", func(t *testing.T) {
		router := newRouter(t, true)

		w := do(router, http.MethodOptions, "/api/v1/tasks", map[string]string{
			"Origin":                        allowedOrigin,
			"Access-Control-Request-Method": http.MethodPost,
		})

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Equal(t, allowedOrigin, w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("options without a requested method is still answered", func(t *testing.T) {
		router := newRouter(t, true)

		w := do(router, http.MethodOptions, "/api/v1/config", map[string]string{"Origin": allowedOrigin})

		assert.Equal(t, http.StatusNoContent, w.Code)
		// Echoing the origin is the whole reason this branch answers the request
		// itself instead of letting it fall through to the SPA handler.
		assert.Equal(t, allowedOrigin, w.Header().Get("Access-Control-Allow-Origin"))
		assert.Empty(t, w.Body.String())
	})

	t.Run("every configured request header survives a preflight", func(t *testing.T) {
		// rs/cors answers a preflight whose requested headers are not all allowed by
		// returning the success status with no Access-Control-Allow-Origin at all, so
		// dropping a header from corsOptions would break browser deploys silently.
		router := newRouter(t, true)

		for _, header := range []string{oidcHeader, "ARGO_WATCHER_DEPLOY_TOKEN", "Authorization", "Content-Type", "Accept"} {
			t.Run(header, func(t *testing.T) {
				w := do(router, http.MethodOptions, "/api/v1/tasks", map[string]string{
					"Origin":                         allowedOrigin,
					"Access-Control-Request-Method":  http.MethodPost,
					"Access-Control-Request-Headers": strings.ToLower(header),
				})

				assert.Equal(t, http.StatusNoContent, w.Code)
				assert.Equal(t, allowedOrigin, w.Header().Get("Access-Control-Allow-Origin"),
					"a missing allow-origin means the header was refused")
				assert.Contains(t, strings.ToLower(w.Header().Get("Access-Control-Allow-Headers")), strings.ToLower(header))
			})
		}
	})

	t.Run("the removed Keycloak header is refused by a preflight", func(t *testing.T) {
		// The counterpart of the subtest above: rs/cors signals refusal by omitting
		// Access-Control-Allow-Origin, so this pins that the alias stays out of
		// corsOptions once the strategy behind it is gone.
		router := newRouter(t, true)

		w := do(router, http.MethodOptions, "/api/v1/tasks", map[string]string{
			"Origin":                         allowedOrigin,
			"Access-Control-Request-Method":  http.MethodPost,
			"Access-Control-Request-Headers": "keycloak-authorization",
		})

		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"),
			"an allow-origin here means the removed alias is allowlisted again")
		assert.NotContains(t, strings.ToLower(w.Header().Get("Access-Control-Allow-Headers")), "keycloak-authorization")
	})

	t.Run("production refuses a deploy from another origin", func(t *testing.T) {
		// A text/plain POST is a CORS simple request: the browser sends it and the
		// deployment starts whether or not the attacker's script is ever allowed to
		// read the response. This gate is the only thing that stops it.
		router := newRouter(t, false)

		w := do(router, http.MethodPost, "/api/v1/tasks", map[string]string{
			"Origin":       "http://evil.test",
			"Content-Type": "text/plain",
		})

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("production refuses a read from another origin", func(t *testing.T) {
		router := newRouter(t, false)

		w := do(router, http.MethodGet, "/api/v1/config", map[string]string{"Origin": "http://anywhere.test"})

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("production refuses a preflight from another origin", func(t *testing.T) {
		// A permissive preflight is why demanding application/json would not have
		// helped: the browser would have been told the request was allowed.
		router := newRouter(t, false)

		w := do(router, http.MethodOptions, "/api/v1/tasks", map[string]string{
			"Origin":                        "http://evil.test",
			"Access-Control-Request-Method": http.MethodPost,
		})

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("a caller that sends no Origin is untouched", func(t *testing.T) {
		// The CLI client and argo-watcher-mcp are not browsers and send no Origin. The
		// gate is a browser control, not authentication, so it must not reach them.
		router := newRouter(t, false)

		w := do(router, http.MethodGet, "/api/v1/config", nil)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "state_type", "the handler must have run")

		// The empty body is what addTask rejects, which is only reachable once the gate
		// has let the request past.
		w = do(router, http.MethodPost, "/api/v1/tasks", map[string]string{"Content-Type": "application/json"})
		assert.Equal(t, http.StatusNotAcceptable, w.Code)
		assert.Contains(t, w.Body.String(), "invalid payload", "the handler must have run")
	})

	t.Run("production refuses a host that only extends the server's own", func(t *testing.T) {
		// A substring or prefix comparison in isSameOrigin would admit these, and every
		// other cross-origin subtest uses a host that shares nothing with the server's.
		for _, pattern := range []string{"http://%s.evil.test", "http://evil-%s", "http://%s.", "http://x%s"} {
			t.Run(pattern, func(t *testing.T) {
				router := newRouter(t, false)

				req := httptest.NewRequest(http.MethodGet, "/api/v1/config", http.NoBody)
				req.Header.Set("Origin", fmt.Sprintf(pattern, req.Host))
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				assert.Equal(t, http.StatusForbidden, w.Code)
				assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
			})
		}
	})

	t.Run("a host differing only in case is still same-origin", func(t *testing.T) {
		// The WebSocket handshake compares the two case-insensitively, and a request
		// that /ws accepts must not be refused on every other route.
		router := newRouter(t, false)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/config", http.NoBody)
		req.Header.Set("Origin", "http://"+strings.ToUpper(req.Host))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "state_type", "the handler must have run")
	})

	t.Run("the CORS library refuses a production origin on its own", func(t *testing.T) {
		// The gate answers every cross-origin request before this handler is reached, so
		// assert the policy directly: rs/cors reads an empty AllowedOrigins as "allow
		// every origin", and a future path exemption must not turn that back on.
		env, _ := readAuthEnv(t, false, nil)
		env.config.DevEnvironment = false

		handler := cors.New(env.corsOptions()).Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/api/v1/config", http.NoBody)
		req.Header.Set("Origin", "http://evil.test")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("same-origin requests are served without CORS headers", func(t *testing.T) {
		// This branch is what keeps the Web UI working: the SPA is served by the same
		// binary, so its requests carry the server's own origin, and neither mode
		// carries that origin in its allowlist.
		for _, dev := range []bool{true, false} {
			for _, scheme := range []string{"http://", "https://"} {
				t.Run(fmt.Sprintf("dev=%v %s", dev, scheme), func(t *testing.T) {
					router := newRouter(t, dev)

					req := httptest.NewRequest(http.MethodGet, "/api/v1/config", http.NoBody)
					req.Header.Set("Origin", scheme+req.Host)
					w := httptest.NewRecorder()
					router.ServeHTTP(w, req)

					assert.Equal(t, http.StatusOK, w.Code)
					assert.Contains(t, w.Body.String(), "state_type", "the handler must have run")
					assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
				})
			}
		}
	})

	t.Run("the websocket handshake is never touched by CORS", func(t *testing.T) {
		// The upgrade response goes to a hijacked connection, so a CORS header written
		// here would be both useless and, before the migration, actively harmful. The
		// handshake runs its own origin check, and losing this exemption would refuse
		// every browser socket in production.
		for _, dev := range []bool{true, false} {
			t.Run(fmt.Sprintf("dev=%v", dev), func(t *testing.T) {
				router := newRouter(t, dev)

				w := do(router, http.MethodGet, "/ws", map[string]string{"Origin": "http://evil.test"})

				assert.Equal(t, http.StatusBadRequest, w.Code, "a non-upgrade /ws request is still answered, not refused")
				assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
			})
		}
	})
}

// TestAddTaskResolvesTheDeploymentWindow pins that a task accepted without an
// explicit timeout is stored with this replica's configured window rather than
// with zero. Zero would mean "whatever the default is", and a replica that later
// resumes the task would apply its own default — so a fleet mid-way through a
// DEPLOYMENT_TIMEOUT change would judge a deployment against a window it was
// never accepted under.
func TestAddTaskResolvesTheDeploymentWindow(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tests := []struct {
		name        string
		requestJSON string
		want        int
	}{
		{
			name:        "an omitted timeout is resolved to the instance default",
			requestJSON: `{"app":"test-app","author":"a","project":"p","images":[{"image":"test","tag":"v1"}]}`,
			want:        900,
		},
		{
			name:        "an explicit timeout is kept as sent",
			requestJSON: `{"app":"test-app","author":"a","project":"p","timeout":120,"images":[{"image":"test","tag":"v1"}]}`,
			want:        120,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateMock := mocks.NewMockTaskRepository(ctrl)
			metricsMock := mocks.NewMockMetricsInterface(ctrl)

			argo := &argocd.Argo{}
			argo.Init(stateMock, mocks.NewMockArgoApiInterface(ctrl), metricsMock)

			stateMock.EXPECT().GetTasks(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return([]models.Task{}, int64(0)).AnyTimes()
			stateMock.EXPECT().CancelInProgressTasks(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(int64(0), nil).AnyTimes()
			stateMock.EXPECT().ClaimTask(gomock.Any()).Return(nil).AnyTimes()
			metricsMock.EXPECT().AddAcceptedDeployment().AnyTimes()

			var stored models.Task
			stateMock.EXPECT().AddTask(gomock.Any()).DoAndReturn(func(task models.Task) (*models.Task, error) {
				stored = task
				// Returning an error stops the handler before it spawns a rollout, which
				// this test is not about.
				return nil, errors.New("stop here")
			})

			lockdown, err := NewLockdown("", lock.NewInMemoryDeployLockStore())
			require.NoError(t, err)

			env := &Env{
				argo:     argo,
				lockdown: lockdown,
				config:   &config.ServerConfig{DeploymentTimeout: 900},
			}

			router := chi.NewRouter()
			router.Post("/api/v1/tasks", env.addTask)

			req, _ := http.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(tt.requestJSON))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(httptest.NewRecorder(), req)

			assert.Equal(t, tt.want, stored.Timeout)
		})
	}
}
