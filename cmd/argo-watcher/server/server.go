package server

import (
	"os"
	"time"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/auth"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/argocd"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/queue"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/prometheus"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/state"
)

// initLogs initializes the logging configuration based on the provided log level.
// It parses the log level string and sets the global log level accordingly using the zerolog library.
// If the log level string is invalid, it falls back to the default InfoLevel.
func initLogs(logLevel string, logFormat string) {
	// set log format
	if logFormat == config.LogFormatText {
		output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
		log.Logger = zerolog.New(output).With().Timestamp().Logger()
	}
	// set log level
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
	initLogs(serverConfig.LogLevel, serverConfig.LogFormat)

	// initialize metrics
	metrics := &prometheus.Metrics{}
	metrics.Init()
	metrics.Register()

	// create API client
	api := &argocd.ArgoApi{}
	if err := api.Init(serverConfig); err != nil {
		log.Fatal().Msgf("Couldn't initialize the Argo API. Got the following error: %s", err)
	}

	// create argoState management
	argoState, err := state.NewState(serverConfig)
	if err != nil {
		log.Fatal().Msgf("Couldn't create state manager (in-memory / database). Got the following error: %s", err)
	}
	// start cleanup go routine (retryTimes set to 0 to retry indefinitely)
	go argoState.ProcessObsoleteTasks(0)

	// initialize argo client
	argo := &argocd.Argo{}
	argo.Init(argoState, api, metrics)

	// initialize argo updater
	updater := &argocd.ArgoStatusUpdater{}
	updater.Init(*argo,
		serverConfig.GetRetryAttempts(),
		argocd.ArgoSyncRetryDelay,
		serverConfig.RegistryProxyUrl,
		serverConfig.AcceptSuspendedApp,
	)

	// initialize queue manager
	queueManager := &queue.QueueManager{Updater: updater}
	queueManager.Connect(serverConfig)
	// TODO: process any tasks that are in status "Queued" but aren't in the queue, e.g. before we start listening
	queueManager.StartListen()

	// create environment
	env := &Env{config: serverConfig, argo: argo, metrics: metrics, queueManager: queueManager}

	// initialize auth service
	if serverConfig.Keycloak.Url != "" {
		env.auth = auth.NewExternalAuthService()
		env.auth.Init(
			serverConfig.Keycloak.Url,
			serverConfig.Keycloak.Realm,
			serverConfig.Keycloak.ClientId,
			serverConfig.Keycloak.PrivilegedGroups,
		)
	}

	// set up cron jobs
	if env.config.ScheduledLockdownEnabled {
		env.SetCron(env.config.LockdownSchedule)
	}

	// start the server
	log.Info().Msg("Starting web server")
	router := env.CreateRouter()
	env.StartRouter(router)
}
