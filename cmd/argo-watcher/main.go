package main

import (
	"fmt"
	"github.com/gin-gonic/contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/romana/rlog"
	"net/http"
	"os"
	"strconv"
	"time"

	h "github.com/shini4i/argo-watcher/internal/helpers"
	m "github.com/shini4i/argo-watcher/internal/models"
)

const version = "0.0.9"

var (
	argo = Argo{
		User:     os.Getenv("ARGO_USER"),
		Password: os.Getenv("ARGO_PASSWORD"),
		Url:      os.Getenv("ARGO_URL"),
	}
	client = argo.Init()
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
)

func setupRouter() *gin.Engine {
	staticFilesPath := h.GetEnv("STATIC_FILES_PATH", "static")

	prometheus.MustRegister(failedDeployment)
	prometheus.MustRegister(processedDeployments)

	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Recovery())

	router.Use(static.Serve("/", static.LocalFile(staticFilesPath, true)))
	router.NoRoute(func(c *gin.Context) {
		c.File(fmt.Sprintf("%s/index.html", staticFilesPath))
	})
	router.GET("/healthz", healthz)
	router.GET("/metrics", prometheusHandler())

	apiGroup := router.Group("/api/v1")
	apiGroup.POST("/tasks", addTask)
	apiGroup.GET("/tasks", getState)
	apiGroup.GET("/tasks/:id", getTaskStatus)
	apiGroup.GET("/apps", getApps)
	apiGroup.GET("/version", getVersion)

	return router
}

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

	id := client.AddTask(task)

	c.JSON(http.StatusAccepted, m.TaskStatus{
		Id:     id,
		Status: "accepted",
	})
}

func getState(c *gin.Context) {
	startTime, _ := strconv.ParseFloat(c.Query("from_timestamp"), 64)
	endTime, _ := strconv.ParseFloat(c.Query("to_timestamp"), 64)
	if endTime == 0 {
		endTime = float64(time.Now().Unix())
	}
	app := c.Query("app")

	c.JSON(http.StatusOK, client.GetTasks(startTime, endTime, app))
}

func getTaskStatus(c *gin.Context) {
	id := c.Param("id")
	c.JSON(http.StatusOK, m.TaskStatus{
		Status: client.GetTaskStatus(id),
	})
}

func getApps(c *gin.Context) {
	c.JSON(http.StatusOK, client.GetAppList())
}

func getVersion(c *gin.Context) {
	c.JSON(http.StatusOK, version)
}

func healthz(c *gin.Context) {
	if status := client.Check(); status == "up" {
		c.JSON(http.StatusOK, m.HealthStatus{
			Status: "up",
		})
	} else {
		c.JSON(http.StatusServiceUnavailable, m.HealthStatus{
			Status: "down",
		})
	}
}

func prometheusHandler() gin.HandlerFunc {
	ph := promhttp.Handler()

	return func(c *gin.Context) {
		ph.ServeHTTP(c.Writer, c.Request)
	}
}

func main() {
	rlog.Info("Starting web server")
	router := setupRouter()
	err := router.Run(":8080")
	if err != nil {
		rlog.Critical(err)
	}
}
