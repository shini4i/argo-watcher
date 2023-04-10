package main

import (
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/conf"
)

// reference: https://www.alexedwards.net/blog/organising-database-access
type Env struct {
	// environment configurations
	config *conf.Container
	// argo client
	client *Argo
	// metrics
	metrics *Metrics
}

func main() {
	// initialize config
	config, err := conf.InitConfig()
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

	// initialize argo client
	client := Argo{
		Url:     config.ArgoUrl,
		Token:   config.ArgoToken,
		Timeout: config.ArgoApiTimeout,
	}

	if err := client.InitArgo(config, &metrics); err != nil {
		log.Error().Msgf("Couldn't initialize the client. Got the following error: %s", err)
		os.Exit(1)
	}

	// create environment
	env := &Env{config: config, client: &client, metrics: &metrics}

	// start the server
	log.Info().Msg("Starting web server")
	router := createRouter(env)
	routerBind := fmt.Sprintf("%s:%s", config.Host, config.Port)
	log.Debug().Msgf("Listening on %s", routerBind)
	if err := router.Run(routerBind); err != nil {
		panic(err)
	}
}
