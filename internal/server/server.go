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

	"github.com/prometheus/client_golang/prometheus"

	"github.com/shini4i/argo-watcher/internal/apptoken"
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
	// readinessDrainDelay is how long the process keeps serving after its readiness
	// probe starts answering 503, before the listener closes. Removing a pod from a
	// Service is asynchronous — kube-proxy and ingress controllers keep sending
	// traffic for a short while after the endpoint is marked not-ready — so closing
	// the listener immediately drops that traffic. It is charged to shutdownBudget
	// rather than added on top, so the sequence still fits the pod grace period.
	readinessDrainDelay = 5 * time.Second
	// httpDrainBudget caps the HTTP-request drain so the phases after it are not
	// starved. argo-watcher's handlers are short (task list, add task) and finish
	// well inside it; long-lived WebSocket connections are hijacked and drained
	// separately.
	httpDrainBudget = 5 * time.Second
)

type Server struct {
	router      http.Handler
	config      *config.ServerConfig
	argo        *argocd.Argo
	metrics     *prom.Metrics
	updater     *argocd.ArgoStatusUpdater
	env         *Env
	probeCancel context.CancelFunc
	// drainDelay is how long shutdown waits after failing readiness before closing
	// the listener. NewServer sets it to readinessDrainDelay; tests shorten it.
	drainDelay time.Duration
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
	go s.ProcessObsoleteTasks(0)

	argo := &argocd.Argo{}
	argo.Init(s, api, metrics)

	// The distributed Postgres locker and the shared deploy lock both require the
	// Postgres state; otherwise fall back to in-memory equivalents, which are
	// correct for a single replica only.
	var locker lock.Locker
	var deployLockStore lock.DeployLockStore
	// Nil unless the state is Postgres, which turns application deploy tokens off:
	// they must survive a restart and be visible to every replica.
	var appTokenStore apptoken.Store
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
		appTokenStore = apptoken.NewPostgresStore(db)
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

	env, err := NewEnv(serverConfig, argo, metrics, statusUpdater, deployLockStore, appTokenStore)
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
		drainDelay:  readinessDrainDelay,
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

	// Take over deployments whose replica stopped monitoring them, so losing a pod
	// mid-rollout costs a few seconds of unattended time rather than the whole
	// deployment (issue #152).
	s.env.StartTaskReaper()

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("failed to start server", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server...")

	if s.probeCancel != nil {
		s.probeCancel()
	}

	s.shutdown(srv)

	slog.Info("server exited")
}

// httpShutdowner is the one method of *http.Server the shutdown sequence needs.
// Narrowing it to an interface is what lets the phase budgeting be tested without
// binding a listener or waiting out a real drain.
type httpShutdowner interface {
	Shutdown(ctx context.Context) error
}

// shutdown runs the ordered drain phases: fail readiness so no new traffic is
// routed here, then outstanding HTTP requests, then WebSocket goroutines, then
// queued git write-backs.
//
// Failing readiness before touching the listener is what keeps a rolling update
// from resetting connections: the endpoint removal it triggers is asynchronous, so
// the process must stay up long enough for that removal to propagate.
//
// Stop accepting new connections next and let outstanding HTTP requests drain,
// then shut down the WebSocket goroutines. Closing the listener before env.Shutdown
// means new handshakes can no longer arrive, which is what keeps a WebSocket handler
// from calling connWg.Add(1) after env.Shutdown has begun waiting on connWg (a
// WaitGroup misuse that could panic during shutdown). The handler registers before it
// upgrades, while net/http still counts the request as active, so srv.Shutdown waits
// for handshakes that are in flight. The one remaining way to overlap the two is a
// handshake still running when httpDrainBudget expires, since srv.Shutdown then
// returns with that handler outstanding. Hijacked WebSocket connections are not
// waited on by srv.Shutdown; they are drained by env.Shutdown.
//
// The phases run in sequence and share one deadline, so their total cannot exceed
// shutdownBudget. It must stay inside terminationGracePeriodSeconds: a SIGKILL partway
// through defeats the phase that matters most (draining queued git write-backs, last
// in line).
//
// No phase failure short-circuits the sequence: a forced HTTP shutdown is logged and
// the later phases still get their share, because dropping a queued commit over an
// unrelated stuck HTTP request would be the worse outcome.
func (s *Server) shutdown(srv httpShutdowner) {
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownBudget)
	defer cancelShutdown()

	// Phase 0: report not-ready and give the orchestrator time to stop routing new
	// requests here. The process keeps serving normally throughout — this window is
	// spent staying up, not winding down.
	s.env.beginDraining()
	if s.drainDelay > 0 {
		slog.Info("readiness reported down, waiting for traffic to be routed away", "delay", s.drainDelay)
		select {
		case <-time.After(s.drainDelay):
		case <-shutdownCtx.Done():
		}
	}

	httpCtx, cancelHTTP := context.WithTimeout(shutdownCtx, httpDrainBudget)
	err := srv.Shutdown(httpCtx)
	cancelHTTP()
	if err != nil {
		slog.Error("server forced to shutdown", "error", err)
	}

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

	// Phase 4: hand the rollouts this replica was watching to the others. The
	// monitoring goroutines are about to die with the process, and without this
	// their tasks would sit unattended until their leases lapse. Last, so a
	// surviving replica does not resume a write-back the drain above is still
	// flushing. No-op with in-memory state, where nothing else can pick them up.
	s.env.releaseTaskLeases()
}
