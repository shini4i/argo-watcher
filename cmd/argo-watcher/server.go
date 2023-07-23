package main

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/state"
)

// initLogs initializes the logging configuration based on the provided log level.
// It parses the log level string and sets the global log level accordingly using the zerolog library.
// If the log level string is invalid, it sets the log level to the default InfoLevel.
func initLogs(logLevel string) {
	if logLevel, err := zerolog.ParseLevel(logLevel); err != nil {
		log.Warn().Msgf("Couldn't parse log level. Got the following error: %s", err)
		logLevel = zerolog.InfoLevel
	} else {
		log.Info().Msgf("Setting log level to %s", logLevel)
		zerolog.SetGlobalLevel(logLevel)
	}
}

func serverWatcher() {
	// initialize serverConfig
	serverConfig, err := config.NewServerConfig()
	if err != nil {
		log.Fatal().Msgf("Couldn't initialize config. Error: %s", err)
	}

	// initialize logs
	initLogs(serverConfig.LogLevel)

	// initialize metrics
	metrics := &Metrics{}
	metrics.Init()
	metrics.Register()

	// create API client
	api := &ArgoApi{}
	if err := api.Init(serverConfig); err != nil {
		log.Fatal().Msgf("Couldn't initialize the Argo API. Got the following error: %s", err)
	}

	// create state management
	state, err := state.NewState(serverConfig)
	if err != nil {
		log.Fatal().Msgf("Couldn't create state manager (in-memory / database). Got the following error: %s", err)
	}
	// start cleanup go routine (retryTimes set to 0 to retry indefinitely)
	go state.ProcessObsoleteTasks(0)

	// initialize argo client
	argo := &Argo{}
	argo.Init(state, api, metrics)

	// initialize argo updater
	updater := &ArgoStatusUpdater{}
	updater.Init(*argo, serverConfig.GetRetryAttempts(), argoSyncRetryDelay, serverConfig.RegistryProxyUrl)

	// create environment
	env := &Env{config: serverConfig, argo: argo, metrics: metrics, updater: updater}

	// start the server
	log.Info().Msg("Starting web server")
	router := env.CreateRouter()
	env.StartRouter(router)
}
