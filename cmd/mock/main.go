package main

import (
	"github.com/gin-gonic/gin"
	"github.com/romana/rlog"
	m "github.com/shini4i/argo-watcher/internal/models"
	"net/http"
)

type Token struct {
	Token string `json:"token"`
}

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

	appStatus.Status.Sync.Status = "Synced"
	appStatus.Status.Health.Status = "Healthy"
	appStatus.Status.Summary.Images = []string{"test:v0.0.1", "test2:v0.0.3", "test:v0.0.3"}

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
