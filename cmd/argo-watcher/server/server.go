package server

import (
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/argocd"
	"github.com/shini4i/argo-watcher/internal/lock"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/prometheus"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/state"
)

// initLogs initializes the logging configuration.
func initLogs(logLevel string) {
	if logLevel, err := zerolog.ParseLevel(logLevel); err != nil {
		log.Warn().Msgf("Couldn't parse log level. Got the following error: %s", err)
	} else {
		zerolog.SetGlobalLevel(logLevel)
		log.Debug().Msgf("Configured log level: %s", logLevel)
	}
}

func RunServer() {
	// initialize serverConfig
	serverConfig, err := config.NewServerConfig()
	if err != nil {
		log.Fatal().Msgf("Couldn't initialize config. Error: %s", err)
	}

	// initialize logs
	initLogs(serverConfig.LogLevel)

	// initialize metrics
	metrics := &prometheus.Metrics{}
	metrics.Init()
	metrics.Register()

	// create API client
	api := &argocd.ArgoApi{}
	if err := api.Init(serverConfig); err != nil {
		log.Fatal().Msgf("Couldn't initialize the Argo API. Got the following error: %s", err)
	}

	// create state management
	s, err := state.NewState(serverConfig)
	if err != nil {
		log.Fatal().Msgf("Couldn't create state manager (in-memory / database). Got the following error: %s", err)
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
			log.Fatal().Msg("State type is postgres, but the state object is not a PostgresState instance.")
		}
		db := pgState.GetDB()
		if db == nil {
			log.Fatal().Msg("Could not get a valid DB connection from the postgres state.")
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
		log.Fatal().Msgf("Couldn't initialize the Argo updater. Got the following error: %s", err)
	}

	// create environment
	env, err := NewEnv(serverConfig, argo, metrics, updater)
	if err != nil {
		log.Fatal().Msgf("Couldn't initialize the setup. Error: %s", err)
	}

	// start the server
	log.Info().Msg("Starting web server")
	router := env.CreateRouter()
	env.StartRouter(router)
}
