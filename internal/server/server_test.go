package server

import (
	"net/url"
	"testing"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer_Success(t *testing.T) {
	// Arrange: Create a valid configuration struct directly.
	argoURL, err := url.Parse("https://argo.example.com")
	require.NoError(t, err)

	cfg := &config.ServerConfig{
		ArgoUrl:   *argoURL,
		ArgoToken: "test-token",
		StateType: "in-memory",
	}

	// Act
	s, err := NewServer(cfg)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, s)
	assert.Equal(t, cfg, s.config)
}

func TestNewServer_StateInitFailure(t *testing.T) {
	// This test covers the failure path when the StateType is invalid.
	argoURL, err := url.Parse("https://argo.example.com")
	require.NoError(t, err)

	cfg := &config.ServerConfig{
		ArgoUrl:   *argoURL,
		ArgoToken: "test-token",
		StateType: "invalid-state-type",
	}

	_, err = NewServer(cfg)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected state type received: invalid-state-type")
}

func TestNewServer_PostgresConnectionFailure(t *testing.T) {
	// Arrange: Configure a postgres state type but don't provide a DSN.
	// This will reliably cause state.NewState() to fail.
	argoURL, err := url.Parse("https://argo.example.com")
	require.NoError(t, err)

	// Explicitly unset the DSN to ensure the connection will fail.
	t.Setenv("DB_DSN", "")

	cfg := &config.ServerConfig{
		ArgoUrl:   *argoURL,
		ArgoToken: "test-token",
		StateType: "postgres",
	}

	// Act
	_, err = NewServer(cfg)

	// Assert
	assert.Error(t, err)
	// Expect the broader error message from the underlying driver.
	assert.Contains(t, err.Error(), "failed to connect to")
}

func TestInitLogs(t *testing.T) {
	// This function only logs, so we just verify it runs without panicking.
	assert.NotPanics(t, func() {
		initLogs("debug")
	})
	assert.NotPanics(t, func() {
		initLogs("invalid-level")
	})
}
