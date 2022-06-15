package main

import (
	"github.com/gin-gonic/gin"
	"github.com/romana/rlog"
	"github.com/shini4i/argo-watcher/internal/helpers"
	m "github.com/shini4i/argo-watcher/internal/models"
	"net/http"
)

type Token struct {
	Token string `json:"token"`
}

var (
	requestsCount int
)

func setupRouter() *gin.Engine {
	router := gin.Default()

	apiGroup := router.Group("/api/v1")
	apiGroup.POST("/session", mockGenSession)
	apiGroup.GET("/applications/:id", mockReturnAppStatus)

	return router
}

func mockGenSession(c *gin.Context) {
	c.JSON(http.StatusOK, Token{
		Token: "test_token",
	})
}

func mockReturnAppStatus(c *gin.Context) {
	var appStatus m.Application

	apps := []string{"app", "app2", "app4"}

	app := c.Param("id")

	if !helpers.Contains(apps, app) {
		c.JSON(http.StatusNotFound, nil)
		return
	}

	if app == "app" {
		appStatus.Status.Sync.Status = "Synced"
	} else {
		appStatus.Status.Sync.Status = "OutOfSync"
	}

	if app == "app3" {
		appStatus.Status.Health.Status = "UhHealthy"
	} else {
		appStatus.Status.Health.Status = "Healthy"
	}

	appStatus.Status.Summary.Images = []string{"app:v0.0.1", "nginx:1.21.6", "migrations:v0.0.1"}

	c.JSON(http.StatusOK, appStatus)
}

func main() {
	rlog.Info("Starting mock web server")

	router := setupRouter()

	err := router.Run(":8081")
	if err != nil {
		rlog.Critical(err)
	}
}
