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
			if strings.EqualFold(c.Request.Header.Get("Upgrade"), "websocket") {
				env.handleWebSocketConnection(c)
				c.Abort()
				return
			}
		}
		c.Next()
	})

	router.Use(cors.New(env.corsConfig()))

	// Keep the route registered for non-upgrade requests to /ws (will return 400).
	// Reaching here means the interceptor above saw no "Upgrade: websocket"
	// header, which usually means a proxy in front of argo-watcher stripped it.
	// Log the headers we did receive so that is diagnosable. Requests that do
	// upgrade are handled by the interceptor above and are deliberately not
	// logged, since every browser tab hits /ws and reconnects.
	router.GET("/ws", func(c *gin.Context) {
		slog.Debug("non-upgrade request to /ws",
			"upgrade", c.Request.Header.Get("Upgrade"),
			"connection", c.Request.Header.Get("Connection"))
		c.String(http.StatusBadRequest, "WebSocket upgrade required")
	})

	// API routes. The probe endpoints stay unauthenticated: a kubelet cannot
	// perform an OIDC flow, and they expose no state beyond up/down.
	router.GET("/livez", env.livez)
	router.GET("/readyz", env.readyz)
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

	// Routes are grouped by how they authenticate. `authenticated` holds the reads
	// only the Web UI consumes, so gating them costs no pipeline anything.
	// `open` holds the rest, each for a reason that is not negotiable:
	//   - POST /tasks takes an optional credential by design (docs/reference/api.md).
	//   - GET /config bootstraps the login flow, so it cannot require a token.
	//   - GET /tasks/:id is exempt while OIDC_REQUIRE_TASK_READ_AUTH is off, so a
	//     client polling it without a credential keeps working; the v4 UUID is the
	//     capability and the enumerable list is protected. Setting that variable moves
	//     the lookup under the same gate as every other read.
	//   - POST/DELETE /deploy-lock enforce privileged membership themselves, and are
	//     registered only under OIDC so they are never an open deploy-freeze switch.
	open := router.Group("/api/v1")
	authenticated := router.Group("/api/v1", env.requireAuthenticatedRead())
	{
		open.POST("/tasks", env.addTask)
		open.GET("/config", env.getConfig)

		if env.config.OIDC.RequireTaskReadAuth {
			authenticated.GET("/tasks/:id", env.getTaskStatus)
		} else {
			open.GET("/tasks/:id", env.countUnauthenticatedRead(), env.getTaskStatus)
		}

		authenticated.GET("/tasks", env.getState)
		authenticated.GET("/version", env.getVersion)
		// Read-only ArgoCD + state-backend reachability for the frontend
		// "unreachable" banner (issue #498). It exposes no privileged action and
		// mirrors the cached liveness-probe state without a live probe.
		authenticated.GET("/reachability", env.reachability)
		authenticated.GET(deployLockEndpoint, env.isDeployLockSet)

		if env.config.OIDC.Enabled {
			open.POST(deployLockEndpoint, env.SetDeployLock)
			open.DELETE(deployLockEndpoint, env.ReleaseDeployLock)
		}
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
	slog.Debug("listening", "address", routerBind)
	return &http.Server{
		Addr:              routerBind,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second, // Prevent Slowloris attacks
	}
}

func (env *Env) corsConfig() cors.Config {
	config := cors.Config{
		AllowMethods:           []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowHeaders:           []string{"Origin", "Content-Type", "Accept", "Authorization", oidcHeader, legacyKeycloakHeader, "ARGO_WATCHER_DEPLOY_TOKEN"},
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
