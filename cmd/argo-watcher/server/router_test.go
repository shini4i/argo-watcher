package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/argocd"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/prometheus"

	"nhooyr.io/websocket"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

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
		Host: "localhost",
		Port: "8080",
	}

	argo := &argocd.Argo{}
	metrics := &prometheus.Metrics{}
	updater := &argocd.ArgoStatusUpdater{}

	env := NewEnv(serverConfig, argo, metrics, updater)

	assert.Equal(t, env.config, serverConfig)
	assert.Equal(t, env.argo, argo)
	assert.Equal(t, env.metrics, metrics)
	assert.Equal(t, env.updater, updater)
}
