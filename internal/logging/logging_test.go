package logging

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInit(t *testing.T) {
	// A valid level is parsed and applied to the shared levelVar.
	Init("debug")
	assert.Equal(t, slog.LevelDebug, levelVar.Level())

	Init("warn")
	assert.Equal(t, slog.LevelWarn, levelVar.Level())

	// An unparseable level is a no-op on the level: it logs a warning and
	// leaves the previously configured level (warn) untouched.
	Init("invalid-level")
	assert.Equal(t, slog.LevelWarn, levelVar.Level())
}

func TestParseLevel(t *testing.T) {
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
			got, err := parseLevel(tt.input)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
