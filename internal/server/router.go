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
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/docs"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/prometheus"
	"github.com/shini4i/argo-watcher/internal/argocd"
	"github.com/shini4i/argo-watcher/internal/auth"
	"github.com/shini4i/argo-watcher/internal/export/history"
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
	historyExportBatch  = 1000
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
	strategies map[string]auth.AuthStrategy
	// authenticator orchestrates registered strategies
	authenticator *auth.Authenticator
}

// CreateRouter initialize router.
func (env *Env) CreateRouter() *gin.Engine {
	docs.SwaggerInfo.Title = "Argo-Watcher API"
	docs.SwaggerInfo.Version = version
	docs.SwaggerInfo.Description = "A small tool that will help to improve deployment visibility"

	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(cors.New(env.corsConfig()))

	staticFilesPath := env.config.ResolveStaticFilePath()
	log.Info().
		Str("static_path", staticFilesPath).
		Msg("serving frontend assets")
	router.Use(static.Serve("/", static.LocalFile(staticFilesPath, true)))
	router.NoRoute(func(c *gin.Context) {
		c.File(fmt.Sprintf("%s/index.html", staticFilesPath))
	})
	router.GET("/healthz", env.healthz)
	router.GET("/metrics", prometheusHandler())
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	router.GET("/ws", env.handleWebSocketConnection)

	v1 := router.Group("/api/v1")
	{
		v1.POST("/tasks", env.addTask)
		v1.GET("/tasks", env.getState)
		v1.GET("/tasks/:id", env.getTaskStatus)
		v1.GET("/tasks/export", env.exportTasks)
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

func (env *Env) corsConfig() cors.Config {
	config := cors.Config{
		AllowMethods:           []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowHeaders:           []string{"Origin", "Content-Type", "Accept", "Authorization", keycloakHeader, "ARGO_WATCHER_DEPLOY_TOKEN"},
		ExposeHeaders:          []string{"Content-Length"},
		AllowWebSockets:        true,
		AllowBrowserExtensions: true,
		MaxAge:                 12 * time.Hour,
	}

	if env.config.DevEnvironment {
		config.AllowOrigins = []string{
			"http://localhost:3000",
			"http://127.0.0.1:3000",
			"http://localhost:3100",
			"http://127.0.0.1:3100",
			"http://localhost:5173",
			"http://127.0.0.1:5173",
		}
		config.AllowCredentials = true
	} else {
		config.AllowAllOrigins = true
	}

	return config
}

// prometheusHandler returns the default promhttp handler.
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

// getState godoc
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
func (env *Env) getState(c *gin.Context) {
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

// exportTasks godoc
// @Summary Export historical tasks
// @Description Streams the filtered task history as CSV or JSON.
// @Tags backend, frontend
// @Produce text/csv
// @Produce application/json
// @Param format query string false "Export format (csv or json)" Enums(csv,json)
// @Param anonymize query bool false "Remove author and status_reason columns" default(true)
// @Param from_timestamp query number false "Start timestamp (seconds since epoch, fractional seconds supported)"
// @Param to_timestamp query number false "End timestamp (seconds since epoch, fractional seconds supported)"
// @Param app query string false "Filter by application name"
// @Success 200
// @Failure 400 {object} models.TaskStatus
// @Failure 401 {object} models.TaskStatus
// @Failure 503 {object} models.TaskStatus
// @Router /api/v1/tasks/export [get]
func (env *Env) exportTasks(c *gin.Context) {
	params, reqErr := env.parseExportParams(c)
	if reqErr != nil {
		c.JSON(reqErr.statusCode, models.TaskStatus{
			Status: reqErr.message,
		})
		return
	}

	if reqErr = env.ensureExportAuthorized(c); reqErr != nil {
		c.JSON(reqErr.statusCode, models.TaskStatus{
			Status: reqErr.message,
		})
		return
	}

	writer, contentType := buildExportWriter(params.format, params.anonymize, c.Writer)

	defer func() {
		if err := writer.Close(); err != nil {
			log.Error().Err(err).Msg("failed to flush export writer")
		}
	}()

	now := time.Now().UTC()
	filename := fmt.Sprintf("history-tasks-%s.%s", now.Format("2006-01-02-15-04-05"), params.format)
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Status(http.StatusOK)

	if err := env.streamExportRows(params.startTime, params.endTime, params.app, params.anonymize, writer); err != nil {
		log.Error().Err(err).Msg("failed to stream export rows")
		if !c.Writer.Written() {
			c.JSON(http.StatusServiceUnavailable, models.TaskStatus{
				Status: "failed to stream export rows",
			})
		}
	}
}

// getTaskStatus godoc
// @Summary Get the status of a task
// @Description Get the status of a task
// @Param id path string true "Task id" default(9185fae0-add5-11ec-87f3-56b185c552fa)
// @Tags backend
// @Produce json
// @Success 200 {object} models.TaskStatus
// @Router /api/v1/tasks/{id} [get]
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

// validateToken validates the incoming request using the configured authentication strategies.
// When allowedAuthStrategy is empty, the validation delegates to the default authenticator,
// which returns the last validation error when no strategies succeed. When allowedAuthStrategy
// is provided, validation is restricted to that specific strategy header and the last error from
// that strategy is returned if validation ultimately fails.
func (env *Env) validateToken(c *gin.Context, allowedAuthStrategy string) (bool, error) {
	if allowedAuthStrategy == "" {
		return env.authenticator.Validate(c.Request)
	}

	return env.validateAllowedStrategy(c, allowedAuthStrategy)
}

// validateAllowedStrategy enforces validation against the single allowed authentication strategy header.
// while keeping track of the last validation error produced by that strategy.
func (env *Env) validateAllowedStrategy(c *gin.Context, allowedStrategyHeader string) (bool, error) {
	var lastErr error

	for header, strategy := range env.strategies {
		token := c.GetHeader(header)
		if token == "" {
			continue
		}

		if header != allowedStrategyHeader {
			log.Debug().Msgf("Authorization strategy %s is not allowed for [%s] %s endpoint",
				header,
				c.Request.Method,
				c.Request.URL,
			)
			continue
		}

		log.Debug().Msgf("Using %s strategy for [%s] %s", header, c.Request.Method, c.Request.URL)

		if strings.HasPrefix(token, "Bearer ") {
			token = strings.TrimPrefix(token, "Bearer ")
		}

		valid, err := strategy.Validate(token)
		if err != nil {
			lastErr = err
		}
		if valid {
			return true, nil
		}
	}

	return false, lastErr
}

// parseExportParams extracts and validates query parameters for export requests.
func (env *Env) parseExportParams(c *gin.Context) (exportParams, *requestError) {
	params := exportParams{
		format: strings.ToLower(c.DefaultQuery("format", "csv")),
		app:    c.Query("app"),
	}

	if params.format != "csv" && params.format != "json" {
		return params, &requestError{
			statusCode: http.StatusBadRequest,
			message:    "unsupported export format",
		}
	}

	anonymize, err := parseBoolOrDefault(c.Query("anonymize"), true)
	if err != nil {
		return params, &requestError{
			statusCode: http.StatusBadRequest,
			message:    fmt.Sprintf("invalid anonymize flag: %v", err),
		}
	}

	now := time.Now().UTC()
	defaultFrom := now.Add(-24 * time.Hour).Unix()

	params.startTime, err = parseTimestampOrDefault(c.Query("from_timestamp"), float64(defaultFrom))
	if err != nil {
		return params, &requestError{
			statusCode: http.StatusBadRequest,
			message:    fmt.Sprintf("invalid from_timestamp: %v", err),
		}
	}

	params.endTime, err = parseTimestampOrDefault(c.Query("to_timestamp"), float64(now.Unix()))
	if err != nil {
		return params, &requestError{
			statusCode: http.StatusBadRequest,
			message:    fmt.Sprintf("invalid to_timestamp: %v", err),
		}
	}

	if params.endTime < params.startTime {
		return params, &requestError{
			statusCode: http.StatusBadRequest,
			message:    "to_timestamp must be greater than or equal to from_timestamp",
		}
	}

	params.anonymize = anonymize
	if !env.config.Keycloak.Enabled {
		// Without keycloak-provided privilege context, default to anonymized exports.
		params.anonymize = true
	}

	return params, nil
}

// ensureExportAuthorized validates authorization for export requests when authentication is configured.
func (env *Env) ensureExportAuthorized(c *gin.Context) *requestError {
	if !env.hasAuthConfigured() {
		return nil
	}

	if env.config.Keycloak.Enabled {
		valid, validationErr := env.validateToken(c, keycloakHeader)
		if validationErr != nil {
			log.Error().Err(validationErr).Msg("failed to validate export token")
			return &requestError{
				statusCode: http.StatusInternalServerError,
				message:    "validation process failed",
			}
		}
		if !valid {
			return &requestError{
				statusCode: http.StatusUnauthorized,
				message:    unauthorizedMessage,
			}
		}

		return nil
	}

	valid, validationErr := env.validateToken(c, "")
	if validationErr != nil {
		log.Error().Err(validationErr).Msg("failed to validate export token")
		return &requestError{
			statusCode: http.StatusInternalServerError,
			message:    "validation process failed",
		}
	}
	if !valid {
		return &requestError{
			statusCode: http.StatusUnauthorized,
			message:    unauthorizedMessage,
		}
	}

	return nil
}

// streamExportRows fetches tasks in batches and streams them via the provided writer.
func (env *Env) streamExportRows(start float64, end float64, app string, anonymize bool, writer history.RowWriter) error {
	if env.argo == nil || env.argo.State == nil {
		return fmt.Errorf("task repository is not initialised")
	}

	offset := 0
	for {
		tasks, total := env.argo.State.GetTasks(start, end, app, historyExportBatch, offset)
		if len(tasks) == 0 {
			return nil
		}

		for _, task := range tasks {
			if err := writer.WriteRow(history.SanitizeTask(task, anonymize)); err != nil {
				return err
			}
		}

		offset += len(tasks)

		if offset >= int(total) || len(tasks) < historyExportBatch {
			return nil
		}
	}
}

// buildExportWriter returns the export writer and related content type for a format and anonymization flag.
func buildExportWriter(format string, anonymize bool, writer http.ResponseWriter) (history.RowWriter, string) {
	switch format {
	case "json":
		return history.NewJSONWriter(writer), "application/json"
	default:
		return history.NewCSVWriter(writer, history.ColumnsFor(anonymize)), "text/csv"
	}
}

// requestError represents an HTTP error response that should be returned to the client.
type requestError struct {
	statusCode int
	message    string
}

// Error implements the error interface for requestError.
func (r requestError) Error() string {
	return r.message
}

// exportParams bundles request parameters required for history export.
type exportParams struct {
	format    string
	anonymize bool
	startTime float64
	endTime   float64
	app       string
}

// hasAuthConfigured reports whether any authentication mechanism is configured and should be enforced.
func (env *Env) hasAuthConfigured() bool {
	return env.config.Keycloak.Enabled ||
		env.config.JWTSecret != "" ||
		env.config.DeployToken != ""
}

func parseTimestampOrDefault(value string, fallback float64) (float64, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

func parseBoolOrDefault(value string, fallback bool) (bool, error) {
	if value == "" {
		return fallback, nil
	}
	return strconv.ParseBool(value)
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

	env.strategies = map[string]auth.AuthStrategy{
		"ARGO_WATCHER_DEPLOY_TOKEN": auth.NewDeployTokenAuthService(env.config.DeployToken),
	}

	if env.config.Keycloak.Enabled {
		env.strategies[keycloakHeader] = auth.NewKeycloakAuthService(env.config)
	}

	if env.config.JWTSecret != "" {
		env.strategies["Authorization"] = auth.NewJWTAuthService(env.config.JWTSecret)
	}

	env.authenticator = auth.NewAuthenticator(env.strategies)

	return env, nil
}
