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
	})
}
