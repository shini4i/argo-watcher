package server

import (
	"fmt"
	"net/http"
	"time"

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
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

var version = "local"

const (
	deployLockEndpoint  = "/deploy-lock"
	unauthorizedMessage = "You are not authorized to perform this action"
	keycloakHeader      = "Keycloak-Authorization"
	historyExportBatch  = 1000
)

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
		v1.GET("/tasks", env.stateHandler)
		v1.GET("/tasks/:id", env.taskStatusHandler)
		v1.GET("/tasks/export", env.exportTasks)
		v1.GET("/version", env.versionHandler)
		v1.GET("/config", env.configHandler)
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
