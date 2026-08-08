package server

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shini4i/argo-watcher/internal/auth"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/state"
)

var version = "local"

// maxTaskListLimit caps the page size accepted by GET /api/v1/tasks. The
// underlying backends treat limit <= 0 as "no LIMIT clause", which would let
// any caller drain the entire task table in a single request. The cap is
// applied at the HTTP boundary so the data layer stays simple.
const maxTaskListLimit = 1000

const (
	unauthorizedMessage = "You are not authorized to perform this action"
	// oidcHeader is the canonical header carrying the OIDC bearer token for
	// privileged (deploy-lock/rollback) requests. legacyKeycloakHeader is the
	// deprecated alias still accepted for backward compatibility.
	oidcHeader           = "Oidc-Authorization"
	legacyKeycloakHeader = "Keycloak-Authorization"
	// taskAppKey is where getTaskStatus leaves the app label of the resolved task for
	// middleware that runs after it; countUnauthenticatedRead labels with it.
	taskAppKey = "task_app"
)

// getVersion godoc
// @Summary Get the version of the server
// @Description Get the version of the server
// @Tags frontend
// @Success 200 {string} string
// @Failure 401 {object} models.TaskStatus "no credential, or the credential was rejected (only when OIDC auth is enabled)"
// @Failure 503 {object} models.TaskStatus "the OIDC provider could not be consulted; retry"
// @Router /api/v1/version [get]
func (env *Env) getVersion(c *gin.Context) {
	c.JSON(http.StatusOK, version)
}

// addTask godoc
// @Summary Add a new task
// @Description Add a new task
// @Tags backend
// @Accept json
// @Produce json
// @Param task body models.Task true "Task"
// @Success 202 {object} models.TaskStatus
// @Failure 401 {object} models.TaskStatus
// @Failure 406 {object} models.TaskStatus
// @Router /api/v1/tasks [post]
func (env *Env) addTask(c *gin.Context) {
	var task models.Task

	err := c.ShouldBindJSON(&task)
	if err != nil {
		slog.Error("failed to parse task payload", "error", err)
		c.JSON(http.StatusNotAcceptable, models.TaskStatus{
			Status: "invalid payload",
			Error:  err.Error(),
		})
		return
	}

	// reject deploys while a lockdown (manual or scheduled) is active
	if env.lockdown.IsLocked() {
		slog.Warn("deploy lock is set, rejecting the task")
		c.JSON(http.StatusNotAcceptable, models.TaskStatus{
			Status: "rejected",
			Error:  "lockdown is active, deployments are not accepted",
		})
		return
	}

	tokenValid, err := env.validateToken(c, "")
	if err != nil {
		// A non-nil error means the strategy was invoked and rejected the
		// token: a client mistake, not a server failure. Return 401 with
		// the reason so the client can show something actionable.
		slog.Warn("rejecting task", "error", err)
		c.JSON(http.StatusUnauthorized, models.TaskStatus{
			Status: unauthorizedMessage,
			Error:  err.Error(),
		})
		return
	}

	task.Validated = tokenValid

	newTask, err := env.argo.AddTask(task)
	if err != nil {
		slog.Error("failed to add task", "error", err)
		c.JSON(http.StatusServiceUnavailable, models.TaskStatus{
			Status: "down",
			Error:  err.Error(),
		})
		return
	}

	go env.updater.WaitForRollout(*newTask)

	c.JSON(http.StatusAccepted, models.TaskStatus{
		Id:     newTask.Id,
		Status: models.StatusAccepted,
	})
}

// getState godoc
// @Summary Get state content
// @Description Get all tasks that match the provided parameters
// @Tags backend, frontend
// @Param app query string false "App name"
// @Param status query string false "Task status (e.g. 'in progress', 'failed', 'deployed', 'cancelled')"
// @Param from_timestamp query int true "From timestamp" default(1648390029)
// @Param to_timestamp query int false "To timestamp"
// @Param limit query int false "Maximum number of tasks to return (1-1000, defaults to 1000)"
// @Param offset query int false "Number of tasks to skip before returning results"
// @Success 200 {object} models.TasksResponse
// @Failure 401 {object} models.TaskStatus "no credential, or the credential was rejected (only when OIDC auth is enabled)"
// @Failure 503 {object} models.TaskStatus "the OIDC provider could not be consulted; retry"
// @Router /api/v1/tasks [get]
func (env *Env) getState(c *gin.Context) {
	startTime, err := strconv.ParseFloat(c.Query("from_timestamp"), 64)
	if err != nil && c.Query("from_timestamp") != "" {
		slog.Debug("invalid from_timestamp, defaulting to 0", "from_timestamp", c.Query("from_timestamp"))
	}
	endTime, err := strconv.ParseFloat(c.Query("to_timestamp"), 64)
	if err != nil && c.Query("to_timestamp") != "" {
		slog.Debug("invalid to_timestamp, defaulting to current time", "to_timestamp", c.Query("to_timestamp"))
	}
	if endTime == 0 {
		endTime = float64(time.Now().Unix())
	}
	app := c.Query("app")
	status := c.Query("status")
	if status != "" && !models.IsAllowedTaskStatus(status) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported status filter"})
		return
	}

	limit, err := strconv.Atoi(c.Query("limit"))
	if err != nil && c.Query("limit") != "" {
		slog.Debug("invalid limit, defaulting to 0", "limit", c.Query("limit"))
	}
	offset, err := strconv.Atoi(c.Query("offset"))
	if err != nil && c.Query("offset") != "" {
		slog.Debug("invalid offset, defaulting to 0", "offset", c.Query("offset"))
	}
	if limit <= 0 || limit > maxTaskListLimit {
		limit = maxTaskListLimit
	}
	if offset < 0 {
		offset = 0
	}

	c.JSON(http.StatusOK, env.argo.GetTasks(startTime, endTime, app, status, limit, offset))
}

// getTaskStatus godoc
// @Summary Get the status of a task
// @Description Get the status of a task
// @Param id path string true "Task id" default(9185fae0-add5-11ec-87f3-56b185c552fa)
// @Tags backend
// @Produce json
// @Success 200 {object} models.TaskStatus
// @Failure 404 {object} models.TaskStatus
// @Failure 500 {object} models.TaskStatus
// @Router /api/v1/tasks/{id} [get]
func (env *Env) getTaskStatus(c *gin.Context) {
	id := c.Param("id")
	task, err := env.argo.State.GetTask(id)

	if err != nil {
		if errors.Is(err, state.ErrTaskNotFound) {
			c.JSON(http.StatusNotFound, models.TaskStatus{
				Id:    id,
				Error: "task not found",
			})
			return
		}
		// Any other error is a backend failure (e.g. the database is
		// unreachable). Return 500 so it surfaces in metrics and alerting
		// instead of masquerading as a missing task, and keep the internal
		// detail out of the client response.
		slog.Error("failed to retrieve task", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, models.TaskStatus{
			Id:    id,
			Error: "internal server error",
		})
	} else {
		c.Set(taskAppKey, task.MetricApp())
		c.JSON(http.StatusOK, models.TaskStatus{
			Id:           task.Id,
			Created:      task.Created,
			Updated:      task.Updated,
			App:          task.App,
			Author:       task.Author,
			Project:      task.Project,
			Images:       task.Images,
			Status:       task.Status,
			StatusReason: task.StatusReason,
		})
	}
}

// healthz godoc
// @Summary Check if the server is healthy
// @Description Check if the argo-watcher is ready to process new tasks
// @Tags service
// @Produce json
// @Success 200 {object} models.HealthStatus
// @Failure 503 {object} models.HealthStatus
// @Router /healthz [get]
func (env *Env) healthz(c *gin.Context) {
	if env.argo.SimpleHealthCheck() {
		c.JSON(http.StatusOK, models.HealthStatus{
			Status: "up",
		})
	} else {
		c.JSON(http.StatusServiceUnavailable, models.HealthStatus{
			Status: "down",
		})
	}

}

// getConfig godoc
// @Summary Get the configuration of the server (excluding sensitive data)
// @Description Get the configuration of the server (excluding sensitive data)
// @Tags backend
// @Produce json
// @Success 200 {object} config.ServerConfig
// @Router /api/v1/config [get]
func (env *Env) getConfig(c *gin.Context) {
	c.JSON(http.StatusOK, env.config)
}

// requireOIDCAuth validates the OIDC token when OIDC auth is enabled. It returns
// true if validation passes (or OIDC is disabled). The canonical Oidc-Authorization
// header is tried first, then the deprecated Keycloak-Authorization alias. On
// failure the response distinguishes:
//   - 401 with "authentication required" when no auth header was sent.
//   - 401 with the strategy's reason when the token was rejected.
//   - 503 when the provider could not be consulted at all, so that a provider
//     outage does not make the Web UI treat the session as dead (see
//     requireAuthenticatedRead). Details land in the server log only.
func (env *Env) requireOIDCAuth(c *gin.Context) bool {
	if !env.config.OIDC.Enabled {
		return true
	}

	for _, header := range []string{oidcHeader, legacyKeycloakHeader} {
		valid, err := env.validateToken(c, header)
		if valid {
			return true
		}
		if errors.Is(err, auth.ErrProviderUnavailable) {
			slog.Error("rejecting request: authentication provider unavailable",
				"method", c.Request.Method, "url", c.Request.URL, "error", err)
			c.JSON(http.StatusServiceUnavailable, models.TaskStatus{
				Status: "authentication provider unavailable",
				Error:  err.Error(),
			})
			return false
		}
		if err != nil {
			// A header was present and the strategy rejected the token. Surface the reason.
			slog.Warn("rejected request with invalid token",
				"method", c.Request.Method, "url", c.Request.URL, "error", err)
			c.JSON(http.StatusUnauthorized, models.TaskStatus{
				Status: unauthorizedMessage,
				Error:  err.Error(),
			})
			return false
		}
	}

	// No auth header sent on either the canonical or the legacy name.
	slog.Warn("rejected unauthenticated request", "method", c.Request.Method, "url", c.Request.URL)
	c.JSON(http.StatusUnauthorized, models.TaskStatus{
		Status: unauthorizedMessage,
		Error:  "authentication required (set " + oidcHeader + " header)",
	})
	return false
}

// requireAuthenticatedRead returns middleware that rejects reads carrying no valid
// credential once OIDC auth is enabled; with OIDC disabled it is a no-op.
//
// Any configured credential is accepted — an OIDC session, the deploy token or a CI
// JWT — and reads are deliberately not restricted to OIDC_PRIVILEGED_GROUPS, which
// gates the deploy-lock writes alone.
//
// A rejected or missing credential is 401; a provider that could not be consulted is
// 503, because the Web UI discards its session on a 401.
func (env *Env) requireAuthenticatedRead() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !env.config.OIDC.Enabled {
			c.Next()
			return
		}

		valid, err := env.authenticator.AuthenticateRequest(c.Request)
		if valid {
			c.Next()
			return
		}

		if errors.Is(err, auth.ErrProviderUnavailable) {
			slog.Error("rejecting read: authentication provider unavailable",
				"method", c.Request.Method, "url", c.Request.URL, "error", err)
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, models.TaskStatus{
				Status: "authentication provider unavailable",
				Error:  err.Error(),
			})
			return
		}

		if err != nil {
			slog.Warn("rejecting read with invalid credential",
				"method", c.Request.Method, "url", c.Request.URL, "error", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, models.TaskStatus{
				Status: unauthorizedMessage,
				Error:  err.Error(),
			})
			return
		}

		slog.Warn("rejecting unauthenticated read", "method", c.Request.Method, "url", c.Request.URL)
		c.AbortWithStatusJSON(http.StatusUnauthorized, models.TaskStatus{
			Status: unauthorizedMessage,
			Error:  "authentication required (set " + oidcHeader + " header)",
		})
	}
}

// countUnauthenticatedRead returns middleware that counts reads arriving with no
// credential while OIDC auth is enabled. It never blocks: the route it guards is open
// on purpose, and the count is the migration signal for closing it later.
//
// It tests for presence rather than validity so the guarded endpoint — polled for the
// whole length of every deployment — never waits on the provider, and so an expired
// token cannot inflate the number whose fall to zero licenses that closure.
//
// The count is recorded after the handler, which is what names the application behind
// the read: a fleet-wide total says a migration is unfinished, the app label says
// whose pipeline to go and upgrade.
func (env *Env) countUnauthenticatedRead() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !env.config.OIDC.Enabled || env.hasCredential(c.Request) {
			c.Next()
			return
		}

		c.Next()

		app := c.GetString(taskAppKey)
		if app == "" {
			app = models.UnknownApp
		}
		env.metrics.AddUnauthenticatedRead(c.FullPath(), app)
	}
}

// hasCredential reports whether the request carries a non-empty value in any header a
// configured auth strategy reads, saying nothing about whether it is valid.
func (env *Env) hasCredential(request *http.Request) bool {
	for header := range env.strategies {
		if request.Header.Get(header) != "" {
			return true
		}
	}

	return false
}

// validateToken validates the incoming request using the configured authentication strategies.
// When allowedAuthStrategy is empty, the validation delegates to the default authenticator,
// which returns the last validation error when no strategies succeed. When allowedAuthStrategy
// is provided, validation is restricted to that specific strategy header via the authenticator.
func (env *Env) validateToken(c *gin.Context, allowedAuthStrategy string) (bool, error) {
	if allowedAuthStrategy == "" {
		return env.authenticator.Validate(c.Request)
	}

	return env.authenticator.ValidateStrategy(c.Request, allowedAuthStrategy)
}
