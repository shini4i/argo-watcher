package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const deployLockEndpoint = "/deploy-lock"

// CreateRouter initialize router.
func (env *Env) CreateRouter() *gin.Engine {
	// Initialize shutdown channel if not set (for tests that create Env directly)
	if env.shutdownCh == nil {
		env.shutdownCh = make(chan struct{})
	}

	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Recovery())

	// WebSocket interceptor - must run BEFORE CORS middleware to prevent "response already written" errors
	// CORS middleware writes headers that interfere with WebSocket hijacking
	router.Use(func(c *gin.Context) {
		if c.Request.URL.Path == "/ws" {
			slog.Debug("WebSocket request received",
				"upgrade", c.Request.Header.Get("Upgrade"),
				"connection", c.Request.Header.Get("Connection"),
				"written", c.Writer.Written())

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
	swaggerPath := filepath.Join(env.config.StaticFilePath, "swagger")
	absSwaggerPath, err := filepath.Abs(swaggerPath)
	if err != nil {
		slog.Error("failed to resolve swagger files path", "error", err)
		os.Exit(1)
	}
	resolvedSwaggerPath, err := filepath.EvalSymlinks(absSwaggerPath)
	if err != nil {
		resolvedSwaggerPath = absSwaggerPath
	}
	swaggerFS := safeFileSystem{
		root:     http.Dir(absSwaggerPath),
		basePath: resolvedSwaggerPath,
	}
	router.StaticFS("/swagger", swaggerFS)

	v1 := router.Group("/api/v1")
	{
		v1.POST("/tasks", env.addTask)
		v1.GET("/tasks", env.getState)
		v1.GET("/tasks/:id", env.getTaskStatus)
		v1.GET("/version", env.getVersion)
		v1.GET("/config", env.getConfig)
		// The state-changing deploy-lock endpoints are only registered when Keycloak
		// is enabled: without an auth backend they cannot be protected, so exposing
		// them would leave an unauthenticated deploy-freeze switch reachable by anyone
		// who can reach the server (including via a victim's browser). The read-only
		// GET stays available so the banner and scheduled lockdown keep working.
		if env.config.Keycloak.Enabled {
			v1.POST(deployLockEndpoint, env.SetDeployLock)
			v1.DELETE(deployLockEndpoint, env.ReleaseDeployLock)
		}
		v1.GET(deployLockEndpoint, env.isDeployLockSet)
	}

	// Static file serving - use NoRoute to handle unmatched paths
	// This prevents static middleware from interfering with API and WebSocket routes
	staticFilesPath := env.config.StaticFilePath
	slog.Debug("serving frontend assets", "static_path", staticFilesPath)

	// Get absolute path for security validation
	absStaticPath, err := filepath.Abs(staticFilesPath)
	if err != nil {
		slog.Error("failed to resolve static files path", "error", err)
		os.Exit(1)
	}

	// Resolve symlinks in the base path to ensure consistent path comparison
	// This is important on macOS where /var is a symlink to /private/var
	resolvedBasePath, err := filepath.EvalSymlinks(absStaticPath)
	if err != nil {
		// If symlink resolution fails (e.g., path doesn't exist yet), fall back to absolute path
		resolvedBasePath = absStaticPath
	}

	// Create a safe file system with symlink protection
	fs := safeFileSystem{
		root:     http.Dir(absStaticPath),
		basePath: resolvedBasePath,
	}

	router.NoRoute(env.createStaticFileHandler(fs, absStaticPath))

	return router
}

// StartRouter creates and returns an HTTP server configured with the given router.
// The caller is responsible for starting the server and handling graceful shutdown.
func (env *Env) StartRouter(router *gin.Engine) *http.Server {
	routerBind := fmt.Sprintf("%s:%s", env.config.Host, env.config.Port)
	slog.Debug(fmt.Sprintf("Listening on %s", routerBind))
	return &http.Server{
		Addr:              routerBind,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second, // Prevent Slowloris attacks
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
