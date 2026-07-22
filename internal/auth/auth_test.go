package auth

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/mocks"
)

// acceptAnyToken returns a MockAuthStrategy that validates any token. It is used
// in tests where the strategy may never be reached (nil request, empty token,
// unmatched header); AnyTimes permits zero calls.
func acceptAnyToken(t *testing.T) *mocks.MockAuthStrategy {
	t.Helper()
	m := mocks.NewMockAuthStrategy(gomock.NewController(t))
	m.EXPECT().Validate(gomock.Any()).Return(true, nil).AnyTimes()
	return m
}

func TestNewOIDCAuthService(t *testing.T) {
	t.Run("should initialize with valid config", func(t *testing.T) {
		conf := &config.ServerConfig{
			OIDC: config.OIDCConfig{
				IssuerURL:        "http://localhost:8080/realms/master",
				ClientId:         "test",
				PrivilegedGroups: []string{"group1", "group2"},
			},
		}

		oidcAuthService, err := NewOIDCAuthService(conf)

		assert.NoError(t, err)
		assert.Equal(t, oidcAuthService.IssuerURL, conf.OIDC.IssuerURL)
		assert.Equal(t, oidcAuthService.ClientId, conf.OIDC.ClientId)
		assert.Equal(t, oidcAuthService.PrivilegedGroups, conf.OIDC.PrivilegedGroups)
	})

	t.Run("should return error for nil config", func(t *testing.T) {
		oidcAuthService, err := NewOIDCAuthService(nil)

		assert.Error(t, err)
		assert.Nil(t, oidcAuthService)
		assert.Contains(t, err.Error(), "server config must not be nil")
	})

	t.Run("should return error for invalid URL", func(t *testing.T) {
		conf := &config.ServerConfig{
			OIDC: config.OIDCConfig{
				IssuerURL: "://invalid",
			},
		}

		oidcAuthService, err := NewOIDCAuthService(conf)

		assert.Error(t, err)
		assert.Nil(t, oidcAuthService)
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

	// The "Bearer " prefix must be stripped before the strategy sees the token.
	strategy := mocks.NewMockAuthStrategy(gomock.NewController(t))
	strategy.EXPECT().Validate("trimmed-token").Return(true, nil).AnyTimes()

	authenticator := NewAuthenticator(map[string]AuthStrategy{
		"Authorization": strategy,
	})

	valid, validateErr := authenticator.Validate(request)

	assert.True(t, valid)
	assert.NoError(t, validateErr)
}

func TestAuthenticatorValidateReturnsLastError(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://example.com", http.NoBody)
	assert.NoError(t, err)

	request.Header.Set("Authorization", "token")

	strategyErr := errors.New("strategy error")
	strategy := mocks.NewMockAuthStrategy(gomock.NewController(t))
	strategy.EXPECT().Validate("token").Return(false, strategyErr).AnyTimes()

	authenticator := NewAuthenticator(map[string]AuthStrategy{
		"Authorization": strategy,
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
			"Authorization": acceptAnyToken(t),
		})

		valid, validateErr := authenticator.ValidateStrategy(nil, "Authorization")
		assert.False(t, valid)
		assert.NoError(t, validateErr)
	})

	t.Run("returns false when strategy not found", func(t *testing.T) {
		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"Authorization": acceptAnyToken(t),
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
			"Authorization": acceptAnyToken(t),
		})

		request, err := http.NewRequest(http.MethodGet, "http://example.com", http.NoBody)
		assert.NoError(t, err)
		// Do not set the Authorization header

		valid, validateErr := authenticator.ValidateStrategy(request, "Authorization")
		assert.False(t, valid)
		assert.NoError(t, validateErr)
	})

	t.Run("validates with matching strategy and valid token", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		authStrategy := mocks.NewMockAuthStrategy(ctrl)
		authStrategy.EXPECT().Validate("jwt-token").Return(true, nil).AnyTimes()
		// The deploy-token strategy is registered but targeting "Authorization"
		// must not reach it; a bare mock (no expectation) fails if it is called.
		deployStrategy := mocks.NewMockAuthStrategy(ctrl)

		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"Authorization":             authStrategy,
			"ARGO_WATCHER_DEPLOY_TOKEN": deployStrategy,
		})

		request, err := http.NewRequest(http.MethodGet, "http://example.com", http.NoBody)
		assert.NoError(t, err)
		request.Header.Set("Authorization", "jwt-token")

		valid, validateErr := authenticator.ValidateStrategy(request, "Authorization")
		assert.True(t, valid)
		assert.NoError(t, validateErr)
	})

	t.Run("strips Bearer prefix before validation", func(t *testing.T) {
		strategy := mocks.NewMockAuthStrategy(gomock.NewController(t))
		strategy.EXPECT().Validate("actual-token").Return(true, nil).AnyTimes()

		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"Authorization": strategy,
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
			"Authorization": acceptAnyToken(t),
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
		strategy := mocks.NewMockAuthStrategy(gomock.NewController(t))
		strategy.EXPECT().Validate(gomock.Any()).Return(false, expectedErr).AnyTimes()

		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"Authorization": strategy,
		})

		request, err := http.NewRequest(http.MethodGet, "http://example.com", http.NoBody)
		assert.NoError(t, err)
		request.Header.Set("Authorization", "expired-token")

		valid, validateErr := authenticator.ValidateStrategy(request, "Authorization")
		assert.False(t, valid)
		assert.Equal(t, expectedErr, validateErr)
	})

	t.Run("ignores other registered strategies", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		// Targeting ARGO_WATCHER_DEPLOY_TOKEN must not evaluate the Authorization
		// strategy; a bare mock (no expectation) fails if it is called.
		authStrategy := mocks.NewMockAuthStrategy(ctrl)
		deployStrategy := mocks.NewMockAuthStrategy(ctrl)
		deployStrategy.EXPECT().Validate("deploy-token").Return(true, nil).AnyTimes()

		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"Authorization":             authStrategy,
			"ARGO_WATCHER_DEPLOY_TOKEN": deployStrategy,
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
		"Authorization": acceptAnyToken(t),
	})

	request, err := http.NewRequest(http.MethodGet, "http://example.com", http.NoBody)
	assert.NoError(t, err)
	request.Header.Set("Authorization", "Bearer ")

	valid, validateErr := authenticator.Validate(request)
	assert.False(t, valid)
	assert.NoError(t, validateErr)
}

// TestAuthenticatorValidateJWTWithAndWithoutBearerPrefix proves that the
// "Bearer " prefix is optional: a raw JWT on the Authorization header
// validates identically to a "Bearer <jwt>" value. The raw form is what makes
// the token maskable as a GitLab CI variable (no space in the value).
func TestAuthenticatorValidateJWTWithAndWithoutBearerPrefix(t *testing.T) {
	secret := "test_secret_key"
	claims := jwt.MapClaims{"exp": float64(time.Now().Add(time.Hour).Unix())}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	assert.NoError(t, err)

	cases := map[string]string{
		"raw JWT (maskable)":       signed,
		"Bearer-prefixed (legacy)": "Bearer " + signed,
	}

	for name, headerValue := range cases {
		t.Run(name, func(t *testing.T) {
			authenticator := NewAuthenticator(map[string]AuthStrategy{
				"Authorization": NewJWTAuthService(secret),
			})

			request, reqErr := http.NewRequest(http.MethodGet, "http://example.com", http.NoBody)
			assert.NoError(t, reqErr)
			request.Header.Set("Authorization", headerValue)

			valid, validateErr := authenticator.Validate(request)
			assert.True(t, valid)
			assert.NoError(t, validateErr)
		})
	}
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
		"Authorization": acceptAnyToken(t),
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
