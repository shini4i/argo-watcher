package main

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/docs"
	"github.com/shini4i/argo-watcher/internal/models"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

var version = "local"

// reference: https://www.alexedwards.net/blog/organising-database-access
type Env struct {
	// environment configurations
	config *config.ServerConfig
	// argo argo
	argo *Argo
	// argo updater
	updater *ArgoStatusUpdater
	// metrics
	metrics *Metrics
}

// initialize router
func (env *Env) CreateRouter() *gin.Engine {
	docs.SwaggerInfo.Title = "Argo-Watcher API"
	docs.SwaggerInfo.Version = version
	docs.SwaggerInfo.Description = "A small tool that will help to improve deployment visibility"

	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Recovery())

	staticFilesPath := env.config.StaticFilePath
	router.Use(static.Serve("/", static.LocalFile(staticFilesPath, true)))
	router.NoRoute(func(c *gin.Context) {
		c.File(fmt.Sprintf("%s/index.html", staticFilesPath))
	})
	router.GET("/healthz", env.healthz)
	router.GET("/metrics", prometheusHandler())
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	v1 := router.Group("/api/v1")
	{
		v1.POST("/tasks", env.addTask)
		v1.GET("/tasks", env.getState)
		v1.GET("/tasks/:id", env.getTaskStatus)
		v1.GET("/apps", env.getApps)
		v1.GET("/version", env.getVersion)
	}

	return router
}

func (env *Env) StartRouter(router *gin.Engine) {
	routerBind := fmt.Sprintf("%s:%s", env.config.Host, env.config.Port)
	log.Debug().Msgf("Listening on %s", routerBind)
	if err := router.Run(routerBind); err != nil {
		panic(err)
	}
}

func prometheusHandler() gin.HandlerFunc {
	ph := promhttp.Handler()

	return func(c *gin.Context) {
		ph.ServeHTTP(c.Writer, c.Request)
	}
}

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
// @Param task body m.Task true "Task"
// @Success 200 {object} m.TaskStatus
// @Router /api/v1/tasks [post]
func (env *Env) addTask(c *gin.Context) {
	var task models.Task

	err := c.ShouldBindJSON(&task)
	if err != nil {
		log.Error().Msgf("Couldn't process new task. Got the following error: %s", err)
		c.JSON(http.StatusNotAcceptable, models.TaskStatus{
			Status: "invalid payload",
			Error:  err.Error(),
		})
		return
	}

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
	c.JSON(http.StatusOK, models.TaskStatus{
		Id:      newTask.Id,
		App:     newTask.App,
		Author:  newTask.Author,
		Project: newTask.Project,
		Images:  newTask.Images,
		Status:  "accepted",
	})
}

// getState godoc
// @Summary Get state content
// @Description Get all tasks that match the provided parameters
// @Tags backend, frontend
// @Param app query string false "App name"
// @Param from_timestamp query int true "From timestamp" default(1648390029)
// @Param to_timestamp query int false "To timestamp"
// @Success 200 {array} m.Task
// @Router /api/v1/tasks [get]
func (env *Env) getState(c *gin.Context) {
	startTime, _ := strconv.ParseFloat(c.Query("from_timestamp"), 64)
	endTime, _ := strconv.ParseFloat(c.Query("to_timestamp"), 64)
	if endTime == 0 {
		endTime = float64(time.Now().Unix())
	}
	app := c.Query("app")

	c.JSON(http.StatusOK, env.argo.GetTasks(startTime, endTime, app))
}

// getTaskStatus godoc
// @Summary Get the status of a task
// @Description Get the status of a task
// @Param id path string true "Task id" default(9185fae0-add5-11ec-87f3-56b185c552fa)
// @Tags backend
// @Produce json
// @Success 200 {object} m.TaskStatus
// @Router /api/v1/tasks/{id} [get]
func (env *Env) getTaskStatus(c *gin.Context) {
	id := c.Param("id")
	task, err := env.argo.state.GetTask(id)

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

// getApps godoc
// @Summary Get the list of apps
// @Description Get the list of apps
// @Tags frontend
// @Success 200 {array} string
// @Router /api/v1/apps [get]
func (env *Env) getApps(c *gin.Context) {
	c.JSON(http.StatusOK, env.argo.GetAppList())
}

// healthz godoc
// @Summary Check if the server is healthy
// @Description Check if the argo-watcher is ready to process new tasks
// @Tags service
// @Produce json
// @Success 200 {object} m.HealthStatus
// @Failure 503 {object} m.HealthStatus
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
