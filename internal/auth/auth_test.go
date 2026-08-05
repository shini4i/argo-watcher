package auth

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// splitStrategy is a strategy that separates authentication from authorization,
// standing in for the OIDC service: Validate additionally demands privilege.
type splitStrategy struct {
	authenticated bool
	privileged    bool
}

func (s splitStrategy) Authenticate(string) error {
	if !s.authenticated {
		return errors.New("not authenticated")
	}
	return nil
}

func (s splitStrategy) Validate(string) (bool, error) {
	if !s.authenticated || !s.privileged {
		return false, errors.New("not privileged")
	}
	return true, nil
}

// unavailableStrategy stands in for an OIDC service whose provider cannot be
// reached, which callers must distinguish from a rejected credential.
type unavailableStrategy struct{}

func (unavailableStrategy) Authenticate(string) error { return ErrProviderUnavailable }

func (unavailableStrategy) Validate(string) (bool, error) { return false, ErrProviderUnavailable }

func TestAuthenticatorAuthenticateRequest(t *testing.T) {
	newRequest := func(t *testing.T, header, value string) *http.Request {
		t.Helper()
		request, err := http.NewRequest(http.MethodGet, "http://example.com", http.NoBody)
		assert.NoError(t, err)
		if header != "" {
			request.Header.Set(header, value)
		}
		return request
	}

	t.Run("accepts an authenticated but unprivileged subject", func(t *testing.T) {
		// This is the whole point of the split: reads are open to any signed-in
		// user, while Validate keeps gating privileged actions.
		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"Oidc-Authorization": splitStrategy{authenticated: true, privileged: false},
		})
		request := newRequest(t, "Oidc-Authorization", "token")

		valid, err := authenticator.AuthenticateRequest(request)
		assert.True(t, valid)
		assert.NoError(t, err)

		valid, err = authenticator.Validate(request)
		assert.False(t, valid)
		assert.Error(t, err)
	})

	t.Run("rejects an unauthenticated subject", func(t *testing.T) {
		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"Oidc-Authorization": splitStrategy{authenticated: false},
		})

		valid, err := authenticator.AuthenticateRequest(newRequest(t, "Oidc-Authorization", "token"))

		assert.False(t, valid)
		assert.Error(t, err)
	})

	t.Run("treats possession as authentication for strategies without groups", func(t *testing.T) {
		// A deploy token or CI JWT carries no group concept, so a valid token is
		// authentication — this is what lets a pipeline read its own task.
		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"ARGO_WATCHER_DEPLOY_TOKEN": NewDeployTokenAuthService("valid"),
		})

		valid, err := authenticator.AuthenticateRequest(newRequest(t, "ARGO_WATCHER_DEPLOY_TOKEN", "valid"))

		assert.True(t, valid)
		assert.NoError(t, err)
	})

	t.Run("reports no credential distinctly from a rejected one", func(t *testing.T) {
		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"ARGO_WATCHER_DEPLOY_TOKEN": NewDeployTokenAuthService("valid"),
		})

		valid, err := authenticator.AuthenticateRequest(newRequest(t, "", ""))

		assert.False(t, valid)
		assert.NoError(t, err, "no header sent must not look like a wrong token")
	})

	t.Run("accepts a valid credential alongside a rejected one", func(t *testing.T) {
		// The frontend sends the OIDC token in both Authorization and
		// Oidc-Authorization; with JWT_SECRET set the former is parsed as an HMAC
		// JWT and fails. One working credential must still authenticate.
		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"Authorization":      NewJWTAuthService("secret"),
			"Oidc-Authorization": splitStrategy{authenticated: true},
		})
		request := newRequest(t, "Authorization", "Bearer not-an-hmac-jwt")
		request.Header.Set("Oidc-Authorization", "Bearer oidc-token")

		valid, err := authenticator.AuthenticateRequest(request)

		assert.True(t, valid)
		assert.NoError(t, err)
	})

	t.Run("prefers an unavailable provider over another strategy's rejection", func(t *testing.T) {
		// The real shape of this: the Web UI sends its OIDC token in BOTH headers,
		// and with JWT_SECRET set the Authorization copy is parsed as an HMAC JWT and
		// rejected, while the OIDC strategy cannot reach the provider. Strategy
		// iteration order is randomized, so the loop below would catch a
		// coin-flipping precedence — and a 401 here signs the user out of a session
		// that may be entirely valid.
		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"Authorization":      NewJWTAuthService("secret"),
			"Oidc-Authorization": unavailableStrategy{},
		})

		for i := 0; i < 50; i++ {
			request := newRequest(t, "Authorization", "Bearer not-an-hmac-jwt")
			request.Header.Set("Oidc-Authorization", "Bearer oidc-token")

			valid, err := authenticator.AuthenticateRequest(request)

			require.False(t, valid)
			require.ErrorIs(t, err, ErrProviderUnavailable, "iteration %d", i)
		}
	})

	t.Run("reports a rejection when every credential was actually evaluated", func(t *testing.T) {
		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"Authorization":      NewJWTAuthService("secret"),
			"Oidc-Authorization": splitStrategy{authenticated: false},
		})
		request := newRequest(t, "Authorization", "Bearer not-an-hmac-jwt")
		request.Header.Set("Oidc-Authorization", "Bearer oidc-token")

		valid, err := authenticator.AuthenticateRequest(request)

		assert.False(t, valid)
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrProviderUnavailable)
	})

	t.Run("tolerates a nil receiver and a nil request", func(t *testing.T) {
		var authenticator *Authenticator
		valid, err := authenticator.AuthenticateRequest(newRequest(t, "", ""))
		assert.False(t, valid)
		assert.NoError(t, err)

		valid, err = NewAuthenticator(nil).AuthenticateRequest(nil)
		assert.False(t, valid)
		assert.NoError(t, err)
	})
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
