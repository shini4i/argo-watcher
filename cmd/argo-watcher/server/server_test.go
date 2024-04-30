package server

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestServer_initLogs_correct(t *testing.T) {
	// Invoke the function being tested
	initLogs("fatal")

	// Assert that the global log level is set to the expected value
	assert.Equal(t, zerolog.FatalLevel, zerolog.GlobalLevel())

	// Cleanup - reset the global log level to the default value
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
}
