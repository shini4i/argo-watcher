package server

import (
	"net/url"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer_Success(t *testing.T) {
	// Arrange
	reg := prometheus.NewRegistry()
	argoURL, err := url.Parse("https://argo.example.com")
	require.NoError(t, err)

	cfg := &config.ServerConfig{
		ArgoUrl:   *argoURL,
		ArgoToken: "test-token",
		StateType: "in-memory",
	}

	// Act
	s, err := NewServer(cfg, reg)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, s)
	assert.Equal(t, cfg, s.config)
}

func TestNewServer_StateInitFailure(t *testing.T) {
	// Arrange
	reg := prometheus.NewRegistry()
	argoURL, err := url.Parse("https://argo.example.com")
	require.NoError(t, err)

	cfg := &config.ServerConfig{
		ArgoUrl:   *argoURL,
		ArgoToken: "test-token",
		StateType: "invalid-state-type",
	}

	// Act
	_, err = NewServer(cfg, reg)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected state type received: invalid-state-type")
}

func TestNewServer_PostgresConnectionFailure(t *testing.T) {
	// Arrange
	reg := prometheus.NewRegistry()
	argoURL, err := url.Parse("https://argo.example.com")
	require.NoError(t, err)

	t.Setenv("DB_DSN", "")

	cfg := &config.ServerConfig{
		ArgoUrl:   *argoURL,
		ArgoToken: "test-token",
		StateType: "postgres",
	}

	// Act
	_, err = NewServer(cfg, reg)

	// Assert
	assert.Error(t, err)
	// This is the corrected, more robust assertion.
	assert.Contains(t, err.Error(), "failed to connect to")
}

func TestInitLogs(t *testing.T) {
	assert.NotPanics(t, func() {
		initLogs("debug")
	})
	assert.NotPanics(t, func() {
		initLogs("invalid-level")
	})
}
