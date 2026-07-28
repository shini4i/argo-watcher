package server

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/shini4i/argo-watcher/internal/models"
)

// This handler deliberately does not push the new state to WebSocket clients.
// StartLockdownWatcher is the single notifier — it compares each poll against the
// state it last broadcast, and a push from here would leave that baseline stale
// (see TestDeployLockNotifiedOnlyByWatcher).
//
// SetDeployLock godoc
// @Summary Set deploy lock
// @Description Set deploy lock. Only available when OIDC auth is enabled; requires a valid OIDC session.
// @Tags frontend
// @Success 200 {string} string
// @Failure 401 {object} models.TaskStatus
// @Failure 500 {object} models.TaskStatus
// @Router /api/v1/deploy-lock [post]
func (env *Env) SetDeployLock(c *gin.Context) {
	if !env.requireOIDCAuth(c) {
		return
	}

	if err := env.lockdown.SetLock(); err != nil {
		// The caller must not believe deployments are frozen when they are not.
		slog.Error("failed to set deploy lock", "error", err)
		c.JSON(http.StatusInternalServerError, models.TaskStatus{
			Status: "failed to set deploy lock",
			Error:  "internal server error",
		})
		return
	}

	slog.Debug("deploy lock is set")

	c.JSON(http.StatusOK, "deploy lock is set")
}

// ReleaseDeployLock godoc
// @Summary Release deploy lock
// @Description Release deploy lock. Only available when OIDC auth is enabled; requires a valid OIDC session.
// @Tags frontend
// @Success 200 {string} string
// @Failure 401 {object} models.TaskStatus
// @Failure 500 {object} models.TaskStatus
// @Router /api/v1/deploy-lock [delete]
func (env *Env) ReleaseDeployLock(c *gin.Context) {
	if !env.requireOIDCAuth(c) {
		return
	}

	if err := env.lockdown.ReleaseLock(); err != nil {
		slog.Error("failed to release deploy lock", "error", err)
		c.JSON(http.StatusInternalServerError, models.TaskStatus{
			Status: "failed to release deploy lock",
			Error:  "internal server error",
		})
		return
	}

	slog.Debug("deploy lock is released")

	c.JSON(http.StatusOK, "deploy lock is released")
}

// isDeployLockSet godoc
// @Summary Check if deploy lock is set
// @Description Check if deploy lock is set
// @Tags frontend
// @Success 200 {boolean} boolean
// @Router /api/v1/deploy-lock [get]
func (env *Env) isDeployLockSet(c *gin.Context) {
	c.JSON(http.StatusOK, env.lockdown.IsLocked())
}
