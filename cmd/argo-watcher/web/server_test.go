package web

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestServer_initLogs_correct(t *testing.T) {
	// Invoke the function being tested
	initLogs("fatal", "json")

	// Assert that the global log level is set to the expected value
	assert.Equal(t, zerolog.FatalLevel, zerolog.GlobalLevel())

	// Cleanup - reset the global log level to the default value
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
}

func TestServer_initLogs_invalid(t *testing.T) {
	// Invoke the function being tested
	initLogs("invalid", "json")

	// Assert that the global log level is set to info level when the log level is invalid
	assert.Equal(t, zerolog.InfoLevel, zerolog.GlobalLevel())

	// Cleanup - reset the global log level to the default value
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
}
