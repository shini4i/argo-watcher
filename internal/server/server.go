package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/shini4i/argo-watcher/internal/argocd"
	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/lock"
	"github.com/shini4i/argo-watcher/internal/logging"
	prom "github.com/shini4i/argo-watcher/internal/prometheus"
	"github.com/shini4i/argo-watcher/internal/state"
	"github.com/shini4i/argo-watcher/internal/updater"
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
	logging.Init(serverConfig.LogLevel)
	metrics := prom.NewMetrics(reg)

	api := argocd.NewArgoApi()
	if err := api.Init(serverConfig); err != nil {
		return nil, err
	}

	s, err := state.NewState(serverConfig)
	if err != nil {
		return nil, err
	}
	// Background cleanup of obsolete tasks.
	go s.ProcessObsoleteTasks(0)

	argo := &argocd.Argo{}
	argo.Init(s, api, metrics)

	// The distributed Postgres locker requires the Postgres state; otherwise fall
	// back to an in-memory lock (single-instance only).
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

	// Batch write-back settings are parsed independently of the full git config so
	// servers that do not use git write-back (no SSH_KEY_PATH) still start.
	batchConfig, err := updater.NewBatchConfig()
	if err != nil {
		return nil, err
	}

	statusUpdater := &argocd.ArgoStatusUpdater{}
	err = statusUpdater.Init(*argo, argocd.ArgoStatusUpdaterConfig{
		RetryAttempts:    serverConfig.GetRetryAttempts(),
		RetryDelay:       argocd.ArgoSyncRetryDelay,
		RegistryProxyURL: serverConfig.RegistryProxyUrl,
		RepoCachePath:    serverConfig.RepoCachePath,
		AcceptSuspended:  serverConfig.AcceptSuspendedApp,
		RefreshApp:       serverConfig.ArgoRefreshApp,
		WebhookConfig:    &serverConfig.Webhook,
		MattermostConfig: &serverConfig.Mattermost,
		Locker:           locker,
		BatchWriteBack:   batchConfig.Enabled,
		BatchMaxSize:     batchConfig.MaxSize,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize the argo updater: %w", err)
	}

	env, err := NewEnv(serverConfig, argo, metrics, statusUpdater)
	if err != nil {
		return nil, err
	}

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
		updater:     statusUpdater,
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

	// Notify clients when ArgoCD reachability changes so the frontend can show
	// or hide the "ArgoCD unreachable" banner (issue #498).
	s.env.StartArgoWatcher()

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

	// Drain any in-flight batch write-backs so queued commits are not abandoned
	// mid-flush. No-op when batch mode is disabled.
	if s.updater != nil {
		s.updater.Close()
	}

	slog.Info("server exited")
}
