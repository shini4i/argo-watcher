package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/argocd"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/auth"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/docs"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/prometheus"
	"github.com/shini4i/argo-watcher/internal/models"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

var version = "local"

var (
	connectionsMutex sync.Mutex
	connections      []*websocket.Conn
)

const (
	deployLockEndpoint  = "/deploy-lock"
	unauthorizedMessage = "You are not authorized to perform this action"
	keycloakHeader      = "Keycloak-Authorization"
)

// Env reference: https://www.alexedwards.net/blog/organising-database-access
type Env struct {
	// environment configurations
	config *config.ServerConfig
	// argo argo
	argo *argocd.Argo
	// argo updater
	updater *argocd.ArgoStatusUpdater
	// metrics
	metrics *prometheus.Metrics
	// deploy lock
	lockdown *Lockdown
	// enabled auth strategies
	strategies map[string]auth.AuthService
}

// CreateRouter initialize router.
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
	router.GET("/metrics", prometheusHandler(env.metrics))
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	router.GET("/ws", env.handleWebSocketConnection)

	v1 := router.Group("/api/v1")
	{
		v1.POST("/tasks", env.addTask)
		v1.GET("/tasks", env.getState)
		v1.GET("/tasks/:id", env.getTaskStatus)
		v1.GET("/version", env.getVersion)
		v1.GET("/config", env.getConfig)
		v1.POST(deployLockEndpoint, env.SetDeployLock)
		v1.DELETE(deployLockEndpoint, env.ReleaseDeployLock)
		v1.GET(deployLockEndpoint, env.isDeployLockSet)
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

func prometheusHandler(metrics *prometheus.Metrics) gin.HandlerFunc {
	ph := promhttp.HandlerFor(metrics.GetRegistry(), promhttp.HandlerOpts{})

	return func(c *gin.Context) {
		ph.ServeHTTP(c.Writer, c.Request)
	}
}

// getVersion godoc
// @Summary Get the version of the server
// @Description Get the version of the server
// @Tags frontend
// @Success 200 {string} string
// @Router /api/v1/version [get].
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
// @Failure 406 {object} models.TaskStatus
// @Router /api/v1/tasks [post].
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

// getState godoc
// @Summary Get state content
// @Description Get all tasks that match the provided parameters
// @Tags backend, frontend
// @Param app query string false "App name"
// @Param from_timestamp query int true "From timestamp" default(1648390029)
// @Param to_timestamp query int false "To timestamp"
// @Success 200 {array} models.Task
// @Router /api/v1/tasks [get].
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
// @Success 200 {object} models.TaskStatus
// @Router /api/v1/tasks/{id} [get].
func (env *Env) getTaskStatus(c *gin.Context) {
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

// healthz godoc
// @Summary Check if the server is healthy
// @Description Check if the argo-watcher is ready to process new tasks
// @Tags service
// @Produce json
// @Success 200 {object} models.HealthStatus
// @Failure 503 {object} models.HealthStatus
// @Router /healthz [get].
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
// @Router /api/v1/config [get].
func (env *Env) getConfig(c *gin.Context) {
	c.JSON(http.StatusOK, env.config)
}

// SetDeployLock godoc
// @Summary Set deploy lock
// @Description Set deploy lock
// @Tags frontend
// @Success 200 {string} string
// @Router /api/v1/deploy-lock [post].
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
// @Router /api/v1/deploy-lock [delete].
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
// @Router /api/v1/deploy-lock [get].
func (env *Env) isDeployLockSet(c *gin.Context) {
	c.JSON(http.StatusOK, env.lockdown.IsLocked())
}

func (env *Env) validateToken(c *gin.Context, allowedAuthStrategy string) (bool, error) {
	for header, strategy := range env.strategies {
		token := c.GetHeader(header)
		if token == "" {
			continue
		}

		// this is a restriction to be able to specify allowed auth service on per endpoint basis
		if allowedAuthStrategy != "" && allowedAuthStrategy != header {
			log.Debug().Msgf("Authorization strategy %s is not allowed for [%s] %s endpoint",
				header,
				c.Request.Method,
				c.Request.URL,
			)
			continue
		}

		log.Debug().Msgf("Using %s strategy for [%s] %s", header, c.Request.Method, c.Request.URL)

		// strip Bearer prefix for JWT validation, is there a more reasonable approach?
		if strings.HasPrefix(token, "Bearer ") {
			token = strings.TrimPrefix(token, "Bearer ")
		}

		valid, err := strategy.Validate(token)
		if valid {
			return true, nil
		}
		if err != nil {
			return false, err
		}
	}

	// No valid token found, but it's not an error in this context.
	return false, nil
}

// handleWebSocketConnection accepts a WebSocket connection, adds it to a slice,
// and initiates a goroutine to ping the connection regularly. If WebSocket
// acceptance fails, an error is logged. The goroutine serves to monitor
// the connection's activity and removes it from the slice if it's inactive.
func (env *Env) handleWebSocketConnection(c *gin.Context) {
	options := &websocket.AcceptOptions{
		InsecureSkipVerify: env.config.DevEnvironment, // It will disable websocket host validation if set to true
	}

	conn, err := websocket.Accept(c.Writer, c.Request, options)
	if err != nil {
		log.Error().Msgf("couldn't accept websocket connection, got the following error: %s", err)
	}

	// Append the connection before starting the goroutine
	connections = append(connections, conn)

	go env.checkConnection(conn)
}

// checkConnection is a method for the Env struct that continuously checks the
// health of a WebSocket connection by sending periodic "heartbeat" messages.
func (env *Env) checkConnection(c *websocket.Conn) {
	ticker := time.NewTicker(time.Second * 30)
	defer ticker.Stop()

	for range ticker.C {
		// we are not using c.Ping here, because it's not working as expected
		// for some reason it's failing even if the connection is still alive
		// if you know how to fix it, please open an issue or PR
		if err := c.Write(context.Background(), websocket.MessageText, []byte("heartbeat")); err != nil {
			removeWebSocketConnection(c)
			return
		}
	}
}

// notifyWebSocketClients is a function that sends a message to all active WebSocket clients.
// It iterates over the global connections slice, which contains all active WebSocket connections,
// and sends the provided message to each connection using the wsjson.Write function.
// If an error occurs while sending the message to a connection (for example, if the connection has been closed),
// it removes the connection from the connections slice to prevent further attempts to send messages to it.
func notifyWebSocketClients(message string) {
	var wg sync.WaitGroup

	for _, conn := range connections {
		wg.Add(1)

		go func(c *websocket.Conn, message string) {
			defer wg.Done()
			if err := c.Write(context.Background(), websocket.MessageText, []byte(message)); err != nil {
				removeWebSocketConnection(c)
			}
		}(conn, message)
	}

	wg.Wait()
}

// removeWebSocketConnection is a helper function that removes a WebSocket connection
// from the global connections slice. It is used to clean up connections that are no longer active.
// The function takes a WebSocket connection as an argument and removes it from the connections slice.
// It uses a mutex to prevent concurrent access to the connections slice, ensuring thread safety.
func removeWebSocketConnection(conn *websocket.Conn) {
	connectionsMutex.Lock()
	defer connectionsMutex.Unlock()

	for i := range connections {
		if connections[i] == conn {
			connections = append(connections[:i], connections[i+1:]...)
			break
		}
	}
}

// NewEnv initializes a new Env instance.
// This function is used to set up the environment for the application's main operation, including setting configurations, initializing Argo service, and metrics.
func NewEnv(serverConfig *config.ServerConfig, argo *argocd.Argo, metrics *prometheus.Metrics, updater *argocd.ArgoStatusUpdater) (*Env, error) {
	var env *Env
	var err error

	env = &Env{
		config:  serverConfig,
		argo:    argo,
		metrics: metrics,
		updater: updater,
	}

	if env.lockdown, err = NewLockdown(serverConfig.LockdownSchedule); err != nil {
		return nil, err
	}

	env.strategies = map[string]auth.AuthService{
		"ARGO_WATCHER_DEPLOY_TOKEN": auth.NewDeployTokenAuthService(env.config.DeployToken),
	}

	if env.config.Keycloak.Enabled {
		env.strategies[keycloakHeader] = auth.NewKeycloakAuthService(env.config)
	}

	if env.config.JWTSecret != "" {
		env.strategies["Authorization"] = auth.NewJWTAuthService(env.config.JWTSecret)
	}

	return env, nil
}
