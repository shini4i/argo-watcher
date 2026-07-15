package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/shini4i/argo-watcher/internal/argocd"
	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/lock"
	prom "github.com/shini4i/argo-watcher/internal/prometheus"
	"github.com/shini4i/argo-watcher/internal/state"
)

type Server struct {
	router      *gin.Engine
	config      *config.ServerConfig
	argo        *argocd.Argo
	metrics     *prom.Metrics
	updater     *argocd.ArgoStatusUpdater
	env         *Env
	probeCancel context.CancelFunc
}

// NewServer creates a new server instance with the given configuration and prometheus registerer.
func NewServer(serverConfig *config.ServerConfig, reg prometheus.Registerer) (*Server, error) {
	// initialize logs
	initLogs(serverConfig.LogLevel)

	// initialize metrics on the provided prometheus registry
	metrics := prom.NewMetrics(reg)

	// create API client
	api := argocd.NewArgoApi()
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
			return nil, fmt.Errorf("state type is postgres but state object is not a PostgresState instance (got %T)", s)
		}
		db := pgState.GetDB()
		if db == nil {
			return nil, fmt.Errorf("could not get a valid DB connection from the postgres state")
		}
		locker = lock.NewPostgresLocker(db)
		slog.Info("Using Postgres advisory locks for distributed locking.")
	} else {
		locker = lock.NewInMemoryLocker()
		slog.Warn("Using in-memory lock. This is not suitable for HA setups.")
	}

	// initialize argo updater
	updater := &argocd.ArgoStatusUpdater{}
	err = updater.Init(*argo, argocd.ArgoStatusUpdaterConfig{
		RetryAttempts:    serverConfig.GetRetryAttempts(),
		RetryDelay:       argocd.ArgoSyncRetryDelay,
		RegistryProxyURL: serverConfig.RegistryProxyUrl,
		RepoCachePath:    serverConfig.RepoCachePath,
		AcceptSuspended:  serverConfig.AcceptSuspendedApp,
		RefreshApp:       serverConfig.ArgoRefreshApp,
		WebhookConfig:    &serverConfig.Webhook,
		MattermostConfig: &serverConfig.Mattermost,
		Locker:           locker,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize the argo updater: %w", err)
	}

	// create environment
	env, err := NewEnv(serverConfig, argo, metrics, updater)
	if err != nil {
		return nil, err
	}

	// create router
	router := env.CreateRouter()

	// Keep the argocd_unavailable metric fresh via a background probe. The task
	// list read path no longer performs an ArgoCD check (so it can't hang on an
	// outage), so this is the only ambient refresher of that gauge. Tie it to a
	// cancellable context so graceful shutdown (and test teardown) can stop the
	// goroutine instead of leaking it. Launched last, past every early-return
	// error path, so the cancel is always owned by the returned Server.
	probeCtx, probeCancel := context.WithCancel(context.Background())
	go argo.StartLivenessProbe(probeCtx, argocd.ArgoLivenessProbeInterval)

	return &Server{
		router:      router,
		config:      serverConfig,
		argo:        argo,
		metrics:     metrics,
		updater:     updater,
		env:         env,
		probeCancel: probeCancel,
	}, nil
}

// Run starts the HTTP server and handles graceful shutdown on SIGINT/SIGTERM.
func (s *Server) Run() {
	slog.Info("Starting web server")

	srv := s.env.StartRouter(s.router)

	// Notify clients about scheduled lockdown transitions they wouldn't
	// otherwise learn about (scheduled state is evaluated lazily).
	s.env.StartLockdownWatcher()

	// Start server in goroutine
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("failed to start server", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server...")

	// Stop the background ArgoCD liveness probe.
	if s.probeCancel != nil {
		s.probeCancel()
	}

	// Stop accepting new connections first and let outstanding HTTP requests
	// drain (up to 30 seconds), then shut down the WebSocket goroutines. Closing
	// the listener before env.Shutdown means new handshakes can no longer arrive,
	// which greatly narrows the window in which a WebSocket handler could call
	// connWg.Add(1) after env.Shutdown has begun waiting on connWg (a WaitGroup
	// misuse that could panic during shutdown). It does not fully eliminate it: a
	// handshake already past the hijack but not yet registered is untracked by
	// srv.Shutdown, so it can still register in that nanosecond gap — an
	// acceptable residual given it can only occur on an already-terminating
	// process. Hijacked WebSocket connections are not waited on by srv.Shutdown;
	// they are drained by env.Shutdown below.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
	}

	// Now that the listener is closed, signal the WebSocket connection
	// goroutines to stop and wait for them to finish.
	s.env.Shutdown()

	slog.Info("server exited")
}

// logLevelVar holds the active log level for the default logger so initLogs can
// adjust it at runtime.
var logLevelVar = new(slog.LevelVar)

// initLogs configures the global slog logger to emit JSON to stderr at the
// level parsed from logLevel. An unparseable level is logged as a warning and
// leaves the logger at its default (info) level.
func initLogs(logLevel string) {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevelVar})))
	if lvl, err := parseLogLevel(logLevel); err != nil {
		slog.Warn(fmt.Sprintf("Couldn't parse log level. Got the following error: %s", err))
	} else {
		logLevelVar.Set(lvl)
		slog.Debug(fmt.Sprintf("Configured log level: %s", lvl))
	}
}

// parseLogLevel maps a textual log level to its slog equivalent. It accepts the
// level names previously handled by zerolog (including the trace/fatal/panic
// aliases) so existing LOG_LEVEL values keep working; unknown values error.
func parseLogLevel(level string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "trace", "debug":
		return slog.LevelDebug, nil
	case "", "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error", "fatal", "panic":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown log level %q", level)
	}
}
