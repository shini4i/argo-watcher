package main

import (
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/conf"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/state"
)

func main() {
	// initialize config
	config, err := conf.Init()
	if err != nil {
		log.Error().Msgf("Couldn't initialize config. Error: %s", err)
		os.Exit(1)
	}

	// initialize logs
	logLevel, err := zerolog.ParseLevel(config.LogLevel)
	if err != nil {
		log.Warn().Msgf("Couldn't parse log level. Got the following error: %s", err)
		logLevel = zerolog.InfoLevel
	}

	log.Debug().Msgf("Setting log level to %s", logLevel)
	zerolog.SetGlobalLevel(logLevel)

	// initialize metrics
	metrics := Metrics{}
	metrics.Init()
	metrics.Register()

	// create API client
	api := ArgoApi{}
	if err := api.Init(config); err != nil {
		log.Error().Msgf("Couldn't initialize the Argo API. Got the following error: %s", err)
		os.Exit(1)
	}

	// create state management
	state, err := state.NewState(config)
	if err != nil {
		log.Error().Msgf("Couldn't create state manager (in-memory / database). Got the following error: %s", err)
		os.Exit(1)
	}
	// start cleanup go routine
	go state.ProcessObsoleteTasks()

	// initialize argo client
	client := Argo{}
	client.Init(&state, &api, &metrics, config.GetRetryAttempts())

	// create environment
	env := &Env{config: config, client: &client, metrics: &metrics}

	// start the server
	log.Info().Msg("Starting web server")
	router := env.CreateRouter()
	env.StartRouter(router)
}
