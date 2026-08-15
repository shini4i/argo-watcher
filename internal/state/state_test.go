package state

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/shini4i/argo-watcher/internal/config"
)

func TestNewState_success(t *testing.T) {
	cfg := &config.ServerConfig{
		StateType: "in-memory",
	}

	state, err := NewState(cfg)

	assert.NotNil(t, state)

	assert.Nil(t, err)
}

func TestNewState_fail(t *testing.T) {
	cfg := &config.ServerConfig{
		StateType: "non-existing-state",
	}

	state, err := NewState(cfg)

	assert.Nil(t, state)

	assert.NotNil(t, err)
}
