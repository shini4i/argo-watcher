package server

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/shini4i/argo-watcher/internal/config"
)

// Giving a rollout up at shutdown only helps when something can pick it up. With
// in-memory state nothing can, so the deployment — its git write-back included —
// would be dropped rather than handed on.
func TestHandsOverOnShutdown(t *testing.T) {
	tests := []struct {
		name      string
		stateType string
		draining  bool
		want      bool
	}{
		{name: "shared state, shutting down", stateType: "postgres", draining: true, want: true},
		{name: "shared state, still serving", stateType: "postgres"},
		{name: "in-memory state, shutting down", stateType: "in-memory", draining: true},
		{name: "in-memory state, still serving", stateType: "in-memory"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := &Env{config: &config.ServerConfig{StateType: tt.stateType}}
			if tt.draining {
				env.beginDraining()
			}

			assert.Equal(t, tt.want, env.handsOverOnShutdown())
		})
	}
}
