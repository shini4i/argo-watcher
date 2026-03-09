package auth

import (
	"errors"
	"net/http"
	"testing"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/stretchr/testify/assert"
)

func TestNewKeycloakAuthService(t *testing.T) {
	t.Run("should initialize with valid config", func(t *testing.T) {
		conf := &config.ServerConfig{
			Keycloak: config.KeycloakConfig{
				Url:              "http://localhost:8080",
				Realm:            "master",
				ClientId:         "test",
				PrivilegedGroups: []string{"group1", "group2"},
			},
		}

		keycloakAuthService, err := NewKeycloakAuthService(conf)

		assert.NoError(t, err)
		assert.Equal(t, keycloakAuthService.Url, conf.Keycloak.Url)
		assert.Equal(t, keycloakAuthService.Realm, conf.Keycloak.Realm)
		assert.Equal(t, keycloakAuthService.ClientId, conf.Keycloak.ClientId)
		assert.Equal(t, keycloakAuthService.PrivilegedGroups, conf.Keycloak.PrivilegedGroups)
	})

	t.Run("should return error for nil config", func(t *testing.T) {
		keycloakAuthService, err := NewKeycloakAuthService(nil)

		assert.Error(t, err)
		assert.Nil(t, keycloakAuthService)
		assert.Contains(t, err.Error(), "server config must not be nil")
	})

	t.Run("should return error for invalid URL", func(t *testing.T) {
		conf := &config.ServerConfig{
			Keycloak: config.KeycloakConfig{
				Url:   "://invalid",
				Realm: "master",
			},
		}

		keycloakAuthService, err := NewKeycloakAuthService(conf)

		assert.Error(t, err)
		assert.Nil(t, keycloakAuthService)
	})
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

func TestAuthenticatorValidateStrategy(t *testing.T) {
	t.Run("returns false when authenticator is nil", func(t *testing.T) {
		var authenticator *Authenticator
		request, err := http.NewRequest(http.MethodGet, "http://example.com", http.NoBody)
		assert.NoError(t, err)

		valid, validateErr := authenticator.ValidateStrategy(request, "Authorization")
		assert.False(t, valid)
		assert.NoError(t, validateErr)
	})

	t.Run("returns false when request is nil", func(t *testing.T) {
		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"Authorization": &stubStrategy{valid: true},
		})

		valid, validateErr := authenticator.ValidateStrategy(nil, "Authorization")
		assert.False(t, valid)
		assert.NoError(t, validateErr)
	})

	t.Run("returns false when strategy not found", func(t *testing.T) {
		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"Authorization": &stubStrategy{valid: true},
		})

		request, err := http.NewRequest(http.MethodGet, "http://example.com", http.NoBody)
		assert.NoError(t, err)
		request.Header.Set("Keycloak-Authorization", "token")

		valid, validateErr := authenticator.ValidateStrategy(request, "Keycloak-Authorization")
		assert.False(t, valid)
		assert.NoError(t, validateErr)
	})

	t.Run("returns false when token is empty", func(t *testing.T) {
		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"Authorization": &stubStrategy{valid: true},
		})

		request, err := http.NewRequest(http.MethodGet, "http://example.com", http.NoBody)
		assert.NoError(t, err)
		// Do not set the Authorization header

		valid, validateErr := authenticator.ValidateStrategy(request, "Authorization")
		assert.False(t, valid)
		assert.NoError(t, validateErr)
	})

	t.Run("validates with matching strategy and valid token", func(t *testing.T) {
		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"Authorization":             &stubStrategy{expectedToken: "jwt-token", valid: true},
			"ARGO_WATCHER_DEPLOY_TOKEN": &stubStrategy{expectedToken: "deploy-token", valid: true},
		})

		request, err := http.NewRequest(http.MethodGet, "http://example.com", http.NoBody)
		assert.NoError(t, err)
		request.Header.Set("Authorization", "jwt-token")

		valid, validateErr := authenticator.ValidateStrategy(request, "Authorization")
		assert.True(t, valid)
		assert.NoError(t, validateErr)
	})

	t.Run("strips Bearer prefix before validation", func(t *testing.T) {
		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"Authorization": &stubStrategy{expectedToken: "actual-token", valid: true},
		})

		request, err := http.NewRequest(http.MethodGet, "http://example.com", http.NoBody)
		assert.NoError(t, err)
		request.Header.Set("Authorization", "Bearer actual-token")

		valid, validateErr := authenticator.ValidateStrategy(request, "Authorization")
		assert.True(t, valid)
		assert.NoError(t, validateErr)
	})

	t.Run("returns false when token is only Bearer prefix", func(t *testing.T) {
		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"Authorization": &stubStrategy{valid: true},
		})

		request, err := http.NewRequest(http.MethodGet, "http://example.com", http.NoBody)
		assert.NoError(t, err)
		request.Header.Set("Authorization", "Bearer ")

		valid, validateErr := authenticator.ValidateStrategy(request, "Authorization")
		assert.False(t, valid)
		assert.NoError(t, validateErr)
	})

	t.Run("returns error from strategy", func(t *testing.T) {
		expectedErr := errors.New("token expired")
		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"Authorization": &stubStrategy{err: expectedErr},
		})

		request, err := http.NewRequest(http.MethodGet, "http://example.com", http.NoBody)
		assert.NoError(t, err)
		request.Header.Set("Authorization", "expired-token")

		valid, validateErr := authenticator.ValidateStrategy(request, "Authorization")
		assert.False(t, valid)
		assert.Equal(t, expectedErr, validateErr)
	})

	t.Run("ignores other registered strategies", func(t *testing.T) {
		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"Authorization":             &stubStrategy{expectedToken: "jwt-token", valid: true},
			"ARGO_WATCHER_DEPLOY_TOKEN": &stubStrategy{expectedToken: "deploy-token", valid: true},
		})

		request, err := http.NewRequest(http.MethodGet, "http://example.com", http.NoBody)
		assert.NoError(t, err)
		// Set both headers, but only ARGO_WATCHER_DEPLOY_TOKEN should be evaluated
		request.Header.Set("Authorization", "jwt-token")
		request.Header.Set("ARGO_WATCHER_DEPLOY_TOKEN", "deploy-token")

		valid, validateErr := authenticator.ValidateStrategy(request, "ARGO_WATCHER_DEPLOY_TOKEN")
		assert.True(t, valid)
		assert.NoError(t, validateErr)
	})
}

func TestAuthenticatorValidateBearerOnlyPrefix(t *testing.T) {
	authenticator := NewAuthenticator(map[string]AuthStrategy{
		"Authorization": &stubStrategy{valid: true},
	})

	request, err := http.NewRequest(http.MethodGet, "http://example.com", http.NoBody)
	assert.NoError(t, err)
	request.Header.Set("Authorization", "Bearer ")

	valid, validateErr := authenticator.Validate(request)
	assert.False(t, valid)
	assert.NoError(t, validateErr)
}

func TestNewAuthenticatorSkipsNilStrategies(t *testing.T) {
	authenticator := NewAuthenticator(map[string]AuthStrategy{
		"Authorization":             nil,
		"ARGO_WATCHER_DEPLOY_TOKEN": NewDeployTokenAuthService("token"),
	})

	// The nil strategy should have been filtered out
	_, found := authenticator.Strategy("Authorization")
	assert.False(t, found)

	// The non-nil strategy should be present
	_, found = authenticator.Strategy("ARGO_WATCHER_DEPLOY_TOKEN")
	assert.True(t, found)
}

func TestAuthenticatorValidateNilReceiver(t *testing.T) {
	var authenticator *Authenticator
	request, err := http.NewRequest(http.MethodGet, "http://example.com", http.NoBody)
	assert.NoError(t, err)

	valid, validateErr := authenticator.Validate(request)
	assert.False(t, valid)
	assert.NoError(t, validateErr)
}

func TestAuthenticatorValidateNilRequest(t *testing.T) {
	authenticator := NewAuthenticator(map[string]AuthStrategy{
		"Authorization": &stubStrategy{valid: true},
	})

	valid, validateErr := authenticator.Validate(nil)
	assert.False(t, valid)
	assert.NoError(t, validateErr)
}

func TestAuthenticatorStrategyNilReceiver(t *testing.T) {
	var authenticator *Authenticator

	strategy, ok := authenticator.Strategy("Authorization")
	assert.Nil(t, strategy)
	assert.False(t, ok)
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
