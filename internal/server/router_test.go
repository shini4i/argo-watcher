package server

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

const exportEndpoint = "/api/v1/tasks/export"

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

// TestExportTasksCSV ensures CSV exports stream the expected rows and headers.
func TestExportTasksCSV(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &fakeTaskRepository{
		tasks: []models.Task{
			{
				Id:      "1",
				App:     "demo",
				Project: "proj",
				Status:  "ok",
				Images: []models.Image{
					{Image: "svc", Tag: "1"},
				},
				Author:       "alice",
				StatusReason: "done",
			},
		},
	}

	env := &Env{
		config: &config.ServerConfig{},
		argo: &argocd.Argo{
			State: repository,
		},
	}

	router := gin.Default()
	router.GET(exportEndpoint, env.exportTasks)

	req := httptest.NewRequest(http.MethodGet, exportEndpoint+"?format=csv&anonymize=false", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "text/csv", resp.Header().Get("Content-Type"))
	disposition := resp.Header().Get("Content-Disposition")
	assert.Contains(t, disposition, "attachment; filename=history-tasks-")
	assert.True(t, strings.HasSuffix(disposition, ".csv"))
	reader := csv.NewReader(strings.NewReader(resp.Body.String()))
	rows, err := reader.ReadAll()
	assert.NoError(t, err)
	assert.Len(t, rows, 2)
	assert.Equal(t, "demo", rows[1][1])
	assert.Equal(t, "alice", rows[1][7])
}

// TestExportTasksJSONHeaders verifies JSON exports respond with correct metadata.
func TestExportTasksJSONHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &fakeTaskRepository{
		tasks: []models.Task{
			{
				Id:      "1",
				App:     "demo",
				Project: "proj",
				Status:  "ok",
				Images: []models.Image{
					{Image: "svc", Tag: "1"},
				},
				Author:       "alice",
				StatusReason: "done",
			},
		},
	}

	env := &Env{
		config: &config.ServerConfig{},
		argo: &argocd.Argo{
			State: repository,
		},
	}

	router := gin.Default()
	router.GET(exportEndpoint, env.exportTasks)

	req := httptest.NewRequest(http.MethodGet, exportEndpoint+"?format=json", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "application/json", resp.Header().Get("Content-Type"))
	disposition := resp.Header().Get("Content-Disposition")
	assert.Contains(t, disposition, "attachment; filename=history-tasks-")
	assert.True(t, strings.HasSuffix(disposition, ".json"))

	var payload []map[string]any
	err := json.Unmarshal(resp.Body.Bytes(), &payload)
	assert.NoError(t, err)
	assert.Len(t, payload, 1)
	assert.Equal(t, "demo", payload[0]["app"])
}

// TestExportTasksRejectsInvalidFormat ensures unsupported formats are rejected.
func TestExportTasksRejectsInvalidFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := &Env{
		config: &config.ServerConfig{},
		argo: &argocd.Argo{
			State: &fakeTaskRepository{},
		},
	}

	router := gin.Default()
	router.GET(exportEndpoint, env.exportTasks)

	req := httptest.NewRequest(http.MethodGet, exportEndpoint+"?format=xml", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Contains(t, resp.Body.String(), "unsupported export format")
}

func TestExportTasksRequiresKeycloak(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repository := &fakeTaskRepository{}
	env := &Env{
		config: &config.ServerConfig{
			Keycloak: config.KeycloakConfig{
				Enabled: true,
			},
		},
		argo: &argocd.Argo{
			State: repository,
		},
		strategies: map[string]auth.AuthStrategy{
			keycloakHeader: fakeStrategy{valid: false},
		},
	}

	router := gin.Default()
	router.GET(exportEndpoint, env.exportTasks)

	req := httptest.NewRequest(http.MethodGet, exportEndpoint, nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusUnauthorized, resp.Code)
	assert.Contains(t, resp.Body.String(), unauthorizedMessage)
}

type fakeTaskRepository struct {
	tasks []models.Task
}

func (f *fakeTaskRepository) Connect(_ *config.ServerConfig) error             { return nil }
func (f *fakeTaskRepository) AddTask(task models.Task) (*models.Task, error)   { return &task, nil }
func (f *fakeTaskRepository) GetTask(_ string) (*models.Task, error)           { return nil, nil }
func (f *fakeTaskRepository) SetTaskStatus(_ string, _ string, _ string) error { return nil }
func (f *fakeTaskRepository) Check() bool                                      { return true }
func (f *fakeTaskRepository) ProcessObsoleteTasks(_ uint)                      {}
func (f *fakeTaskRepository) GetTasks(_ float64, _ float64, app string, limit, offset int) ([]models.Task, int64) {
	filtered := f.tasks
	if app != "" {
		filtered = make([]models.Task, 0, len(f.tasks))
		for _, task := range f.tasks {
			if task.App == app {
				filtered = append(filtered, task)
			}
		}
	}

	total := len(filtered)
	if offset >= total {
		return []models.Task{}, int64(total)
	}

	end := offset + limit
	if limit <= 0 || end > total {
		end = total
	}

	return filtered[offset:end], int64(total)
}

type fakeStrategy struct {
	valid bool
	err   error
}

func (f fakeStrategy) Validate(string) (bool, error) {
	return f.valid, f.err
}
