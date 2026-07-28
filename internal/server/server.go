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

const (
	// shutdownBudget is the total wall-clock allowance for the whole shutdown
	// sequence. It must stay below the pod's terminationGracePeriodSeconds
	// (Kubernetes defaults to 30s) so every phase — including the batch write-back
	// drain that runs last — gets to finish before the kubelet sends SIGKILL.
	shutdownBudget = 25 * time.Second
	// httpDrainBudget caps the HTTP-request drain so the phases after it are not
	// starved. argo-watcher's handlers are short (task list, add task); long-lived
	// WebSocket connections are hijacked and drained separately.
	httpDrainBudget = 8 * time.Second
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

	// The distributed Postgres locker and the shared deploy lock both require the
	// Postgres state; otherwise fall back to in-memory equivalents, which are
	// correct for a single replica only.
	var locker lock.Locker
	var deployLockStore lock.DeployLockStore
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
		deployLockStore = lock.NewPostgresDeployLockStore(db)
		slog.Info("Using Postgres advisory locks for distributed locking and a shared deploy lock.")
	} else {
		locker = lock.NewInMemoryLocker()
		deployLockStore = lock.NewInMemoryDeployLockStore()
		slog.Warn("Using in-memory lock and deploy lock. This is not suitable for HA setups.")
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

	env, err := NewEnv(serverConfig, argo, metrics, statusUpdater, deployLockStore)
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

	// The only notifier of deploy-lock changes to Web UI clients — manual locks
	// (including ones set through another replica), schedule boundaries and override
	// expiry alike. Must always run; see StartLockdownWatcher.
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
	// The three phases below run in sequence and share one deadline, so their total
	// cannot exceed shutdownBudget. Previously each carried its own independent
	// timeout, letting the sequence run far longer than any realistic
	// terminationGracePeriodSeconds — the kubelet then SIGKILLed the process partway
	// through, which defeats the phase that matters most (draining queued git
	// write-backs, last in line).
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownBudget)
	defer cancelShutdown()

	// Phase 1: let outstanding HTTP requests finish. Capped well below the total
	// because argo-watcher's handlers are short-lived; hijacked WebSocket
	// connections are not waited on here at all (they are phase 2).
	httpCtx, cancelHTTP := context.WithTimeout(shutdownCtx, httpDrainBudget)
	err := srv.Shutdown(httpCtx)
	cancelHTTP()
	if err != nil {
		slog.Error("server forced to shutdown", "error", err)
	}

	// Phase 2: now that the listener is closed, signal the WebSocket connection
	// goroutines to stop and wait for them to finish.
	wsCtx, cancelWS := context.WithTimeout(shutdownCtx, shutdownTimeout)
	s.env.Shutdown(wsCtx)
	cancelWS()

	// Phase 3: drain any in-flight batch write-backs so queued commits are not
	// abandoned mid-flush. Gets whatever the earlier phases left of the budget; Close
	// also tells retry loops to stop at their next boundary, which removes the
	// attempts-per-batch multiplier but does not bound one attempt or the number of
	// queued batches (see Batcher.Close). No-op when batch mode is disabled.
	if s.updater != nil {
		s.updater.Close(shutdownCtx)
	}

	slog.Info("server exited")
}
