package server

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// SetDeployLock godoc
// @Summary Set deploy lock
// @Description Set deploy lock. Only available when Keycloak is enabled; requires a valid Keycloak session.
// @Tags frontend
// @Success 200 {string} string
// @Failure 401 {object} models.TaskStatus
// @Router /api/v1/deploy-lock [post]
func (env *Env) SetDeployLock(c *gin.Context) {
	if !env.requireKeycloakAuth(c) {
		return
	}

	env.lockdown.SetLock()

	slog.Debug("deploy lock is set")

	notifyWebSocketClients("locked")

	c.JSON(http.StatusOK, "deploy lock is set")
}

// ReleaseDeployLock godoc
// @Summary Release deploy lock
// @Description Release deploy lock. Only available when Keycloak is enabled; requires a valid Keycloak session.
// @Tags frontend
// @Success 200 {string} string
// @Failure 401 {object} models.TaskStatus
// @Router /api/v1/deploy-lock [delete]
func (env *Env) ReleaseDeployLock(c *gin.Context) {
	if !env.requireKeycloakAuth(c) {
		return
	}

	env.lockdown.ReleaseLock()

	slog.Debug("deploy lock is released")

	notifyWebSocketClients("unlocked")

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
