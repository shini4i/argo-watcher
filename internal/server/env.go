package server

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/shini4i/argo-watcher/internal/argocd"
	"github.com/shini4i/argo-watcher/internal/auth"
	"github.com/shini4i/argo-watcher/internal/config"
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
	// shutdownCh is closed to signal graceful shutdown to all WebSocket goroutines.
	shutdownCh chan struct{}
	// shutdownOnce ensures Shutdown() can be called multiple times safely.
	shutdownOnce sync.Once
	// connWg tracks active WebSocket connection goroutines for graceful shutdown.
	connWg sync.WaitGroup
}

// lockdownPollInterval is how often the scheduled-lockdown watcher re-evaluates
// the lock state to detect schedule boundary transitions. Schedules have
// minute granularity, so a one-minute tick bounds the notification lag.
const lockdownPollInterval = time.Minute

// StartLockdownWatcher launches a background goroutine that notifies WebSocket
// clients when a scheduled lockdown window automatically begins or ends. It is a
// no-op when no schedules are configured, since manual set/release already
// notify clients directly. The goroutine is tracked by connWg and stops when the
// shutdown channel is closed.
func (env *Env) StartLockdownWatcher() {
	if len(env.lockdown.Schedules) == 0 {
		return
	}

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

// argoStatusMessage builds the WebSocket payload for a reachability reason:
// argoUpMessage when everything is reachable, otherwise "argocd_down:<reason>"
// so clients learn which subsystem is down (see argocd.Reason* constants).
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

const shutdownTimeout = 10 * time.Second // Maximum time to wait for WebSocket goroutines during shutdown

// Shutdown gracefully shuts down the server and all WebSocket connections.
// This method is safe to call multiple times. It blocks until all WebSocket
// goroutines have finished or the shutdown timeout is reached. If the timeout
// is reached, some goroutines may still be running but should exit shortly as
// they observe the closed shutdownCh. Any long-running WebSocket writes are
// bounded by their own 5-second timeout in checkConnection.
func (env *Env) Shutdown() {
	if env.shutdownCh != nil {
		env.shutdownOnce.Do(func() {
			close(env.shutdownCh)
		})
	}

	// Wait for all WebSocket connection goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		env.connWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.Debug("All WebSocket connections closed gracefully")
	case <-time.After(shutdownTimeout):
		slog.Warn("Shutdown timeout reached, some WebSocket goroutines may still be running")
	}
}

// NewEnv wires up an Env from the server config: lockdown schedules and the
// enabled auth strategies (deploy token, optional Keycloak, optional JWT).
func NewEnv(serverConfig *config.ServerConfig, argo *argocd.Argo, metrics *prometheus.Metrics, updater *argocd.ArgoStatusUpdater) (*Env, error) {
	var env *Env
	var err error

	env = &Env{
		config:     serverConfig,
		argo:       argo,
		metrics:    metrics,
		updater:    updater,
		shutdownCh: make(chan struct{}),
	}

	if env.lockdown, err = NewLockdown(serverConfig.LockdownSchedule); err != nil {
		return nil, err
	}

	env.strategies = map[string]auth.AuthStrategy{
		"ARGO_WATCHER_DEPLOY_TOKEN": auth.NewDeployTokenAuthService(env.config.DeployToken),
	}

	if env.config.Keycloak.Enabled {
		keycloakService, keycloakErr := auth.NewKeycloakAuthService(env.config)
		if keycloakErr != nil {
			return nil, fmt.Errorf("failed to initialize keycloak auth: %w", keycloakErr)
		}
		env.strategies[keycloakHeader] = keycloakService
	}

	if env.config.JWTSecret != "" {
		env.strategies["Authorization"] = auth.NewJWTAuthService(env.config.JWTSecret)
	}

	env.authenticator = auth.NewAuthenticator(env.strategies)

	return env, nil
}
