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
	// environment configurations
	config *config.ServerConfig
	// argo argo
	argo *argocd.Argo
	// argo updater
	updater *argocd.ArgoStatusUpdater
	// metrics
	metrics *prometheus.Metrics
	// deploy lock
	lockdown *Lockdown
	// enabled auth strategies
	strategies map[string]auth.AuthStrategy
	// authenticator orchestrates registered strategies
	authenticator *auth.Authenticator
	// shutdownCh signals graceful shutdown to all WebSocket goroutines.
	// Using a channel instead of storing context.Context follows Go best practices.
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

// NewEnv initializes a new Env instance.
// This function is used to set up the environment for the application's main operation, including setting configurations, initializing Argo service, and metrics.
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
