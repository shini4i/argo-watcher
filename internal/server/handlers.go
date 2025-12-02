package server

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/internal/models"
)

// addTask godoc
// @Summary Add a new task
// @Description Add a new task
// @Tags backend
// @Accept json
// @Produce json
// @Param task body models.Task true "Task"
// @Success 202 {object} models.TaskStatus
// @Failure 406 {object} models.TaskStatus
// @Router /api/v1/tasks [post]
func (env *Env) addTask(c *gin.Context) {
	var task models.Task

	err := c.ShouldBindJSON(&task)
	if err != nil {
		log.Error().Msgf("couldn't process new task, got the following error: %s", err)
		c.JSON(http.StatusNotAcceptable, models.TaskStatus{
			Status: "invalid payload",
			Error:  err.Error(),
		})
		return
	}

	// we need to handle cases when deploy lock is set either manually or by cron
	if env.lockdown.IsLocked() {
		log.Warn().Msgf("deploy lock is set, rejecting the task")
		c.JSON(http.StatusNotAcceptable, models.TaskStatus{
			Status: "rejected",
			Error:  "lockdown is active, deployments are not accepted",
		})
		return
	}

	tokenValid, err := env.validateToken(c, "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.TaskStatus{})
		log.Error().Msgf("Couldn't validate token. Got the following error: %s", err)
		return
	}

	task.Validated = tokenValid

	newTask, err := env.argo.AddTask(task)
	if err != nil {
		log.Error().Msgf("Couldn't process new task. Got the following error: %s", err)
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

// stateHandler godoc
// @Summary Get state content
// @Description Get all tasks that match the provided parameters
// @Tags backend, frontend
// @Param app query string false "App name"
// @Param from_timestamp query number false "From timestamp (seconds since epoch, fractional seconds supported)"
// @Param to_timestamp query number false "To timestamp (seconds since epoch, fractional seconds supported)"
// @Param limit query int false "Maximum number of tasks to return"
// @Param offset query int false "Number of tasks to skip before returning results"
// @Success 200 {object} models.TasksResponse
// @Router /api/v1/tasks [get]
func (env *Env) stateHandler(c *gin.Context) {
	startTime, err := parseTimestampOrDefault(c.Query("from_timestamp"), 0)
	if err != nil {
		log.Warn().Msgf("invalid from_timestamp provided, using default: %v", err)
		startTime = 0
	}

	endTimeParam := c.Query("to_timestamp")
	endTime := float64(time.Now().Unix())
	if endTimeParam != "" {
		endTime, err = strconv.ParseFloat(endTimeParam, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, models.TaskStatus{
				Status: fmt.Sprintf("invalid to_timestamp: %v", err),
			})
			return
		}
	}

	app := c.Query("app")

	limitParam := c.Query("limit")
	limit := 0
	if limitParam != "" {
		limit, err = strconv.Atoi(limitParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, models.TaskStatus{
				Status: fmt.Sprintf("invalid limit: %v", err),
			})
			return
		}
	}

	offsetParam := c.Query("offset")
	offset := 0
	if offsetParam != "" {
		offset, err = strconv.Atoi(offsetParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, models.TaskStatus{
				Status: fmt.Sprintf("invalid offset: %v", err),
			})
			return
		}
	}

	if limit < 0 {
		limit = 0
	}
	if offset < 0 {
		offset = 0
	}

	c.JSON(http.StatusOK, env.argo.GetTasks(startTime, endTime, app, limit, offset))
}

// taskStatusHandler godoc
// @Summary Get the status of a task
// @Description Get the status of a task
// @Param id path string true "Task id" default(9185fae0-add5-11ec-87f3-56b185c552fa)
// @Tags backend
// @Produce json
// @Success 200 {object} models.TaskStatus
// @Router /api/v1/tasks/{id} [get]
func (env *Env) taskStatusHandler(c *gin.Context) {
	id := c.Param("id")
	task, err := env.argo.State.GetTask(id)

	if err != nil {
		c.JSON(http.StatusOK, models.TaskStatus{
			Id:    id,
			Error: err.Error(),
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

// SetDeployLock godoc
// @Summary Set deploy lock
// @Description Set deploy lock
// @Tags frontend
// @Success 200 {string} string
// @Router /api/v1/deploy-lock [post]
func (env *Env) SetDeployLock(c *gin.Context) {
	if env.config.Keycloak.Enabled {
		valid, err := env.validateToken(c, keycloakHeader)
		if err != nil {
			log.Error().Msgf("Error during validation: %s", err)
			c.JSON(http.StatusInternalServerError, models.TaskStatus{
				Status: "Validation process failed",
			})
			return
		}
		if !valid {
			log.Warn().Msg("User tried to set the lock with either invalid token or auth method")
			c.JSON(http.StatusUnauthorized, models.TaskStatus{
				Status: unauthorizedMessage,
			})
			return
		}
	}

	env.lockdown.SetLock()

	log.Debug().Msg("deploy lock is set")

	notifyWebSocketClients("locked")

	c.JSON(http.StatusOK, "deploy lock is set")
}

// ReleaseDeployLock godoc
// @Summary Release deploy lock
// @Description Release deploy lock
// @Tags frontend
// @Success 200 {string} string
// @Router /api/v1/deploy-lock [delete]
func (env *Env) ReleaseDeployLock(c *gin.Context) {
	if env.config.Keycloak.Enabled {
		valid, err := env.validateToken(c, keycloakHeader)
		if err != nil {
			log.Error().Msgf("Error during validation: %s", err)
			c.JSON(http.StatusInternalServerError, models.TaskStatus{
				Status: "Validation process failed",
			})
			return
		}
		if !valid {
			log.Warn().Msg("User tried to release the lock with either invalid token or auth method")
			c.JSON(http.StatusUnauthorized, models.TaskStatus{
				Status: unauthorizedMessage,
			})
			return
		}
	}

	env.lockdown.ReleaseLock()

	log.Debug().Msg("deploy lock is released")

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

// versionHandler godoc
// @Summary Get the version of the server
// @Description Get the version of the server
// @Tags frontend
// @Success 200 {string} string
// @Router /api/v1/version [get]
func (env *Env) versionHandler(c *gin.Context) {
	c.JSON(http.StatusOK, version)
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

// configHandler godoc
// @Summary Get the configuration of the server (excluding sensitive data)
// @Description Get the configuration of the server (excluding sensitive data)
// @Tags backend
// @Produce json
// @Success 200 {object} config.ServerConfig
// @Router /api/v1/config [get]
func (env *Env) configHandler(c *gin.Context) {
	c.JSON(http.StatusOK, env.config)
}
