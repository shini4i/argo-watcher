package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/prometheus"
	"github.com/shini4i/argo-watcher/internal/argocd"
	"github.com/shini4i/argo-watcher/internal/auth"
	"github.com/shini4i/argo-watcher/internal/models"
)

// mockArgoApi is a minimal mock for ArgoApiInterface used in tests.
type mockArgoApi struct{}

func (m *mockArgoApi) Init(_ *config.ServerConfig) error {
	return nil
}

func (m *mockArgoApi) GetUserInfo() (*models.Userinfo, error) {
	return &models.Userinfo{LoggedIn: true, Username: "test"}, nil
}

func (m *mockArgoApi) GetApplication(_ string) (*models.Application, error) {
	return &models.Application{}, nil
}

// mockTaskRepository is a minimal mock for TaskRepository used in tests.
type mockTaskRepository struct{}

func (m *mockTaskRepository) Connect(_ *config.ServerConfig) error {
	return nil
}

func (m *mockTaskRepository) AddTask(task models.Task) (*models.Task, error) {
	return &task, nil
}

func (m *mockTaskRepository) GetTasks(_, _ float64, _ string, _, _ int) ([]models.Task, int64) {
	return []models.Task{}, 0
}

func (m *mockTaskRepository) GetTask(_ string) (*models.Task, error) {
	return &models.Task{}, nil
}

func (m *mockTaskRepository) SetTaskStatus(_, _, _ string) error {
	return nil
}

func (m *mockTaskRepository) Check() bool {
	return true
}

func (m *mockTaskRepository) ProcessObsoleteTasks(_ uint) {}

// mockMetrics is a minimal mock for MetricsInterface used in tests.
type mockMetrics struct{}

func (m *mockMetrics) AddProcessedDeployment(_ string) {}
func (m *mockMetrics) AddFailedDeployment(_ string)    {}
func (m *mockMetrics) ResetFailedDeployment(_ string)  {}
func (m *mockMetrics) SetArgoUnavailable(_ bool)       {}
func (m *mockMetrics) AddInProgressTask()              {}
func (m *mockMetrics) RemoveInProgressTask()           {}

func TestGetVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.Default()
	env := &Env{}
	router.GET("/api/v1/version", env.getVersion)

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

	gin.SetMode(gin.TestMode)

	dummyConfig := &config.ServerConfig{}

	router := gin.Default()
	env := &Env{config: dummyConfig}

	env.lockdown, err = NewLockdown(dummyConfig.LockdownSchedule)
	assert.NoError(t, err)

	t.Run("SetDeployLock", func(t *testing.T) {
		router.POST("/api/v1/deploy-lock", env.SetDeployLock)

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
		router.DELETE("/api/v1/deploy-lock", env.ReleaseDeployLock)

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
		router.GET("/api/v1/deploy-lock", env.isDeployLockSet)

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

func TestRemoveWebSocketConnection(t *testing.T) {
	conn := &websocket.Conn{}
	connections = append(connections, conn)
	removeWebSocketConnection(conn)
	assert.NotContains(t, connections, conn)
}

func TestNewEnv(t *testing.T) {
	serverConfig := &config.ServerConfig{
		Host:        "localhost",
		Port:        "8080",
		DeployToken: "deployToken",
		Keycloak: config.KeycloakConfig{
			Enabled: true,
		},
		JWTSecret: "jwtSecret",
	}

	argo := &argocd.Argo{}
	metrics := &prometheus.Metrics{}
	updater := &argocd.ArgoStatusUpdater{}

	env, err := NewEnv(serverConfig, argo, metrics, updater)

	assert.NoError(t, err)
	assert.Equal(t, env.config, serverConfig)
	assert.Equal(t, env.argo, argo)
	assert.Equal(t, env.metrics, metrics)
	assert.Equal(t, env.updater, updater)

	expectedStrategies := map[string]auth.AuthStrategy{
		"ARGO_WATCHER_DEPLOY_TOKEN": auth.NewDeployTokenAuthService(serverConfig.DeployToken),
		"Authorization":             auth.NewJWTAuthService(serverConfig.JWTSecret),
		keycloakHeader:              auth.NewKeycloakAuthService(serverConfig),
	}

	assert.Equal(t, expectedStrategies, env.strategies)
	assert.NotNil(t, env.authenticator)
}

// TestGetStateInvalidQueryParams verifies that the getState handler gracefully handles
// invalid query parameters by logging debug messages and using default values.
func TestGetStateInvalidQueryParams(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Set up Argo with mock dependencies
	argo := &argocd.Argo{}
	argo.Init(&mockTaskRepository{}, &mockArgoApi{}, &mockMetrics{})

	env := &Env{
		argo:   argo,
		config: &config.ServerConfig{},
	}

	router := gin.Default()
	router.GET("/api/v1/tasks", env.getState)

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

			// The handler should return 200 OK even with invalid params
			// (it falls back to defaults and logs debug messages)
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}
