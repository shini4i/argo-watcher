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
	requestsCount = 0
)

func setupRouter() *gin.Engine {
	router := gin.Default()

	apiGroup := router.Group("/api/v1")
	apiGroup.POST("/session", mockGenSession)
	apiGroup.GET("/session/userinfo", mockUserinfo)
	apiGroup.GET("/applications/:id", mockReturnAppStatus)

	return router
}

func mockGenSession(c *gin.Context) {
	c.JSON(http.StatusOK, Token{
		Token: "test_token",
	})
}

func mockUserinfo(c *gin.Context) {
	c.JSON(http.StatusOK, m.Userinfo{LoggedIn: true})
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

	appStatus.Status.Summary.Images = []string{"app:v0.0.1", "nginx:1.21.6", "migrations:v0.0.1"}

	if app == "app4" && requestsCount < 5 {
		rlog.Infof("app4 requests count %d", requestsCount)
		requestsCount++
		if requestsCount < 2 {
			appStatus.Status.Summary.Images = []string{"app:v0.0.1-rc1", "nginx:1.21.6", "migrations:v0.0.1"}
		}
		appStatus.Status.Health.Status = "UhHealthy"
		rlog.Infof("app4 sync status is %s", appStatus.Status.Sync.Status)
		rlog.Infof("app4 health status is %s", appStatus.Status.Health.Status)
	} else if app == "app4" {
		requestsCount = 0
		appStatus.Status.Health.Status = "Healthy"
		appStatus.Status.Sync.Status = "Synced"
		rlog.Infof("app4 sync status is %s", appStatus.Status.Sync.Status)
		rlog.Infof("app4 health status is %s", appStatus.Status.Health.Status)
	} else {
		appStatus.Status.Health.Status = "Healthy"
	}

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
