package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/apptoken"
)

// stubStore is an in-process Store standing in for Postgres. The production code
// has no in-memory implementation on purpose (see apptoken.Store), so the double
// lives here, where losing it on restart is the point.
type stubStore struct {
	tokens    map[string]*apptoken.Token
	lookupErr error
	usedIds   []uuid.UUID
	usedErr   error
}

func newStubStore() *stubStore {
	return &stubStore{tokens: map[string]*apptoken.Token{}}
}

func (s *stubStore) add(secret string, token apptoken.Token) string {
	if token.Id == uuid.Nil {
		token.Id = uuid.New()
	}
	s.tokens[secret] = &token
	return secret
}

func (s *stubStore) Issue(apptoken.Scope, string, string, time.Time) (*apptoken.IssuedToken, error) {
	return nil, errors.New("not used in these tests")
}

func (s *stubStore) Lookup(secret string) (*apptoken.Token, error) {
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	token, ok := s.tokens[secret]
	if !ok {
		return nil, apptoken.ErrNotFound
	}
	return token, nil
}

func (s *stubStore) List() ([]apptoken.Token, error) { return nil, nil }

func (s *stubStore) Revoke(uuid.UUID) error { return nil }

func (s *stubStore) MarkUsed(id uuid.UUID) error {
	s.usedIds = append(s.usedIds, id)
	return s.usedErr
}

func TestAppTokenValidateForApp(t *testing.T) {
	t.Run("accepts a token scoped to the application", func(t *testing.T) {
		store := newStubStore()
		secret := store.add("awt_scoped", apptoken.Token{Scope: apptoken.Scope{Apps: []string{"app1", "app2"}}})
		service := NewAppTokenAuthService(store)

		valid, err := service.ValidateForApp(secret, "app2")

		require.NoError(t, err)
		assert.True(t, valid)
	})

	t.Run("accepts a wildcard token for any application", func(t *testing.T) {
		store := newStubStore()
		secret := store.add("awt_wildcard", apptoken.Token{Scope: apptoken.Scope{AllApps: true}})
		service := NewAppTokenAuthService(store)

		valid, err := service.ValidateForApp(secret, "some-app-issued-after-the-token")

		require.NoError(t, err)
		assert.True(t, valid)
	})

	t.Run("records that the token authorized a deployment", func(t *testing.T) {
		store := newStubStore()
		secret := store.add("awt_used", apptoken.Token{Scope: apptoken.Scope{AllApps: true}})
		service := NewAppTokenAuthService(store)

		_, err := service.ValidateForApp(secret, "app1")
		require.NoError(t, err)

		require.Len(t, store.usedIds, 1)
		assert.Equal(t, store.tokens[secret].Id, store.usedIds[0])
	})

	t.Run("authorizes even when recording the use fails", func(t *testing.T) {
		store := newStubStore()
		store.usedErr = errors.New("write failed")
		secret := store.add("awt_bookkeeping", apptoken.Token{Scope: apptoken.Scope{AllApps: true}})
		service := NewAppTokenAuthService(store)

		valid, err := service.ValidateForApp(secret, "app1")

		require.NoError(t, err, "bookkeeping must never fail a deployment")
		assert.True(t, valid)
	})

	tests := []struct {
		name    string
		secret  string
		token   *apptoken.Token
		app     string
		message string
	}{
		{
			name:    "rejects an application outside the scope",
			secret:  "awt_narrow",
			token:   &apptoken.Token{Scope: apptoken.Scope{Apps: []string{"app1"}}},
			app:     "app2",
			message: "not authorize application app2",
		},
		{
			name:    "rejects an unknown token",
			secret:  "awt_unknown",
			app:     "app1",
			message: "not a known application deploy token",
		},
		{
			name:    "rejects a revoked token",
			secret:  "awt_revoked",
			token:   &apptoken.Token{Scope: apptoken.Scope{AllApps: true}, RevokedAt: time.Now().Add(-time.Hour)},
			app:     "app1",
			message: "has been revoked",
		},
		{
			name:    "rejects an expired token",
			secret:  "awt_expired",
			token:   &apptoken.Token{Scope: apptoken.Scope{AllApps: true}, ExpiresAt: time.Now().Add(-time.Minute)},
			app:     "app1",
			message: "has expired",
		},
		{
			name:    "rejects a request with no application",
			secret:  "awt_noapp",
			token:   &apptoken.Token{Scope: apptoken.Scope{AllApps: true}},
			app:     "",
			message: "names no application",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newStubStore()
			if test.token != nil {
				store.add(test.secret, *test.token)
			}
			service := NewAppTokenAuthService(store)

			valid, err := service.ValidateForApp(test.secret, test.app)

			assert.False(t, valid)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.message)
			assert.Empty(t, store.usedIds, "a rejected token must not be recorded as used")
		})
	}
}

func TestAppTokenStoreFailureIsNotARejection(t *testing.T) {
	store := newStubStore()
	store.lookupErr = errors.New("connection refused")
	service := NewAppTokenAuthService(store)

	t.Run("ValidateForApp", func(t *testing.T) {
		valid, err := service.ValidateForApp("awt_anything", "app1")

		assert.False(t, valid)
		// A database that cannot be consulted must map to 503, not 401: a blip must
		// not tell a pipeline its credential is bad.
		require.ErrorIs(t, err, ErrProviderUnavailable)
	})

	t.Run("Authenticate", func(t *testing.T) {
		require.ErrorIs(t, service.Authenticate("awt_anything"), ErrProviderUnavailable)
	})
}

func TestAppTokenValidateRefusesWithoutAnApplication(t *testing.T) {
	store := newStubStore()
	secret := store.add("awt_wildcard", apptoken.Token{Scope: apptoken.Scope{AllApps: true}})
	service := NewAppTokenAuthService(store)

	// Plain Validate has no application to weigh the scope against. It must refuse
	// even a wildcard token, so a call site that forgets ValidateForApp fails closed
	// instead of treating an app token as a global credential.
	valid, err := service.Validate(secret)

	assert.False(t, valid)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no application context")
}

func TestAppTokenAuthenticate(t *testing.T) {
	store := newStubStore()
	live := store.add("awt_live", apptoken.Token{Scope: apptoken.Scope{Apps: []string{"app1"}}})
	revoked := store.add("awt_revoked", apptoken.Token{Scope: apptoken.Scope{AllApps: true}, RevokedAt: time.Now()})
	service := NewAppTokenAuthService(store)

	// Reads need no application context, so a live token of any scope grants them.
	require.NoError(t, service.Authenticate(live))
	assert.Empty(t, store.usedIds, "a read must not count as a deployment")

	require.Error(t, service.Authenticate(revoked))
	require.Error(t, service.Authenticate("awt_unknown"))
}

func TestPrefixRouter(t *testing.T) {
	store := newStubStore()
	secret := store.add("awt_scoped", apptoken.Token{Scope: apptoken.Scope{Apps: []string{"app1"}}})
	jwtService := NewJWTAuthService("secret", "", "")
	router := NewPrefixRouter(NewAppTokenAuthService(store), jwtService)

	t.Run("routes a prefixed token to the app token strategy", func(t *testing.T) {
		valid, err := router.ValidateForApp(secret, "app1")

		require.NoError(t, err)
		assert.True(t, valid)
	})

	t.Run("routes anything else to the fallback", func(t *testing.T) {
		valid, err := router.ValidateForApp("not-a-jwt-either", "app1")

		assert.False(t, valid)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "application deploy token")
	})

	t.Run("forwards the unscoped question too", func(t *testing.T) {
		valid, err := router.Validate(secret)

		assert.False(t, valid)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no application context")
	})

	t.Run("forwards authentication", func(t *testing.T) {
		require.NoError(t, router.Authenticate(secret))
	})

	t.Run("reports no credential when the fallback is absent", func(t *testing.T) {
		bare := NewPrefixRouter(NewAppTokenAuthService(store), nil)

		valid, err := bare.Validate("eyJhbGciOiJIUzI1NiJ9.e30.sig")

		assert.False(t, valid)
		require.ErrorIs(t, err, ErrNoCredential)
	})
}

func TestDisabledAppTokenStrategy(t *testing.T) {
	jwtService := NewJWTAuthService("secret", "", "")
	router := NewPrefixRouter(DisabledAppTokenStrategy(), jwtService)

	valid, err := router.ValidateForApp("awt_whatever", "app1")

	assert.False(t, valid)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OIDC_ENABLED")
	assert.Contains(t, err.Error(), "STATE_TYPE=postgres")
}

func TestAppTokenStrategyIsRecognisedByTheAuthenticator(t *testing.T) {
	store := newStubStore()
	secret := store.add("awt_scoped", apptoken.Token{Scope: apptoken.Scope{Apps: []string{"app1"}}})
	authenticator := NewAuthenticator(map[string]AuthStrategy{
		"Authorization": NewPrefixRouter(NewAppTokenAuthService(store), NewJWTAuthService("secret", "", "")),
	})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", nil)
	request.Header.Set("Authorization", "Bearer "+secret)

	valid, err := authenticator.ValidateForApp(request, "app1")
	require.NoError(t, err)
	assert.True(t, valid)

	valid, err = authenticator.ValidateForApp(request, "app2")
	assert.False(t, valid)
	require.Error(t, err)
}

func TestAuthenticatorValidateForAppFallsBackForUnscopedStrategies(t *testing.T) {
	authenticator := NewAuthenticator(map[string]AuthStrategy{
		"ARGO_WATCHER_DEPLOY_TOKEN": NewDeployTokenAuthService("shared-secret"),
	})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", nil)
	request.Header.Set("ARGO_WATCHER_DEPLOY_TOKEN", "shared-secret")

	// The shared deploy token has no application scope, so it answers the unscoped
	// question and keeps authorizing every application exactly as before.
	valid, err := authenticator.ValidateForApp(request, "any-app")

	require.NoError(t, err)
	assert.True(t, valid)

	// The fallback must still be the strategy's own answer, not a blanket grant.
	request.Header.Set("ARGO_WATCHER_DEPLOY_TOKEN", "wrong-secret")

	valid, err = authenticator.ValidateForApp(request, "any-app")

	assert.False(t, valid)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deploy token is invalid")
}

func TestNoCredentialIsNotARejection(t *testing.T) {
	store := newStubStore()
	// No JWT secret is configured, so nothing evaluates a non-prefixed token on
	// Authorization. Such a header must stay ignored rather than becoming a 401:
	// a client setting Authorization for an unrelated proxy kept working before
	// application deploy tokens existed, and must keep working now.
	authenticator := NewAuthenticator(map[string]AuthStrategy{
		"Authorization": NewPrefixRouter(NewAppTokenAuthService(store), nil),
	})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", nil)
	request.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiJ9.e30.sig")

	valid, err := authenticator.ValidateForApp(request, "app1")

	assert.False(t, valid)
	assert.NoError(t, err, "an unevaluated header is 'no credential', not a rejection")

	valid, err = authenticator.AuthenticateRequest(request)
	assert.False(t, valid)
	assert.NoError(t, err)
}

// TestDisabledAppTokenStrategyRefusesEveryQuestion covers the stand-in used while
// the feature is off, including the read path: a pipeline polling a task status with
// a prefixed token must be told why rather than being treated as uncredentialed.
func TestDisabledAppTokenStrategyRefusesEveryQuestion(t *testing.T) {
	strategy := DisabledAppTokenStrategy()

	t.Run("Validate", func(t *testing.T) {
		valid, err := strategy.Validate("awt_someIssuedToken")

		assert.False(t, valid)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "OIDC_ENABLED")
	})

	t.Run("ValidateForApp", func(t *testing.T) {
		scoped, ok := strategy.(interface {
			ValidateForApp(token, app string) (bool, error)
		})
		require.True(t, ok)

		valid, err := scoped.ValidateForApp("awt_someIssuedToken", "app1")

		assert.False(t, valid)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "STATE_TYPE=postgres")
	})

	t.Run("Authenticate", func(t *testing.T) {
		authenticator, ok := strategy.(interface{ Authenticate(token string) error })
		require.True(t, ok)

		err := authenticator.Authenticate("awt_someIssuedToken")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "OIDC_ENABLED")
	})

	t.Run("a read through the authenticator is refused, not ignored", func(t *testing.T) {
		router := NewPrefixRouter(strategy, nil)
		authenticator := NewAuthenticator(map[string]AuthStrategy{"Authorization": router})

		request := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
		request.Header.Set("Authorization", "Bearer awt_someIssuedToken")

		valid, err := authenticator.AuthenticateRequest(request)

		assert.False(t, valid)
		require.Error(t, err, "an unevaluable feature must not read as no credential")
	})
}

// TestPrefixRouterHandles pins the predicate hasCredential relies on to tell a
// credential apart from a header nothing reads.
func TestPrefixRouterHandles(t *testing.T) {
	store := newStubStore()
	appTokens := NewAppTokenAuthService(store)

	t.Run("with a fallback everything is handled", func(t *testing.T) {
		router := NewPrefixRouter(appTokens, NewJWTAuthService("secret", "", ""))

		assert.True(t, router.Handles("awt_prefixed"))
		assert.True(t, router.Handles("eyJhbGciOiJIUzI1NiJ9.e30.sig"))
	})

	t.Run("without a fallback only prefixed tokens are handled", func(t *testing.T) {
		router := NewPrefixRouter(appTokens, nil)

		assert.True(t, router.Handles("awt_prefixed"))
		assert.False(t, router.Handles("eyJhbGciOiJIUzI1NiJ9.e30.sig"),
			"nothing evaluates this, so it is not a credential")
	})
}
