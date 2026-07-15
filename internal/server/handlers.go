package server

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

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
	keycloakHeader      = "Keycloak-Authorization"
)

// getVersion godoc
// @Summary Get the version of the server
// @Description Get the version of the server
// @Tags frontend
// @Success 200 {string} string
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
		slog.Error(fmt.Sprintf("couldn't process new task, got the following error: %s", err))
		c.JSON(http.StatusNotAcceptable, models.TaskStatus{
			Status: "invalid payload",
			Error:  err.Error(),
		})
		return
	}

	// we need to handle cases when deploy lock is set either manually or by cron
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
		slog.Warn(fmt.Sprintf("rejecting task: %s", err))
		c.JSON(http.StatusUnauthorized, models.TaskStatus{
			Status: unauthorizedMessage,
			Error:  err.Error(),
		})
		return
	}

	task.Validated = tokenValid

	newTask, err := env.argo.AddTask(task)
	if err != nil {
		slog.Error(fmt.Sprintf("Couldn't process new task. Got the following error: %s", err))
		c.JSON(http.StatusServiceUnavailable, models.TaskStatus{
			Status: "down",
			Error:  err.Error(),
		})
		return
	}

	// start rollout monitor
	go env.updater.WaitForRollout(*newTask)

	// return information about created task
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
		slog.Error(fmt.Sprintf("failed to retrieve task %s: %s", id, err))
		c.JSON(http.StatusInternalServerError, models.TaskStatus{
			Id:    id,
			Error: "internal server error",
		})
	} else {
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

// requireKeycloakAuth validates the Keycloak token when Keycloak is enabled.
// It returns true if validation passes (or Keycloak is disabled). On failure
// the response distinguishes:
//   - 401 with "authentication required" when no auth header was sent.
//   - 401 with the strategy's reason when the token was rejected.
//
// Strategy transport/parse failures (e.g. Keycloak unreachable) are also
// returned as 401 with a sanitized "token validation failed" message; full
// details land in the server log only.
func (env *Env) requireKeycloakAuth(c *gin.Context) bool {
	if !env.config.Keycloak.Enabled {
		return true
	}

	valid, err := env.validateToken(c, keycloakHeader)
	if valid {
		return true
	}

	if err != nil {
		// Strategy was invoked and rejected the token. Surface the reason.
		slog.Warn(fmt.Sprintf("User tried %s %s with invalid token: %s",
			c.Request.Method, c.Request.URL, err))
		c.JSON(http.StatusUnauthorized, models.TaskStatus{
			Status: unauthorizedMessage,
			Error:  err.Error(),
		})
		return false
	}

	// (false, nil): no auth header sent at all.
	slog.Warn(fmt.Sprintf("User tried %s %s without authentication", c.Request.Method, c.Request.URL))
	c.JSON(http.StatusUnauthorized, models.TaskStatus{
		Status: unauthorizedMessage,
		Error:  "authentication required (set " + keycloakHeader + " header)",
	})
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
