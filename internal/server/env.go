package server

import (
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/prometheus"
	"github.com/shini4i/argo-watcher/internal/argocd"
	"github.com/shini4i/argo-watcher/internal/auth"
)

// Env reference: https://www.alexedwards.net/blog/organising-database-access
type Env struct {
	// environment configurations
	config *config.ServerConfig
	// argo client
	argo *argocd.Argo
	// argo updater
	updater Updater
	// metrics
	metrics *prometheus.Metrics
	// deploy lock
	lockdown *Lockdown
	// enabled auth strategies
	strategies map[string]auth.AuthStrategy
	// authenticator orchestrates registered strategies
	authenticator *auth.Authenticator
}
