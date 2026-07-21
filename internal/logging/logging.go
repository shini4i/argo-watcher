// Package logging centralizes configuration of the global slog logger so every
// entrypoint (the server and the --migrate CLI path) emits structured JSON logs
// at a consistent, LOG_LEVEL-driven level.
package logging

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

var levelVar = new(slog.LevelVar)

// Init configures the global slog logger to emit JSON to stderr at the level
// parsed from level. An unparseable level is logged as a warning and leaves the
// logger at its default (info) level.
func Init(level string) {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: levelVar})))
	if lvl, err := parseLevel(level); err != nil {
		slog.Warn("couldn't parse log level, using default", "error", err)
	} else {
		levelVar.Set(lvl)
		slog.Debug("configured log level", "level", lvl)
	}
}

// parseLevel maps a textual log level to its slog equivalent. It accepts the
// level names previously handled by zerolog (including the trace/fatal/panic
// aliases) so existing LOG_LEVEL values keep working; unknown values error.
func parseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "trace", "debug":
		return slog.LevelDebug, nil
	case "", "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error", "fatal", "panic":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown log level %q", level)
	}
}
