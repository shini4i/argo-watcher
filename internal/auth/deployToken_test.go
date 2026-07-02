package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateDeployToken(t *testing.T) {
	t.Run("valid token", func(t *testing.T) {
		service := NewDeployTokenAuthService("valid_token")
		isValid, err := service.Validate("valid_token")

		assert.NoError(t, err)
		assert.True(t, isValid)
	})

	t.Run("invalid token", func(t *testing.T) {
		service := NewDeployTokenAuthService("valid_token")
		_, err := service.Validate("invalid_token")

		assert.Error(t, err)
		// The wording must reflect what actually happened: the strategy is
		// only ever invoked with a non-empty token, so "missing or invalid"
		// is misleading. Should clearly say the token is invalid.
		assert.Contains(t, err.Error(), "invalid")
		assert.NotContains(t, err.Error(), "missing")
	})
}
