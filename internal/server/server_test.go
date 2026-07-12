package server

import (
	"log/slog"
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
	// A valid level is parsed and applied to the shared logLevelVar.
	initLogs("debug")
	assert.Equal(t, slog.LevelDebug, logLevelVar.Level())

	initLogs("warn")
	assert.Equal(t, slog.LevelWarn, logLevelVar.Level())

	// An unparseable level is a no-op on the level: it logs a warning and
	// leaves the previously configured level (warn) untouched.
	initLogs("invalid-level")
	assert.Equal(t, slog.LevelWarn, logLevelVar.Level())
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input     string
		want      slog.Level
		expectErr bool
	}{
		{"trace", slog.LevelDebug, false},
		{"debug", slog.LevelDebug, false},
		{"info", slog.LevelInfo, false},
		{"", slog.LevelInfo, false},
		{"WARN", slog.LevelWarn, false},
		{"warning", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"fatal", slog.LevelError, false},
		{"panic", slog.LevelError, false},
		{" Info ", slog.LevelInfo, false},
		{"bogus", slog.LevelInfo, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseLogLevel(tt.input)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
