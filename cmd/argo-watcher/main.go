package main

import (
	"fmt"
	"github.com/gin-gonic/contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/romana/rlog"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/docs"
	h "github.com/shini4i/argo-watcher/internal/helpers"
	m "github.com/shini4i/argo-watcher/internal/models"
)

var version = "local"

var (
	client = Argo{
		User:     os.Getenv("ARGO_USER"),
		Password: os.Getenv("ARGO_PASSWORD"),
		Url:      os.Getenv("ARGO_URL"),
		Timeout:  h.GetEnv("ARGO_API_TIMEOUT", "60"),
	}
)

var (
	failedDeployment = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "failed_deployment",
		Help: "Per application failed deployment count before first success.",
	}, []string{"app"})
	processedDeployments = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "processed_deployments",
		Help: "The amount of deployment processed since startup.",
	})
	argocdUnavailable = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "argocd_unavailable",
		Help: "Whether ArgoCD is available for argo-watcher.",
	})
)

func setupRouter() *gin.Engine {
	staticFilesPath := h.GetEnv("STATIC_FILES_PATH", "static")

	docs.SwaggerInfo.Title = "Argo-Watcher API"
	docs.SwaggerInfo.Version = version
	docs.SwaggerInfo.Description = "A small tool that will help to improve deployment visibility"

	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Recovery())

	router.Use(static.Serve("/", static.LocalFile(staticFilesPath, true)))
	router.NoRoute(func(c *gin.Context) {
		c.File(fmt.Sprintf("%s/index.html", staticFilesPath))
	})
	router.GET("/healthz", healthz)
	router.GET("/metrics", prometheusHandler())
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	apiGroup := router.Group("/api/v1")
	apiGroup.POST("/tasks", addTask)
	apiGroup.GET("/tasks", getState)
	apiGroup.GET("/tasks/:id", getTaskStatus)
	apiGroup.GET("/apps", getApps)
	apiGroup.GET("/version", getVersion)

	return router
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
func addTask(c *gin.Context) {
	var task m.Task

	err := c.ShouldBindJSON(&task)
	if err != nil {
		rlog.Errorf("Couldn't process new task. Got the following error: %s", err)
		c.JSON(http.StatusNotAcceptable, m.TaskStatus{
			Status: "invalid payload",
		})
		return
	}

	id, err := client.AddTask(task)
	if err != nil {
		rlog.Errorf("Couldn't process new task. Got the following error: %s", err)
		c.JSON(http.StatusServiceUnavailable, m.TaskStatus{
			Status: id,
			Error:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusAccepted, m.TaskStatus{
		Id:     id,
		Status: "accepted",
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
func getState(c *gin.Context) {
	startTime, _ := strconv.ParseFloat(c.Query("from_timestamp"), 64)
	endTime, _ := strconv.ParseFloat(c.Query("to_timestamp"), 64)
	if endTime == 0 {
		endTime = float64(time.Now().Unix())
	}
	app := c.Query("app")

	c.JSON(http.StatusOK, client.GetTasks(startTime, endTime, app))
}

// getTaskStatus godoc
// @Summary Get the status of a task
// @Description Get the status of a task
// @Param id path string true "Task id" default(9185fae0-add5-11ec-87f3-56b185c552fa)
// @Tags backend
// @Produce json
// @Success 200 {object} m.TaskStatus
// @Router /api/v1/tasks/{id} [get]
func getTaskStatus(c *gin.Context) {
	id := c.Param("id")
	c.JSON(http.StatusOK, m.TaskStatus{
		Status: client.GetTaskStatus(id),
	})
}

// getApps godoc
// @Summary Get the list of apps
// @Description Get the list of apps
// @Tags frontend
// @Success 200 {array} string
// @Router /api/v1/apps [get]
func getApps(c *gin.Context) {
	c.JSON(http.StatusOK, client.GetAppList())
}

// getVersion godoc
// @Summary Get the version of the server
// @Description Get the version of the server
// @Tags frontend
// @Success 200 {string} string
// @Router /api/v1/version [get]
func getVersion(c *gin.Context) {
	c.JSON(http.StatusOK, version)
}

// healthz godoc
// @Summary Check if the server is healthy
// @Description Check if the argo-watcher is ready to process new tasks
// @Tags service
// @Produce json
// @Success 200 {object} m.HealthStatus
// @Failure 503 {object} m.HealthStatus
// @Router /healthz [get]
func healthz(c *gin.Context) {
	if client.SimpleHealthCheck() {
		c.JSON(http.StatusOK, m.HealthStatus{
			Status: "up",
		})
		return
	}
	c.JSON(http.StatusServiceUnavailable, m.HealthStatus{
		Status: "down",
	})
}

func prometheusHandler() gin.HandlerFunc {
	ph := promhttp.Handler()

	return func(c *gin.Context) {
		ph.ServeHTTP(c.Writer, c.Request)
	}
}

func prometheusRegisterMetrics() {
	rlog.Debug("Registering prometheus metrics...")
	prometheus.MustRegister(failedDeployment)
	prometheus.MustRegister(processedDeployments)
	prometheus.MustRegister(argocdUnavailable)
}

func main() {
	rlog.Info("Starting web server")

	router := setupRouter()

	rlog.Debug("Initializing argo-watcher client...")
	go client.Init()

	prometheusRegisterMetrics()

	routerHost := h.GetEnv("HOST", "0.0.0.0")
	routerPort := h.GetEnv("PORT", "8080")

	rlog.Debugf("Running on %s:%s", routerHost, routerPort)
	err := router.Run(routerHost + ":" + routerPort)
	if err != nil {
		rlog.Critical(err)
	}
}
