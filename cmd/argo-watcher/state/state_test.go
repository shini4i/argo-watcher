package state

import (
	"testing"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/stretchr/testify/assert"
)

func TestNewState_success(t *testing.T) {
	// Create a ServerConfig instance with a specific StateType value
	cfg := &config.ServerConfig{
		StateType: "in-memory",
	}

	// Call the NewState function
	state, err := NewState(cfg)

	// Assert that the state is not nil
	assert.NotNil(t, state)

	// Assert that the error is nil
	assert.Nil(t, err)
}

func TestNewState_fail(t *testing.T) {
	// Create a ServerConfig instance with a specific StateType value
	cfg := &config.ServerConfig{
		StateType: "non-existing-state",
	}

	// Call the NewState function
	state, err := NewState(cfg)

	// Assert that the state is nil
	assert.Nil(t, state)

	// Assert that the error is not nil
	assert.NotNil(t, err)
}
