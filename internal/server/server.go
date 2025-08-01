package server

import (
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/argocd"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	prom "github.com/shini4i/argo-watcher/cmd/argo-watcher/prometheus"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/state"
	"github.com/shini4i/argo-watcher/internal/lock"
)

type Server struct {
	router  *gin.Engine
	config  *config.ServerConfig
	argo    *argocd.Argo
	metrics *prom.Metrics
	updater *argocd.ArgoStatusUpdater
	env     *Env
}

// NewServer creates a new server instance with the given configuration and prometheus registerer.
func NewServer(serverConfig *config.ServerConfig, reg prometheus.Registerer) (*Server, error) {
	// initialize logs
	initLogs(serverConfig.LogLevel)

	// initialize metrics on the provided prometheus registry
	metrics := prom.NewMetrics(reg)

	// create API client
	api := &argocd.ArgoApi{}
	if err := api.Init(serverConfig); err != nil {
		return nil, err
	}

	// create state management
	s, err := state.NewState(serverConfig)
	if err != nil {
		return nil, err
	}
	// start cleanup go routine
	go s.ProcessObsoleteTasks(0)

	// initialize argo client
	argo := &argocd.Argo{}
	argo.Init(s, api, metrics)

	// Create the locker instance based on the state type
	var locker lock.Locker
	if serverConfig.StateType == "postgres" {
		pgState, ok := s.(*state.PostgresState)
		if !ok {
			return nil, err
		}
		db := pgState.GetDB()
		if db == nil {
			return nil, err
		}
		locker = lock.NewPostgresLocker(db)
		log.Info().Msg("Using Postgres advisory locks for distributed locking.")
	} else {
		locker = lock.NewInMemoryLocker()
		log.Warn().Msg("Using in-memory lock. This is not suitable for HA setups.")
	}

	// initialize argo updater
	updater := &argocd.ArgoStatusUpdater{}
	err = updater.Init(*argo,
		serverConfig.GetRetryAttempts(),
		argocd.ArgoSyncRetryDelay,
		serverConfig.RegistryProxyUrl,
		serverConfig.RepoCachePath,
		serverConfig.AcceptSuspendedApp,
		&serverConfig.Webhook,
		locker,
	)
	if err != nil {
		return nil, err
	}

	// create environment
	env, err := NewEnv(serverConfig, argo, metrics, updater)
	if err != nil {
		return nil, err
	}

	// create router
	router := env.CreateRouter()

	return &Server{
		router:  router,
		config:  serverConfig,
		argo:    argo,
		metrics: metrics,
		updater: updater,
		env:     env,
	}, nil
}

func (s *Server) Run() {
	log.Info().Msg("Starting web server")
	s.env.StartRouter(s.router)
}

// initLogs initializes the logging configuration.
func initLogs(logLevel string) {
	if lvl, err := zerolog.ParseLevel(logLevel); err != nil {
		log.Warn().Msgf("Couldn't parse log level. Got the following error: %s", err)
	} else {
		zerolog.SetGlobalLevel(lvl)
		log.Debug().Msgf("Configured log level: %s", lvl)
	}
}
