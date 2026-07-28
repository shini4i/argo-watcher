package server

import (
	"net/url"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/config"
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

// TestShutdownBudgetFitsGracePeriod guards the invariant that makes the batch
// write-back drain reachable. The shutdown phases run in sequence, so the earlier
// two caps must leave the last phase a USABLE share, not merely fit inside the
// budget: "sum < budget" alone would still pass with a 1s remainder, which is the
// starvation this bound exists to prevent.
//
// The 5s floor is not enough for a fresh git attempt (GIT_OP_TIMEOUT defaults to
// 90s) — nothing inside a 25s budget could be. It is sized for what the drain can
// actually accomplish: letting a retry loop observe the drain signal, resolve its
// batch, and deliver every result so no deploying goroutine is left blocked.
func TestShutdownBudgetFitsGracePeriod(t *testing.T) {
	assert.Less(t, shutdownBudget, 30*time.Second, "budget must fit the default pod grace period")

	writebackDrainShare := shutdownBudget - (httpDrainBudget + shutdownTimeout)
	assert.GreaterOrEqual(t, writebackDrainShare, 5*time.Second,
		"HTTP and WebSocket drains must leave the batch write-back drain a usable share")
}
