package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/argocd"
	"github.com/shini4i/argo-watcher/internal/models"
)

// TestAddTask tests the addTask handler with various scenarios
func TestAddTask(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Success - Valid Task", func(t *testing.T) {
		repository := &fakeTaskRepository{
			tasks: []models.Task{},
		}

		argo := &argocd.Argo{}
		argo.Init(repository, &fakeArgoAPI{}, &fakeMetrics{})

		env := &Env{
			config:   &config.ServerConfig{},
			argo:     argo,
			updater:  &fakeUpdater{}, // Use fake updater for tests
			lockdown: &Lockdown{ManualLock: false},
		}

		router := gin.Default()
		router.POST("/api/v1/tasks", env.addTask)

		task := models.Task{
			App:     "test-app",
			Project: "test-project",
			Author:  "test-user",
			Images: []models.Image{
				{Image: "nginx", Tag: "latest"},
			},
		}
		body, _ := json.Marshal(task)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusAccepted, resp.Code)

		var result models.TaskStatus
		err := json.Unmarshal(resp.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.Equal(t, models.StatusAccepted, result.Status)
		assert.NotEmpty(t, result.Id)
	})

	t.Run("Error - Invalid JSON Payload", func(t *testing.T) {
		env := &Env{
			config:   &config.ServerConfig{},
			lockdown: &Lockdown{ManualLock: false},
		}

		router := gin.Default()
		router.POST("/api/v1/tasks", env.addTask)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewBufferString("invalid json"))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusNotAcceptable, resp.Code)
		assert.Contains(t, resp.Body.String(), "invalid payload")
	})

	t.Run("Error - Deploy Lock Active", func(t *testing.T) {
		env := &Env{
			config:   &config.ServerConfig{},
			lockdown: &Lockdown{ManualLock: true},
		}

		router := gin.Default()
		router.POST("/api/v1/tasks", env.addTask)

		task := models.Task{
			App:     "test-app",
			Project: "test-project",
			Author:  "test-user",
			Images: []models.Image{
				{Image: "nginx", Tag: "latest"},
			},
		}
		body, _ := json.Marshal(task)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusNotAcceptable, resp.Code)
		assert.Contains(t, resp.Body.String(), "rejected")
		assert.Contains(t, resp.Body.String(), "lockdown is active")
	})

	t.Run("Error - Argo AddTask Fails", func(t *testing.T) {
		repository := &fakeTaskRepositoryWithError{
			addTaskError: errors.New("database error"),
		}

		argo := &argocd.Argo{}
		argo.Init(repository, &fakeArgoAPI{}, &fakeMetrics{})

		env := &Env{
			config:   &config.ServerConfig{},
			argo:     argo,
			lockdown: &Lockdown{ManualLock: false},
		}

		router := gin.Default()
		router.POST("/api/v1/tasks", env.addTask)

		task := models.Task{
			App:     "test-app",
			Project: "test-project",
			Author:  "test-user",
			Images: []models.Image{
				{Image: "nginx", Tag: "latest"},
			},
		}
		body, _ := json.Marshal(task)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusServiceUnavailable, resp.Code)
		assert.Contains(t, resp.Body.String(), "down")
	})
}

// TestStateHandler tests the stateHandler endpoint
func TestStateHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Success - Get All Tasks", func(t *testing.T) {
		repository := &fakeTaskRepository{
			tasks: []models.Task{
				{
					Id:      "1",
					App:     "app1",
					Project: "proj1",
					Status:  "deployed",
				},
				{
					Id:      "2",
					App:     "app2",
					Project: "proj2",
					Status:  "pending",
				},
			},
		}

		argo := &argocd.Argo{}
		argo.Init(repository, &fakeArgoAPI{}, &fakeMetrics{})

		env := &Env{
			config: &config.ServerConfig{},
			argo:   argo,
		}

		router := gin.Default()
		router.GET("/api/v1/tasks", env.stateHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)

		var result models.TasksResponse
		err := json.Unmarshal(resp.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.Len(t, result.Tasks, 2)
		assert.Equal(t, int64(2), result.Total)
	})

	t.Run("Success - Filter By App", func(t *testing.T) {
		repository := &fakeTaskRepository{
			tasks: []models.Task{
				{
					Id:      "1",
					App:     "app1",
					Project: "proj1",
					Status:  "deployed",
				},
				{
					Id:      "2",
					App:     "app2",
					Project: "proj2",
					Status:  "pending",
				},
			},
		}

		argo := &argocd.Argo{}
		argo.Init(repository, &fakeArgoAPI{}, &fakeMetrics{})

		env := &Env{
			config: &config.ServerConfig{},
			argo:   argo,
		}

		router := gin.Default()
		router.GET("/api/v1/tasks", env.stateHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?app=app1", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)

		var result models.TasksResponse
		err := json.Unmarshal(resp.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.Len(t, result.Tasks, 1)
		assert.Equal(t, "app1", result.Tasks[0].App)
	})

	t.Run("Success - With Limit And Offset", func(t *testing.T) {
		repository := &fakeTaskRepository{
			tasks: []models.Task{
				{Id: "1", App: "app1"},
				{Id: "2", App: "app2"},
				{Id: "3", App: "app3"},
			},
		}

		argo := &argocd.Argo{}
		argo.Init(repository, &fakeArgoAPI{}, &fakeMetrics{})

		env := &Env{
			config: &config.ServerConfig{},
			argo:   argo,
		}

		router := gin.Default()
		router.GET("/api/v1/tasks", env.stateHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?limit=1&offset=1", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)

		var result models.TasksResponse
		err := json.Unmarshal(resp.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.Len(t, result.Tasks, 1)
		assert.Equal(t, "2", result.Tasks[0].Id)
	})

	t.Run("Error - Invalid from_timestamp", func(t *testing.T) {
		env := &Env{
			config: &config.ServerConfig{},
			argo: &argocd.Argo{
				State: &fakeTaskRepository{},
			},
		}

		router := gin.Default()
		router.GET("/api/v1/tasks", env.stateHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?from_timestamp=invalid", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assert.Contains(t, resp.Body.String(), "invalid from_timestamp")
	})

	t.Run("Error - Invalid to_timestamp", func(t *testing.T) {
		env := &Env{
			config: &config.ServerConfig{},
			argo: &argocd.Argo{
				State: &fakeTaskRepository{},
			},
		}

		router := gin.Default()
		router.GET("/api/v1/tasks", env.stateHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?to_timestamp=invalid", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assert.Contains(t, resp.Body.String(), "invalid to_timestamp")
	})

	t.Run("Error - Invalid limit", func(t *testing.T) {
		env := &Env{
			config: &config.ServerConfig{},
			argo: &argocd.Argo{
				State: &fakeTaskRepository{},
			},
		}

		router := gin.Default()
		router.GET("/api/v1/tasks", env.stateHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?limit=invalid", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assert.Contains(t, resp.Body.String(), "invalid limit")
	})

	t.Run("Error - Invalid offset", func(t *testing.T) {
		env := &Env{
			config: &config.ServerConfig{},
			argo: &argocd.Argo{
				State: &fakeTaskRepository{},
			},
		}

		router := gin.Default()
		router.GET("/api/v1/tasks", env.stateHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?offset=invalid", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assert.Contains(t, resp.Body.String(), "invalid offset")
	})
}

// TestTaskStatusHandler tests the taskStatusHandler endpoint
func TestTaskStatusHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Success - Task Found", func(t *testing.T) {
		task := &models.Task{
			Id:      "test-id-123",
			App:     "test-app",
			Project: "test-project",
			Status:  "deployed",
			Images: []models.Image{
				{Image: "nginx", Tag: "latest"},
			},
		}

		repository := &fakeTaskRepositoryWithGet{
			task: task,
		}

		argo := &argocd.Argo{}
		argo.Init(repository, &fakeArgoAPI{}, &fakeMetrics{})

		env := &Env{
			config: &config.ServerConfig{},
			argo:   argo,
		}

		router := gin.Default()
		router.GET("/api/v1/tasks/:id", env.taskStatusHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/test-id-123", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)

		var result models.TaskStatus
		err := json.Unmarshal(resp.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.Equal(t, "test-id-123", result.Id)
		assert.Equal(t, "test-app", result.App)
		assert.Equal(t, "deployed", result.Status)
	})

	t.Run("Error - Task Not Found", func(t *testing.T) {
		repository := &fakeTaskRepositoryWithGet{
			err: gorm.ErrRecordNotFound,
		}

		argo := &argocd.Argo{}
		argo.Init(repository, &fakeArgoAPI{}, &fakeMetrics{})

		env := &Env{
			config: &config.ServerConfig{},
			argo:   argo,
		}

		router := gin.Default()
		router.GET("/api/v1/tasks/:id", env.taskStatusHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/non-existent", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusNotFound, resp.Code)
		assert.Contains(t, resp.Body.String(), "record not found")
	})

	t.Run("Error - Internal Server Error", func(t *testing.T) {
		repository := &fakeTaskRepositoryWithGet{
			err: errors.New("database connection lost"),
		}

		argo := &argocd.Argo{}
		argo.Init(repository, &fakeArgoAPI{}, &fakeMetrics{})

		env := &Env{
			config: &config.ServerConfig{},
			argo:   argo,
		}

		router := gin.Default()
		router.GET("/api/v1/tasks/:id", env.taskStatusHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/test-id", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusInternalServerError, resp.Code)
		assert.Contains(t, resp.Body.String(), "database connection lost")
	})
}

// TestHealthz tests the healthz endpoint
func TestHealthz(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Success - Service Healthy", func(t *testing.T) {
		repository := &fakeTaskRepository{}

		argo := &argocd.Argo{}
		argo.Init(repository, &fakeArgoAPI{}, &fakeMetrics{})

		env := &Env{
			config: &config.ServerConfig{},
			argo:   argo,
		}

		router := gin.Default()
		router.GET("/healthz", env.healthz)

		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)

		var result models.HealthStatus
		err := json.Unmarshal(resp.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.Equal(t, "up", result.Status)
	})

	t.Run("Error - Service Unavailable", func(t *testing.T) {
		repository := &fakeTaskRepositoryUnhealthy{}

		argo := &argocd.Argo{}
		argo.Init(repository, &fakeArgoAPI{}, &fakeMetrics{})

		env := &Env{
			config: &config.ServerConfig{},
			argo:   argo,
		}

		router := gin.Default()
		router.GET("/healthz", env.healthz)

		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusServiceUnavailable, resp.Code)

		var result models.HealthStatus
		err := json.Unmarshal(resp.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.Equal(t, "down", result.Status)
	})
}

// TestConfigHandler tests the configHandler endpoint
func TestConfigHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Success - Returns Config", func(t *testing.T) {
		testConfig := &config.ServerConfig{
			StateType:     "in-memory",
			LogLevel:      "info",
			SkipTlsVerify: true,
		}

		env := &Env{
			config: testConfig,
		}

		router := gin.Default()
		router.GET("/api/v1/config", env.configHandler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)

		var result config.ServerConfig
		err := json.Unmarshal(resp.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.Equal(t, "in-memory", result.StateType)
		assert.Equal(t, "info", result.LogLevel)
		assert.True(t, result.SkipTlsVerify)
	})
}

// Fake implementations for testing

type fakeTaskRepositoryWithError struct {
	fakeTaskRepository
	addTaskError error
}

func (f *fakeTaskRepositoryWithError) AddTask(task models.Task) (*models.Task, error) {
	if f.addTaskError != nil {
		return nil, f.addTaskError
	}
	return f.fakeTaskRepository.AddTask(task)
}

type fakeTaskRepositoryWithGet struct {
	task *models.Task
	err  error
}

func (f *fakeTaskRepositoryWithGet) Connect(_ *config.ServerConfig) error             { return nil }
func (f *fakeTaskRepositoryWithGet) AddTask(task models.Task) (*models.Task, error)   { return &task, nil }
func (f *fakeTaskRepositoryWithGet) GetTask(_ string) (*models.Task, error)           { return f.task, f.err }
func (f *fakeTaskRepositoryWithGet) SetTaskStatus(_ string, _ string, _ string) error { return nil }
func (f *fakeTaskRepositoryWithGet) Check() bool                                      { return true }
func (f *fakeTaskRepositoryWithGet) ProcessObsoleteTasks(_ uint)                      {}
func (f *fakeTaskRepositoryWithGet) GetTasks(_ float64, _ float64, app string, limit, offset int) ([]models.Task, int64) {
	return []models.Task{}, 0
}

type fakeTaskRepositoryUnhealthy struct {
	fakeTaskRepository
}

func (f *fakeTaskRepositoryUnhealthy) Check() bool {
	return false
}

type fakeArgoAPI struct{}

func (f *fakeArgoAPI) Init(_ *config.ServerConfig) error {
	return nil
}

func (f *fakeArgoAPI) GetUserInfo() (*models.Userinfo, error) {
	return &models.Userinfo{LoggedIn: true}, nil
}

func (f *fakeArgoAPI) GetApplication(_ string) (*models.Application, error) {
	return &models.Application{}, nil
}

type fakeMetrics struct{}

func (f *fakeMetrics) AddProcessedDeployment(_ string) {}
func (f *fakeMetrics) AddFailedDeployment(_ string)    {}
func (f *fakeMetrics) ResetFailedDeployment(_ string)  {}
func (f *fakeMetrics) SetArgoUnavailable(_ bool)       {}
func (f *fakeMetrics) AddInProgressTask()              {}
func (f *fakeMetrics) RemoveInProgressTask()           {}

type fakeUpdater struct{}

func (f *fakeUpdater) WaitForRollout(_ models.Task) {
	// Do nothing in tests
}
