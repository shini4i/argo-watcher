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

	t.Run("wrong token of the same length", func(t *testing.T) {
		// The only input that exercises the comparison itself: a length mismatch is
		// refused before a single byte is compared.
		service := NewDeployTokenAuthService("valid_token")
		isValid, err := service.Validate("valid_tokeX")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid")
		assert.False(t, isValid)
	})

	t.Run("invalid token", func(t *testing.T) {
		service := NewDeployTokenAuthService("valid_token")
		_, err := service.Validate("invalid_token")

		assert.Error(t, err)
		// The strategy is only ever invoked with a non-empty token, so
		// "missing or invalid" would be misleading wording.
		assert.Contains(t, err.Error(), "invalid")
		assert.NotContains(t, err.Error(), "missing")
	})
}

// TestDeployTokenNeverAuthorizesAnEmptySecret pins the guard against a misconfigured
// service: with no deploy token set, a constant-time compare of two empty strings
// matches, so an empty credential would authorize a deployment.
func TestDeployTokenNeverAuthorizesAnEmptySecret(t *testing.T) {
	t.Run("unset token rejects an empty credential", func(t *testing.T) {
		service := NewDeployTokenAuthService("")
		isValid, err := service.Validate("")

		assert.Error(t, err)
		assert.False(t, isValid)
	})

	t.Run("unset token rejects any credential", func(t *testing.T) {
		service := NewDeployTokenAuthService("")
		isValid, err := service.Validate("guess")

		assert.Error(t, err)
		assert.False(t, isValid)
	})

	t.Run("configured token rejects an empty credential", func(t *testing.T) {
		service := NewDeployTokenAuthService("valid_token")
		isValid, err := service.Validate("")

		assert.Error(t, err)
		assert.False(t, isValid)
	})
}
