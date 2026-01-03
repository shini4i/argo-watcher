package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/docs"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/prometheus"
	"github.com/shini4i/argo-watcher/internal/argocd"
	"github.com/shini4i/argo-watcher/internal/auth"
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

// safeFileSystem wraps http.Dir with additional symlink protection.
// This provides defense-in-depth beyond http.Dir's built-in path sanitization.
type safeFileSystem struct {
	root     http.Dir
	basePath string
}

// validatePath checks if a path is safe before any I/O operations.
// Returns the cleaned path if valid, or an error if the path would escape the base directory.
// This performs validation without any I/O operations by checking the cleaned path.
func (fs safeFileSystem) validatePath(name string) (string, error) {
	// Clean the path to remove any .. or . components
	cleanName := filepath.Clean("/" + name)

	// Construct the would-be full path and verify it stays within bounds
	// filepath.Join handles path separators and cleaning
	fullPath := filepath.Join(fs.basePath, cleanName)

	// Clean the full path to resolve any remaining . or .. components
	cleanedFull := filepath.Clean(fullPath)

	// Verify the cleaned path is still within the base directory
	// Check for exact match (root directory) or proper prefix with separator
	if cleanedFull != fs.basePath && !strings.HasPrefix(cleanedFull, fs.basePath+string(filepath.Separator)) {
		log.Debug().
			Str("requested_path", name).
			Str("resolved_path", cleanedFull).
			Str("base_path", fs.basePath).
			Msg("blocked path traversal attempt")
		return "", os.ErrPermission
	}

	return cleanName, nil
}

// Open implements http.FileSystem interface with path validation and symlink protection.
func (fs safeFileSystem) Open(name string) (http.File, error) {
	// Validate path before any I/O operation
	cleanName, err := fs.validatePath(name)
	if err != nil {
		return nil, err
	}

	// Open using the validated clean path
	// http.Dir.Open has its own path sanitization as additional protection
	f, err := fs.root.Open(cleanName)
	if err != nil {
		return nil, err
	}

	// Additional symlink protection: verify the real path is within bounds
	osFile, ok := f.(*os.File)
	if !ok {
		return f, nil
	}

	realPath, err := filepath.EvalSymlinks(osFile.Name())
	if err != nil {
		_ = f.Close() // #nosec G104 - best effort cleanup
		return nil, err
	}

	if !strings.HasPrefix(realPath, fs.basePath+string(filepath.Separator)) && realPath != fs.basePath {
		log.Debug().
			Str("requested_path", name).
			Str("resolved_path", realPath).
			Str("base_path", fs.basePath).
			Msg("blocked symlink escape attempt")
		_ = f.Close() // #nosec G104 - best effort cleanup
		return nil, os.ErrPermission
	}

	return f, nil
}

// wsResponseWriter wraps gin's ResponseWriter to provide proper WebSocket hijacking.
// gin's ResponseWriter fails Hijack() after WriteHeader() is called, but WebSocket
// upgrade requires both operations. This wrapper hijacks the connection early
// (before WriteHeader) and stores the raw connection for later use.
type wsResponseWriter struct {
	gin.ResponseWriter
	conn          net.Conn
	brw           *bufio.ReadWriter
	headerWritten bool
}

// Hijack returns the pre-hijacked connection, bypassing gin's "already written" check.
func (w *wsResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if w.conn == nil {
		return nil, nil, errors.New("connection was not pre-hijacked")
	}
	return w.conn, w.brw, nil
}

// Write writes data through the buffered writer to maintain consistency.
func (w *wsResponseWriter) Write(data []byte) (int, error) {
	if w.brw == nil {
		return 0, errors.New("buffered writer not available")
	}
	n, err := w.brw.Write(data)
	if err != nil {
		return n, err
	}
	return n, w.brw.Flush()
}

// WriteHeader writes the status line and headers through the buffered writer.
// Note: The http.ResponseWriter interface does not allow WriteHeader to return an error,
// so errors are logged but cannot be propagated to the caller.
// Per http.ResponseWriter contract, multiple calls should be no-ops after the first.
func (w *wsResponseWriter) WriteHeader(code int) {
	if w.headerWritten {
		return
	}
	w.headerWritten = true

	if w.brw == nil {
		log.Error().Msg("buffered writer not available during WriteHeader")
		return
	}
	statusLine := fmt.Sprintf("HTTP/1.1 %d %s\r\n", code, http.StatusText(code))
	if _, err := w.brw.WriteString(statusLine); err != nil {
		log.Error().Err(err).Msg("failed to write status line during WebSocket upgrade")
		return
	}
	if err := w.Header().Write(w.brw); err != nil {
		log.Error().Err(err).Msg("failed to write headers during WebSocket upgrade")
		return
	}
	if _, err := w.brw.WriteString("\r\n"); err != nil {
		log.Error().Err(err).Msg("failed to write header terminator during WebSocket upgrade")
		return
	}
	if err := w.brw.Flush(); err != nil {
		log.Error().Err(err).Msg("failed to flush WebSocket upgrade response")
	}
}

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

	// WebSocket interceptor - must run BEFORE CORS middleware to prevent "response already written" errors
	// CORS middleware writes headers that interfere with WebSocket hijacking
	router.Use(func(c *gin.Context) {
		if c.Request.URL.Path == "/ws" {
			log.Debug().
				Str("upgrade", c.Request.Header.Get("Upgrade")).
				Str("connection", c.Request.Header.Get("Connection")).
				Bool("written", c.Writer.Written()).
				Msg("WebSocket request received")

			if strings.EqualFold(c.Request.Header.Get("Upgrade"), "websocket") {
				env.handleWebSocketConnection(c)
				c.Abort()
				return
			}
		}
		c.Next()
	})

	router.Use(cors.New(env.corsConfig()))

	// Keep the route registered for non-upgrade requests to /ws (will return 400)
	router.GET("/ws", func(c *gin.Context) {
		c.String(http.StatusBadRequest, "WebSocket upgrade required")
	})

	// API routes
	router.GET("/healthz", env.healthz)
	router.GET("/metrics", prometheusHandler())
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

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

	// Static file serving - use NoRoute to handle unmatched paths
	// This prevents static middleware from interfering with API and WebSocket routes
	staticFilesPath := env.config.ResolveStaticFilePath()
	log.Debug().
		Str("static_path", staticFilesPath).
		Msg("serving frontend assets")

	// Get absolute path for security validation
	absStaticPath, err := filepath.Abs(staticFilesPath)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to resolve static files path")
	}

	// Create a safe file system with symlink protection
	fs := safeFileSystem{
		root:     http.Dir(absStaticPath),
		basePath: absStaticPath,
	}

	router.NoRoute(func(c *gin.Context) {
		// Try to serve the file using the safe file system
		// http.Dir handles path sanitization internally
		f, err := fs.Open(c.Request.URL.Path)
		if err == nil {
			defer f.Close()
			stat, err := f.Stat()
			if err == nil && !stat.IsDir() {
				// Safe type assertion - *os.File implements io.ReadSeeker
				rs, ok := f.(io.ReadSeeker)
				if ok {
					http.ServeContent(c.Writer, c.Request, stat.Name(), stat.ModTime(), rs)
					return
				}
			}
		}

		// Fall back to index.html for SPA routing
		c.File(filepath.Join(absStaticPath, "index.html"))
	})

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
// @Param from_timestamp query int true "From timestamp" default(1648390029)
// @Param to_timestamp query int false "To timestamp"
// @Param limit query int false "Maximum number of tasks to return"
// @Param offset query int false "Number of tasks to skip before returning results"
// @Success 200 {object} models.TasksResponse
// @Router /api/v1/tasks [get]
func (env *Env) getState(c *gin.Context) {
	startTime, err := strconv.ParseFloat(c.Query("from_timestamp"), 64)
	if err != nil && c.Query("from_timestamp") != "" {
		log.Debug().Str("from_timestamp", c.Query("from_timestamp")).Msg("invalid from_timestamp, defaulting to 0")
	}
	endTime, err := strconv.ParseFloat(c.Query("to_timestamp"), 64)
	if err != nil && c.Query("to_timestamp") != "" {
		log.Debug().Str("to_timestamp", c.Query("to_timestamp")).Msg("invalid to_timestamp, defaulting to current time")
	}
	if endTime == 0 {
		endTime = float64(time.Now().Unix())
	}
	app := c.Query("app")

	limit, err := strconv.Atoi(c.Query("limit"))
	if err != nil && c.Query("limit") != "" {
		log.Debug().Str("limit", c.Query("limit")).Msg("invalid limit, defaulting to 0")
	}
	offset, err := strconv.Atoi(c.Query("offset"))
	if err != nil && c.Query("offset") != "" {
		log.Debug().Str("offset", c.Query("offset")).Msg("invalid offset, defaulting to 0")
	}
	if limit < 0 {
		limit = 0
	}
	if offset < 0 {
		offset = 0
	}

	c.JSON(http.StatusOK, env.argo.GetTasks(startTime, endTime, app, limit, offset))
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

// handleWebSocketConnection accepts a WebSocket connection, adds it to a slice,
// and initiates a goroutine to ping the connection regularly. If WebSocket
// acceptance fails, an error is logged. The goroutine serves to monitor
// the connection's activity and removes it from the slice if it's inactive.
func (env *Env) handleWebSocketConnection(c *gin.Context) {
	options := &websocket.AcceptOptions{
		InsecureSkipVerify: env.config.DevEnvironment, // It will disable websocket host validation if set to true
	}

	// Pre-hijack the connection BEFORE WriteHeader is called
	// gin's ResponseWriter fails Hijack after WriteHeader, so we hijack first
	hijacker, ok := c.Writer.(http.Hijacker)
	if !ok {
		log.Error().Msg("ResponseWriter does not support hijacking")
		c.String(http.StatusInternalServerError, "WebSocket not supported")
		return
	}

	netConn, brw, err := hijacker.Hijack()
	if err != nil {
		log.Error().Err(err).Msg("failed to hijack connection for WebSocket")
		// After a failed hijack, the connection state is unknown and we cannot reliably
		// write a response. The client connection will eventually timeout.
		return
	}

	// Create wrapper with pre-hijacked connection
	wrappedWriter := &wsResponseWriter{
		ResponseWriter: c.Writer,
		conn:           netConn,
		brw:            brw,
	}

	conn, err := websocket.Accept(wrappedWriter, c.Request, options)
	if err != nil {
		log.Error().Err(err).Msg("failed to accept websocket connection")
		_ = netConn.Close() // #nosec G104 - best effort cleanup, already in error path
		return
	}

	// Append the connection before starting the goroutine
	connectionsMutex.Lock()
	connections = append(connections, conn)
	connectionsMutex.Unlock()

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
			_ = c.Close(websocket.StatusNormalClosure, "heartbeat failed")
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

	// Copy connections slice under mutex to avoid race condition during iteration
	connectionsMutex.Lock()
	connsCopy := make([]*websocket.Conn, len(connections))
	copy(connsCopy, connections)
	connectionsMutex.Unlock()

	for _, conn := range connsCopy {
		wg.Add(1)

		go func(c *websocket.Conn, message string) {
			defer wg.Done()
			if err := c.Write(context.Background(), websocket.MessageText, []byte(message)); err != nil {
				_ = c.Close(websocket.StatusNormalClosure, "write failed")
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
// Note: Callers are responsible for closing the connection before calling this function.
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
