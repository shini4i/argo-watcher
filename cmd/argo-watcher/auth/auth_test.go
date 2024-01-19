package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A super simple test to check if NewExternalAuthService returns a KeycloakAuthService
// We have it just not to lose test coverage percentage at the moment
func TestNewExternalAuthService(t *testing.T) {
	service := NewExternalAuthService()
	assert.IsType(t, &KeycloakAuthService{}, service)
}
