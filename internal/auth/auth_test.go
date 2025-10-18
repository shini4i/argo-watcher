package auth

import (
	"errors"
	"net/http"
	"testing"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/stretchr/testify/assert"
)

func TestNewKeycloakAuthService(t *testing.T) {
	conf := &config.ServerConfig{
		Keycloak: config.KeycloakConfig{
			Url:              "http://localhost:8080",
			Realm:            "master",
			ClientId:         "test",
			PrivilegedGroups: []string{"group1", "group2"},
		},
	}

	keycloakAuthService := NewKeycloakAuthService(conf)

	assert.Equal(t, keycloakAuthService.Url, conf.Keycloak.Url)
	assert.Equal(t, keycloakAuthService.Realm, conf.Keycloak.Realm)
	assert.Equal(t, keycloakAuthService.ClientId, conf.Keycloak.ClientId)
	assert.Equal(t, keycloakAuthService.PrivilegedGroups, conf.Keycloak.PrivilegedGroups)
}

func TestNewJWTAuthService(t *testing.T) {
	secret := "testSecret"
	jwtAuthService := NewJWTAuthService(secret)
	assert.Equal(t, jwtAuthService.secretKey, []byte(secret))
}

func TestAuthenticatorValidate(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.com", http.NoBody)
	assert.NoError(t, err)

	request.Header.Set("ARGO_WATCHER_DEPLOY_TOKEN", "valid")

	authenticator := NewAuthenticator(map[string]AuthStrategy{
		"ARGO_WATCHER_DEPLOY_TOKEN": NewDeployTokenAuthService("valid"),
	})

	valid, validateErr := authenticator.Validate(request)

	assert.True(t, valid)
	assert.NoError(t, validateErr)
}

func TestAuthenticatorValidateWithBearerPrefix(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.com", http.NoBody)
	assert.NoError(t, err)

	request.Header.Set("Authorization", "Bearer trimmed-token")

	authenticator := NewAuthenticator(map[string]AuthStrategy{
		"Authorization": &stubStrategy{
			expectedToken: "trimmed-token",
			valid:         true,
		},
	})

	valid, validateErr := authenticator.Validate(request)

	assert.True(t, valid)
	assert.NoError(t, validateErr)
}

func TestAuthenticatorValidateReturnsLastError(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.com", http.NoBody)
	assert.NoError(t, err)

	request.Header.Set("Authorization", "token")

	authenticator := NewAuthenticator(map[string]AuthStrategy{
		"Authorization": &stubStrategy{
			expectedToken: "token",
			err:           errors.New("strategy error"),
		},
	})

	valid, validateErr := authenticator.Validate(request)

	assert.False(t, valid)
	assert.EqualError(t, validateErr, "strategy error")
}

func TestAuthenticatorStrategyLookup(t *testing.T) {
	strategy := NewDeployTokenAuthService("valid")
	authenticator := NewAuthenticator(map[string]AuthStrategy{
		"ARGO_WATCHER_DEPLOY_TOKEN": strategy,
	})

	resolved, ok := authenticator.Strategy("ARGO_WATCHER_DEPLOY_TOKEN")
	assert.True(t, ok)
	assert.Equal(t, strategy, resolved)

	resolved, ok = authenticator.Strategy("missing")
	assert.False(t, ok)
	assert.Nil(t, resolved)
}

type stubStrategy struct {
	expectedToken string
	valid         bool
	err           error
}

func (s *stubStrategy) Validate(token string) (bool, error) {
	if s.expectedToken != "" && token != s.expectedToken {
		return false, errors.New("token mismatch")
	}

	if s.err != nil {
		return false, s.err
	}

	return s.valid, nil
}
