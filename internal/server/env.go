package server

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shini4i/argo-watcher/internal/apptoken"
	"github.com/shini4i/argo-watcher/internal/argocd"
	"github.com/shini4i/argo-watcher/internal/auth"
	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/lock"
	"github.com/shini4i/argo-watcher/internal/prometheus"
)

// Env reference: https://www.alexedwards.net/blog/organising-database-access
type Env struct {
	config        *config.ServerConfig
	argo          *argocd.Argo
	updater       *argocd.ArgoStatusUpdater
	metrics       *prometheus.Metrics
	lockdown      *Lockdown
	strategies    map[string]auth.AuthStrategy
	authenticator *auth.Authenticator
	// appTokens is nil unless application deploy tokens are available, which is
	// what the issue and revoke endpoints are registered on.
	appTokens apptoken.Store
	// shutdownCh is closed to signal graceful shutdown to all WebSocket goroutines.
	shutdownCh chan struct{}
	// draining is set once graceful shutdown begins, so the readiness probe can
	// report the pod out of service while it is still able to serve. It is separate
	// from shutdownCh, which is closed later in the sequence (after the listener),
	// and deliberately never reset: shutdown is terminal.
	draining     atomic.Bool
	shutdownOnce sync.Once
	// connWg tracks active WebSocket connection goroutines for graceful shutdown.
	connWg sync.WaitGroup
}

// lockdownPollInterval is how often the lockdown watcher re-evaluates the lock
// state to detect a transition it was not told about: a schedule boundary, an
// override expiring, or — with a shared deploy lock store — a lock another
// replica set or released. The last of those can happen at any instant, so the
// tick is well below the minute granularity of schedules. Each tick is one
// indexed single-row read, or no I/O at all with the in-memory store.
//
// This bounds only how quickly the *banner* updates, including on the replica
// that served the request. Enforcement is not affected: every deploy request
// resolves the lock state at that moment.
const lockdownPollInterval = 5 * time.Second

// StartLockdownWatcher launches a background goroutine that notifies WebSocket
// clients whenever the resolved lock state changes: an operator setting or
// releasing the lock through any replica, a scheduled window opening or closing,
// or a temporary override expiring.
//
// It is the *only* thing that pushes lock state to clients. The API handlers
// deliberately stay silent, because the watcher compares each poll against the
// state it last broadcast: a push from elsewhere would leave that baseline stale
// and could swallow a later transition (see TestDeployLockNotifiedOnlyByWatcher).
// The price is that clients on the replica serving a lock change learn about it
// within one poll interval rather than instantly — the same delay every other
// replica already had. The goroutine is tracked by connWg and stops when the
// shutdown channel is closed.
func (env *Env) StartLockdownWatcher() {
	env.connWg.Add(1)
	go func() {
		defer env.connWg.Done()
		env.lockdown.WatchTransitions(env.shutdownCh, lockdownPollInterval, notifyWebSocketClients)
	}()
}

// WebSocket messages pushed when ArgoCD reachability changes. Clients treat
// argoDownMessage as "show the unreachable banner" and argoUpMessage as "clear
// it". A down message carries the cause as a suffix ("argocd_down:<reason>", see
// argoStatusMessage) so the banner can name which subsystem is unreachable.
// Kept in sync with the frontend argocdStatus feature (issue #498).
const (
	argoUpMessage   = "argocd_up"
	argoDownMessage = "argocd_down"
)

func argoStatusMessage(reason string) string {
	if reason == argocd.ReasonNone {
		return argoUpMessage
	}
	return argoDownMessage + ":" + reason
}

// argoWatchInterval is how often the ArgoCD-availability watcher samples the
// cached reachability to detect a transition. The liveness probe refreshes that
// state every argocd.ArgoLivenessProbeInterval; sampling it more frequently
// bounds how quickly clients see the banner appear or clear after a transition,
// while adding no live ArgoCD calls (each sample is a single atomic load).
const argoWatchInterval = 5 * time.Second

// StartArgoWatcher launches a background goroutine that notifies WebSocket
// clients when ArgoCD reachability changes, so the frontend can show or hide the
// "ArgoCD unreachable" banner (issue #498). The cached reachability is refreshed
// by the liveness probe; this watcher only observes it and pushes transitions.
// Clients connecting mid-outage learn the current state via the reachability
// endpoint instead. The goroutine is tracked by connWg and stops when the
// shutdown channel is closed.
func (env *Env) StartArgoWatcher() {
	env.connWg.Add(1)
	go func() {
		defer env.connWg.Done()
		watchArgoTransitions(env.shutdownCh, argoWatchInterval, env.argo.UnavailableReason, notifyWebSocketClients)
	}()
}

// watchArgoTransitions samples the unavailability reason on the given interval
// and invokes notify with the matching payload (see argoStatusMessage) whenever
// it changes. Sampling the reason rather than a bare boolean means a switch
// between causes (e.g. database-only to both) also pushes an update, so the
// banner wording stays accurate. The initial state is recorded without
// notifying, so only genuine transitions produce a message. It runs until stop
// is closed. Dependencies are parameters so the logic can be unit-tested.
func watchArgoTransitions(stop <-chan struct{}, interval time.Duration, reason func() string, notify func(string)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	last := reason()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			current := reason()
			if current == last {
				continue
			}
			last = current
			notify(argoStatusMessage(current))
		}
	}
}

// shutdownTimeout caps the WebSocket-drain phase of shutdown. The caller derives
// the context passed to Shutdown from it, so the phase can also end earlier if the
// overall shutdown budget (see shutdownBudget) is already spent.
const shutdownTimeout = 10 * time.Second

// beginDraining marks the process as shutting down so the readiness probe starts
// answering 503. It is called at the very start of the shutdown sequence, before
// the listener closes, so an orchestrator has a window to stop routing new requests
// here while the pod can still serve the ones already in flight.
func (env *Env) beginDraining() {
	env.draining.Store(true)
}

func (env *Env) isDraining() bool {
	return env.draining.Load()
}

// Shutdown gracefully shuts down the server and all WebSocket connections.
// This method is safe to call multiple times. It blocks until all WebSocket
// goroutines have finished or ctx expires. If it gives up, some goroutines may
// still be running but should exit shortly as they observe the closed shutdownCh.
// Any long-running WebSocket writes are bounded by their own 5-second timeout in
// checkConnection.
//
// ctx is the caller's remaining shutdown budget: this is one phase of a sequence
// (see Server.shutdown), and the phases after it — notably draining queued git
// write-backs — need what is left of it.
func (env *Env) Shutdown(ctx context.Context) {
	if env.shutdownCh != nil {
		env.shutdownOnce.Do(func() {
			close(env.shutdownCh)
		})
	}

	done := make(chan struct{})
	go func() {
		env.connWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.Debug("All WebSocket connections closed gracefully")
	case <-ctx.Done():
		slog.Warn("Shutdown timeout reached, some WebSocket goroutines may still be running", "error", ctx.Err())
	}
}

// NewEnv wires up an Env from the server config: lockdown schedules backed by the
// given deploy lock store, and the enabled auth strategies. appTokenStore is nil
// when application deploy tokens are unavailable, which the caller decides from
// the state backend; they additionally require OIDC.
func NewEnv(serverConfig *config.ServerConfig, argo *argocd.Argo, metrics *prometheus.Metrics, updater *argocd.ArgoStatusUpdater, deployLockStore lock.DeployLockStore, appTokenStore apptoken.Store) (*Env, error) {
	var env *Env
	var err error

	env = &Env{
		config:     serverConfig,
		argo:       argo,
		metrics:    metrics,
		updater:    updater,
		shutdownCh: make(chan struct{}),
	}

	if env.lockdown, err = NewLockdown(serverConfig.LockdownSchedule, deployLockStore); err != nil {
		return nil, err
	}

	env.strategies = map[string]auth.AuthStrategy{
		"ARGO_WATCHER_DEPLOY_TOKEN": auth.NewDeployTokenAuthService(env.config.DeployToken),
	}

	if env.config.OIDC.Enabled {
		oidcService, oidcErr := auth.NewOIDCAuthService(env.config)
		if oidcErr != nil {
			return nil, fmt.Errorf("failed to initialize OIDC auth: %w", oidcErr)
		}
		env.strategies[oidcHeader] = oidcService
	}

	// Application deploy tokens and CI JWTs share the Authorization header, so one
	// router owns it and dispatches on the token's own shape.
	var jwtStrategy auth.AuthStrategy
	if env.config.JWTSecret != "" {
		jwtStrategy = auth.NewJWTAuthService(env.config.JWTSecret, env.config.JWTIssuer, env.config.JWTAudience)
	}

	// A non-nil store is what CreateRouter registers the token endpoints on, so the
	// assignment stays here rather than inside the resolver.
	appTokenStrategy, appTokens := env.resolveAppTokens(appTokenStore)
	env.appTokens = appTokens
	env.strategies["Authorization"] = auth.NewPrefixRouter(appTokenStrategy, jwtStrategy)

	env.authenticator = auth.NewAuthenticator(env.strategies)

	return env, nil
}

// resolveAppTokens returns the strategy for application deploy tokens and the store
// to serve them from, which is nil unless both preconditions hold. When either is
// missing the tokens are refused by name rather than ignored, so a pipeline holding
// one is told why instead of having its write-back silently skipped.
func (env *Env) resolveAppTokens(store apptoken.Store) (auth.AuthStrategy, apptoken.Store) {
	switch {
	case store == nil:
		slog.Warn("Application deploy tokens are unavailable: they require STATE_TYPE=postgres.")
	case !env.config.OIDC.Enabled:
		slog.Warn("Application deploy tokens are unavailable: they require OIDC_ENABLED, the only way to issue and revoke them.")
	default:
		return auth.NewAppTokenAuthService(store), store
	}

	return auth.DisabledAppTokenStrategy(), nil
}
