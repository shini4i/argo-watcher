package main

import (
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/state"
)

func main() {
	// initialize serverConfig
	serverConfig, err := config.NewServerConfig()
	if err != nil {
		log.Error().Msgf("Couldn't initialize config. Error: %s", err)
		os.Exit(1)
	}

	// initialize logs
	logLevel, err := zerolog.ParseLevel(serverConfig.LogLevel)
	if err != nil {
		log.Warn().Msgf("Couldn't parse log level. Got the following error: %s", err)
		logLevel = zerolog.InfoLevel
	}

	log.Debug().Msgf("Setting log level to %s", logLevel)
	zerolog.SetGlobalLevel(logLevel)

	// initialize metrics
	metrics := &Metrics{}
	metrics.Init()
	metrics.Register()

	// create API client
	api := &ArgoApi{}
	if err := api.Init(serverConfig); err != nil {
		log.Error().Msgf("Couldn't initialize the Argo API. Got the following error: %s", err)
		os.Exit(1)
	}

	// create state management
	state, err := state.NewState(serverConfig)
	if err != nil {
		log.Error().Msgf("Couldn't create state manager (in-memory / database). Got the following error: %s", err)
		os.Exit(1)
	}
	// start cleanup go routine
	go state.ProcessObsoleteTasks()

	// initialize argo client
	argo := &Argo{}
	argo.Init(state, api, metrics)

	// initialize argo updater
	updater := &ArgoStatusUpdater{}
	updater.Init(*argo, serverConfig.GetRetryAttempts())
	
	// create environment
	env := &Env{config: serverConfig, argo: argo, metrics: metrics, updater: updater}

	// start the server
	log.Info().Msg("Starting web server")
	router := env.CreateRouter()
	env.StartRouter(router)
}
