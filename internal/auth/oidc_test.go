package auth

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The discovery document advertises this same server's /userinfo path, so the service
// resolves it via discovery exactly as it would against a real provider. discoveryHits
// (optional) counts discovery requests so tests can assert caching / retry behaviour.
func newOIDCTestServer(t *testing.T, userinfoStatus int, userinfoBody string, discoveryHits *int32, failFirstDiscovery bool) *httptest.Server {
	t.Helper()

	var server *httptest.Server
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(rw http.ResponseWriter, _ *http.Request) {
		if discoveryHits != nil {
			atomic.AddInt32(discoveryHits, 1)
		}
		if failFirstDiscovery && discoveryHits != nil && atomic.LoadInt32(discoveryHits) == 1 {
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}
		rw.WriteHeader(http.StatusOK)
		_, err := fmt.Fprintf(rw, `{"userinfo_endpoint": %q}`, server.URL+"/userinfo")
		if err != nil {
			t.Error(err)
		}
	})

	mux.HandleFunc("/userinfo", func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(userinfoStatus)
		_, err := rw.Write([]byte(userinfoBody))
		if err != nil {
			t.Error(err)
		}
	})

	server = httptest.NewServer(mux)
	return server
}

// TestOIDCAuthService_Init verifies the service stores its configuration and
// validates the issuer URL without contacting the network (userinfo is resolved
// lazily on first Validate).
func TestOIDCAuthService_Init(t *testing.T) {
	t.Run("should initialize with valid issuer URL", func(t *testing.T) {
		service := &OIDCAuthService{}

		issuer := "http://localhost:8080/realms/test"
		err := service.Init(issuer, "test", []string{}, 0)

		assert.NoError(t, err)
		assert.Equal(t, issuer, service.IssuerURL)
		assert.Equal(t, "test", service.ClientId)
		assert.IsType(t, &http.Client{}, service.client)
		assert.Empty(t, service.userinfoURL)
	})

	t.Run("should set http client with timeout", func(t *testing.T) {
		service := &OIDCAuthService{}

		err := service.Init("http://localhost:8080", "test", []string{}, 0)

		assert.NoError(t, err)
		assert.Equal(t, 10*time.Second, service.client.Timeout)
	})

	t.Run("should return error for invalid URL scheme", func(t *testing.T) {
		service := &OIDCAuthService{}

		err := service.Init("ftp://localhost:8080", "test", []string{}, 0)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid OIDC issuer URL scheme")
	})

	t.Run("should return error for missing host", func(t *testing.T) {
		service := &OIDCAuthService{}

		err := service.Init("http:///realms/test", "test", []string{}, 0)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing host")
	})

	t.Run("should return error for URL with query parameters", func(t *testing.T) {
		service := &OIDCAuthService{}

		err := service.Init("https://oidc.example.com?x=1", "test", []string{}, 0)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "query and fragment are not allowed")
	})

	t.Run("should return error for URL with fragment", func(t *testing.T) {
		service := &OIDCAuthService{}

		err := service.Init("https://oidc.example.com#frag", "test", []string{}, 0)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "query and fragment are not allowed")
	})
}

func TestOIDCAuthService_Validate(t *testing.T) {
	newService := func(t *testing.T, server *httptest.Server, groups []string) *OIDCAuthService {
		t.Helper()
		service := &OIDCAuthService{}
		require.NoError(t, service.Init(server.URL, "test", groups, 0))
		service.client = server.Client()
		return service
	}

	t.Run("should return true if token is valid and user is in privileged group", func(t *testing.T) {
		server := newOIDCTestServer(t, http.StatusOK, `{"groups": ["group1"]}`, nil, false)
		defer server.Close()

		service := newService(t, server, []string{"group1"})

		ok, err := service.Validate("test")

		assert.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("should return false if token is valid but user is not in privileged group", func(t *testing.T) {
		server := newOIDCTestServer(t, http.StatusOK, `{"groups": ["group2"]}`, nil, false)
		defer server.Close()

		service := newService(t, server, []string{"group1"})

		ok, err := service.Validate("test")

		assert.Error(t, err)
		assert.False(t, ok)
	})

	t.Run("should return false if token is invalid", func(t *testing.T) {
		server := newOIDCTestServer(t, http.StatusUnauthorized, `Unauthorized`, nil, false)
		defer server.Close()

		service := newService(t, server, []string{})

		ok, err := service.Validate("test")

		assert.Error(t, err)
		assert.False(t, ok)
	})

	t.Run("should return sanitized error if response body is invalid JSON", func(t *testing.T) {
		server := newOIDCTestServer(t, http.StatusOK, `invalid json`, nil, false)
		defer server.Close()

		service := newService(t, server, []string{"group1"})

		ok, err := service.Validate("test")

		assert.Error(t, err)
		// Transport/parse failures are sanitized: details live in the server log only.
		assert.Equal(t, "token validation failed", err.Error())
		assert.False(t, ok)
	})

	t.Run("should return sanitized error if provider is unreachable", func(t *testing.T) {
		service := &OIDCAuthService{}
		require.NoError(t, service.Init("http://127.0.0.1:1", "test", []string{"group1"}, 0))

		ok, err := service.Validate("test")

		assert.Error(t, err)
		assert.Equal(t, "token validation failed", err.Error())
		assert.False(t, ok)
	})

	t.Run("should discover the userinfo endpoint only once across calls", func(t *testing.T) {
		var hits int32
		server := newOIDCTestServer(t, http.StatusOK, `{"groups": ["group1"]}`, &hits, false)
		defer server.Close()

		service := newService(t, server, []string{"group1"})

		for i := 0; i < 3; i++ {
			ok, err := service.Validate("test")
			assert.NoError(t, err)
			assert.True(t, ok)
		}

		assert.Equal(t, int32(1), atomic.LoadInt32(&hits), "discovery must be cached after the first success")
	})

	t.Run("should retry discovery after a transient failure", func(t *testing.T) {
		var hits int32
		server := newOIDCTestServer(t, http.StatusOK, `{"groups": ["group1"]}`, &hits, true)
		defer server.Close()

		service := newService(t, server, []string{"group1"})

		ok, err := service.Validate("test")
		assert.Error(t, err)
		assert.False(t, ok)

		ok, err = service.Validate("test")
		assert.NoError(t, err)
		assert.True(t, ok)
	})
}

// Counts userinfo requests so tests can assert how often the provider is actually consulted.
func newCountingOIDCServer(t *testing.T, groups string) (*httptest.Server, *int32) {
	t.Helper()

	var hits int32
	var server *httptest.Server
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(rw http.ResponseWriter, _ *http.Request) {
		_, err := fmt.Fprintf(rw, `{"userinfo_endpoint": %q}`, server.URL+"/userinfo")
		if err != nil {
			t.Error(err)
		}
	})
	mux.HandleFunc("/userinfo", func(rw http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, err := fmt.Fprintf(rw, `{"preferred_username": "someone", "groups": %s}`, groups)
		if err != nil {
			t.Error(err)
		}
	})

	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, &hits
}

// TestOIDCAuthService_Authenticate covers the authentication-only check, which
// deliberately ignores privileged-group membership: read access must be available
// to every signed-in user, while Validate stays the gate for privileged actions.
func TestOIDCAuthService_Authenticate(t *testing.T) {
	t.Run("accepts a valid token for a user in no privileged group", func(t *testing.T) {
		server, _ := newCountingOIDCServer(t, `["some-other-group"]`)
		service := &OIDCAuthService{}
		require.NoError(t, service.Init(server.URL, "test", []string{"privileged"}, 0))
		service.client = server.Client()

		// The same token that Validate must reject for lacking privileges.
		require.NoError(t, service.Authenticate("token"))

		ok, err := service.Validate("token")
		assert.Error(t, err)
		assert.False(t, ok)
	})

	t.Run("rejects a token the provider does not accept", func(t *testing.T) {
		server := newOIDCTestServer(t, http.StatusUnauthorized, `Unauthorized`, nil, false)
		defer server.Close()
		service := &OIDCAuthService{}
		require.NoError(t, service.Init(server.URL, "test", nil, 0))
		service.client = server.Client()

		err := service.Authenticate("token")

		assert.Error(t, err)
		// A rejected token is the client's problem (401), not the provider's (503).
		assert.NotErrorIs(t, err, ErrProviderUnavailable)
	})

	t.Run("reports an unreachable provider distinctly from a rejection", func(t *testing.T) {
		service := &OIDCAuthService{}
		require.NoError(t, service.Init("http://127.0.0.1:1", "test", nil, 0))

		err := service.Authenticate("token")

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrProviderUnavailable)
	})

	t.Run("treats a provider server error as unavailable, not as a rejection", func(t *testing.T) {
		// A 502 or 429 means the token was never judged; calling that a rejection
		// signs valid users out.
		for _, status := range []int{
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusTooManyRequests,
			http.StatusNotFound,
		} {
			server := newOIDCTestServer(t, status, `{}`, nil, false)
			service := &OIDCAuthService{}
			require.NoError(t, service.Init(server.URL, "test", nil, 0))
			service.client = server.Client()

			err := service.Authenticate("token")

			require.Error(t, err, "status %d", status)
			assert.ErrorIs(t, err, ErrProviderUnavailable, "status %d", status)
			server.Close()
		}
	})

	t.Run("treats a forbidden response as a rejection", func(t *testing.T) {
		for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
			server := newOIDCTestServer(t, status, `{}`, nil, false)
			service := &OIDCAuthService{}
			require.NoError(t, service.Init(server.URL, "test", nil, 0))
			service.client = server.Client()

			err := service.Authenticate("token")

			require.Error(t, err, "status %d", status)
			assert.NotErrorIs(t, err, ErrProviderUnavailable, "status %d", status)
			server.Close()
		}
	})

	t.Run("reports an unusable provider response distinctly from a rejection", func(t *testing.T) {
		server := newOIDCTestServer(t, http.StatusOK, `not json`, nil, false)
		defer server.Close()
		service := &OIDCAuthService{}
		require.NoError(t, service.Init(server.URL, "test", nil, 0))
		service.client = server.Client()

		err := service.Authenticate("token")

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrProviderUnavailable)
	})
}

// TestOIDCAuthService_ValidationCaching pins the property that makes gating reads
// on OIDC viable: a token is validated against the provider once per interval, not
// once per request.
func TestOIDCAuthService_ValidationCaching(t *testing.T) {
	t.Run("consults the provider once per interval", func(t *testing.T) {
		server, hits := newCountingOIDCServer(t, `["privileged"]`)
		service := &OIDCAuthService{}
		require.NoError(t, service.Init(server.URL, "test", []string{"privileged"}, time.Minute))
		service.client = server.Client()

		for i := 0; i < 5; i++ {
			require.NoError(t, service.Authenticate("token"))
		}

		assert.Equal(t, int32(1), atomic.LoadInt32(hits),
			"five read authorizations must cost one userinfo call")
	})

	t.Run("re-verifies the provider on every privileged check", func(t *testing.T) {
		// Caching is a trade made for reads, which arrive on a timer. A privileged
		// action is a rare human click, so it always re-asks — that is what keeps
		// group removal and provider-side revocation effective immediately.
		server, hits := newCountingOIDCServer(t, `["privileged"]`)
		service := &OIDCAuthService{}
		require.NoError(t, service.Init(server.URL, "test", []string{"privileged"}, time.Minute))
		service.client = server.Client()

		require.NoError(t, service.Authenticate("token"))
		for i := 0; i < 2; i++ {
			ok, err := service.Validate("token")
			require.NoError(t, err)
			require.True(t, ok)
		}

		assert.Equal(t, int32(3), atomic.LoadInt32(hits),
			"one cached read plus two privileged checks must reach the provider three times")
	})

	t.Run("reflects a group removal on the next privileged check", func(t *testing.T) {
		// The staleness window the validation interval introduces must not extend to
		// privileged authorization: a demoted user loses the deploy lock at once.
		var hits int32
		var server *httptest.Server
		mux := http.NewServeMux()
		mux.HandleFunc("/.well-known/openid-configuration", func(rw http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprintf(rw, `{"userinfo_endpoint": %q}`, server.URL+"/userinfo")
		})
		mux.HandleFunc("/userinfo", func(rw http.ResponseWriter, _ *http.Request) {
			if atomic.AddInt32(&hits, 1) == 1 {
				_, _ = rw.Write([]byte(`{"preferred_username": "someone", "groups": ["privileged"]}`))
				return
			}
			_, _ = rw.Write([]byte(`{"preferred_username": "someone", "groups": []}`))
		})
		server = httptest.NewServer(mux)
		t.Cleanup(server.Close)

		service := &OIDCAuthService{}
		require.NoError(t, service.Init(server.URL, "test", []string{"privileged"}, time.Hour))
		service.client = server.Client()

		ok, err := service.Validate("token")
		require.NoError(t, err)
		require.True(t, ok)

		ok, err = service.Validate("token")
		assert.False(t, ok, "a demoted user must not keep privilege for the validation interval")
		assert.ErrorContains(t, err, "not a member of any of the privileged groups")
	})

	t.Run("reuses a cached rejection even on the privileged path", func(t *testing.T) {
		// A rejection cannot over-grant, so honoring it keeps a loop of bad tokens
		// from being amplified into provider traffic — POST /api/v1/tasks validates
		// this way and is reachable without a credential.
		var hits int32
		var server *httptest.Server
		mux := http.NewServeMux()
		mux.HandleFunc("/.well-known/openid-configuration", func(rw http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprintf(rw, `{"userinfo_endpoint": %q}`, server.URL+"/userinfo")
		})
		mux.HandleFunc("/userinfo", func(rw http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(&hits, 1)
			rw.WriteHeader(http.StatusUnauthorized)
		})
		server = httptest.NewServer(mux)
		t.Cleanup(server.Close)

		service := &OIDCAuthService{}
		require.NoError(t, service.Init(server.URL, "test", []string{"privileged"}, time.Minute))
		service.client = server.Client()

		for i := 0; i < 3; i++ {
			ok, err := service.Validate("bad-token")
			require.False(t, ok)
			require.Error(t, err)
		}

		assert.Equal(t, int32(1), atomic.LoadInt32(&hits),
			"a rejection is safe to reuse, so repeats must not reach the provider")
	})

	t.Run("a cached authentication cannot become an authorization", func(t *testing.T) {
		// The worst failure this design could produce: caching the fact that someone
		// is signed in must never be mistaken for the fact that they are privileged.
		server, _ := newCountingOIDCServer(t, `["some-other-group"]`)
		service := &OIDCAuthService{}
		require.NoError(t, service.Init(server.URL, "test", []string{"privileged"}, time.Minute))
		service.client = server.Client()

		require.NoError(t, service.Authenticate("token"))

		for i := 0; i < 2; i++ {
			ok, err := service.Validate("token")
			assert.False(t, ok, "call %d", i)
			assert.ErrorContains(t, err, "not a member of any of the privileged groups")
		}

		// And the reverse: a privilege rejection must not be remembered as an
		// authentication failure, or a read would start failing after a lock click.
		assert.NoError(t, service.Authenticate("token"))
	})

	t.Run("re-validates once the interval elapses", func(t *testing.T) {
		server, hits := newCountingOIDCServer(t, `["privileged"]`)
		service := &OIDCAuthService{}
		require.NoError(t, service.Init(server.URL, "test", []string{"privileged"}, 10*time.Millisecond))
		service.client = server.Client()

		require.NoError(t, service.Authenticate("token"))
		time.Sleep(20 * time.Millisecond)
		require.NoError(t, service.Authenticate("token"))

		assert.Equal(t, int32(2), atomic.LoadInt32(hits))
	})

	t.Run("caches per token, not globally", func(t *testing.T) {
		server, hits := newCountingOIDCServer(t, `["privileged"]`)
		service := &OIDCAuthService{}
		require.NoError(t, service.Init(server.URL, "test", []string{"privileged"}, time.Minute))
		service.client = server.Client()

		require.NoError(t, service.Authenticate("token-a"))
		require.NoError(t, service.Authenticate("token-b"))

		assert.Equal(t, int32(2), atomic.LoadInt32(hits))
	})

	t.Run("never caches an unreachable provider", func(t *testing.T) {
		// A provider outage must not be remembered: the next request has to retry,
		// or a transient blip would deny access for the whole interval.
		var hits int32
		var server *httptest.Server
		mux := http.NewServeMux()
		mux.HandleFunc("/.well-known/openid-configuration", func(rw http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprintf(rw, `{"userinfo_endpoint": %q}`, server.URL+"/userinfo")
		})
		mux.HandleFunc("/userinfo", func(rw http.ResponseWriter, _ *http.Request) {
			if atomic.AddInt32(&hits, 1) == 1 {
				// An unparseable answer means the provider is not usable — the
				// token itself was never judged, so this must not be remembered.
				_, _ = rw.Write([]byte(`<html>gateway error</html>`))
				return
			}
			_, _ = rw.Write([]byte(`{"groups": ["privileged"]}`))
		})
		server = httptest.NewServer(mux)
		t.Cleanup(server.Close)

		service := &OIDCAuthService{}
		require.NoError(t, service.Init(server.URL, "test", []string{"privileged"}, time.Minute))
		service.client = server.Client()

		err := service.Authenticate("token")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrProviderUnavailable)

		assert.NoError(t, service.Authenticate("token"), "the failure must not have been cached")
		assert.Equal(t, int32(2), atomic.LoadInt32(&hits))
	})

	t.Run("serves concurrent validations safely", func(t *testing.T) {
		server, _ := newCountingOIDCServer(t, `["privileged"]`)
		service := &OIDCAuthService{}
		require.NoError(t, service.Init(server.URL, "test", []string{"privileged"}, time.Minute))
		service.client = server.Client()

		var wg sync.WaitGroup
		for i := 0; i < 25; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				assert.NoError(t, service.Authenticate(fmt.Sprintf("token-%d", i%5)))
			}(i)
		}
		wg.Wait()
	})
}

// TestValidateUserinfoURL guards the SSRF check applied to the endpoint the
// discovery document advertises.
func TestValidateUserinfoURL(t *testing.T) {
	assert.Error(t, validateUserinfoURL(""))
	assert.Error(t, validateUserinfoURL("ftp://example.com/userinfo"))
	assert.Error(t, validateUserinfoURL("https:///userinfo"))
	assert.NoError(t, validateUserinfoURL("https://example.com/userinfo"))
}

// signedToken mints a JWT whose signature this package never verifies: the binding
// check reads the claims of a token the provider has already vouched for.
func signedToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tokenStr, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("irrelevant"))
	require.NoError(t, err)
	return tokenStr
}

// TestOIDCAuthService_TokenBinding covers the audience binding: a token the provider
// accepts must also have been issued to this application, so one realm shared by the
// whole organisation cannot authenticate its every client here.
func TestOIDCAuthService_TokenBinding(t *testing.T) {
	newService := func(t *testing.T, server *httptest.Server) *OIDCAuthService {
		t.Helper()
		service := &OIDCAuthService{}
		require.NoError(t, service.Init(server.URL, "argo-watcher", []string{"group1"}, 0))
		service.client = server.Client()
		return service
	}

	accepted := []struct {
		name   string
		claims jwt.MapClaims
	}{
		{"azp names this client", jwt.MapClaims{"azp": "argo-watcher", "aud": []string{"account"}}},
		{"aud is this client", jwt.MapClaims{"aud": "argo-watcher"}},
		{"aud lists this client", jwt.MapClaims{"aud": []string{"other", "argo-watcher"}}},
		{"token carries neither claim", jwt.MapClaims{"sub": "user"}},
		{"aud is malformed and no client is named", jwt.MapClaims{"aud": 42}},
		// An array holding a non-string is unreadable as a whole, so the client id
		// beside it does not count as a match — the token names nobody.
		{"aud array is unreadable and no client is named", jwt.MapClaims{"aud": []any{"argo-watcher", 42}}},
		// RFC 9068 gives aud to the resource server and names the client in client_id;
		// Okta spells that claim cid. Neither emits azp, so both would 401 wholesale
		// if only azp and aud were read.
		{"client_id names this client (RFC 9068)", jwt.MapClaims{"client_id": "argo-watcher", "aud": []string{"https://api.example.com"}}},
		{"cid names this client (Okta)", jwt.MapClaims{"cid": "argo-watcher", "aud": []string{"https://api.example.com"}}},
		{"appid names this client (Entra v1.0)", jwt.MapClaims{"appid": "argo-watcher", "aud": "https://graph.microsoft.com"}},
		// Issued by another client but deliberately audienced to this one, as an
		// audience mapper or a token exchange produces.
		{"azp names another client but aud names this one", jwt.MapClaims{"azp": "other-app", "aud": []string{"argo-watcher"}}},
	}

	for _, tc := range accepted {
		t.Run("accepts a token whose "+tc.name, func(t *testing.T) {
			server := newOIDCTestServer(t, http.StatusOK, `{"groups": ["group1"]}`, nil, false)
			defer server.Close()

			ok, err := newService(t, server).Validate(signedToken(t, tc.claims))

			assert.NoError(t, err)
			assert.True(t, ok)
		})
	}

	t.Run("accepts an opaque token the provider vouches for", func(t *testing.T) {
		// Binding an opaque token would need introspection, which a public client
		// cannot perform; the provider's judgement stays the only gate.
		server := newOIDCTestServer(t, http.StatusOK, `{"groups": ["group1"]}`, nil, false)
		defer server.Close()

		ok, err := newService(t, server).Validate("not-a-jwt")

		assert.NoError(t, err)
		assert.True(t, ok)
	})

	rejected := []struct {
		name   string
		claims jwt.MapClaims
	}{
		{"azp names another client", jwt.MapClaims{"azp": "other-app", "aud": []string{"account"}}},
		{"aud names another client", jwt.MapClaims{"aud": "other-app"}},
		{"aud lists only other clients", jwt.MapClaims{"aud": []string{"other-app", "account"}}},
		{"client_id names another client", jwt.MapClaims{"client_id": "other-app", "aud": []string{"https://api.example.com"}}},
		{"cid names another client", jwt.MapClaims{"cid": "other-app", "aud": []string{"https://api.example.com"}}},
		{"appid names another client", jwt.MapClaims{"appid": "other-app", "aud": "https://graph.microsoft.com"}},
		// An unreadable aud must not rescue a token another client owns.
		{"azp names another client and aud is malformed", jwt.MapClaims{"azp": "other-app", "aud": 42}},
		{"azp names another client and the aud array is unreadable", jwt.MapClaims{"azp": "other-app", "aud": []any{"argo-watcher", 42}}},
	}

	for _, tc := range rejected {
		t.Run("rejects a token whose "+tc.name, func(t *testing.T) {
			server := newOIDCTestServer(t, http.StatusOK, `{"groups": ["group1"]}`, nil, false)
			defer server.Close()
			service := newService(t, server)
			token := signedToken(t, tc.claims)

			ok, err := service.Validate(token)
			require.Error(t, err)
			assert.False(t, ok)
			assert.Contains(t, err.Error(), "not issued to")
			// A foreign token is the client's problem (401), not the provider's (503).
			assert.NotErrorIs(t, err, ErrProviderUnavailable)

			// The read path must reject it too, not only the privileged one.
			assert.Error(t, service.Authenticate(token))
		})
	}
}

// TestOIDCAuthService_TokenBindingCache exercises the binding on the path a real
// deployment takes, where OIDC_TOKEN_VALIDATION_INTERVAL is non-zero: a rejection is
// cached, must survive reuse without another provider call, and must not poison a
// token that is properly bound.
func TestOIDCAuthService_TokenBindingCache(t *testing.T) {
	server, hits := newCountingOIDCServer(t, `["group1"]`)
	service := &OIDCAuthService{}
	require.NoError(t, service.Init(server.URL, "argo-watcher", []string{"group1"}, time.Minute))
	service.client = server.Client()

	foreign := signedToken(t, jwt.MapClaims{"azp": "other-app", "aud": []string{"account"}})

	ok, err := service.Validate(foreign)
	require.Error(t, err)
	assert.False(t, ok)
	assert.Contains(t, err.Error(), "not issued to")
	after := atomic.LoadInt32(hits)

	ok, err = service.Validate(foreign)
	assert.Error(t, err)
	assert.False(t, ok)
	assert.Equal(t, after, atomic.LoadInt32(hits), "a cached rejection must not re-ask the provider")

	ok, err = service.Validate(signedToken(t, jwt.MapClaims{"azp": "argo-watcher"}))
	assert.NoError(t, err)
	assert.True(t, ok, "the cached rejection is keyed per token and must not deny a bound one")
}

// TestOIDCAuthService_UnboundTokenWarning pins the one operator-visible signal that
// tokens are not being bound: it names the reason and is logged once, not per request.
func TestOIDCAuthService_UnboundTokenWarning(t *testing.T) {
	newService := func(t *testing.T, server *httptest.Server) *OIDCAuthService {
		t.Helper()
		service := &OIDCAuthService{}
		require.NoError(t, service.Init(server.URL, "argo-watcher", []string{"group1"}, 0))
		service.client = server.Client()
		return service
	}

	captureWarnings := func(t *testing.T) *bytes.Buffer {
		t.Helper()
		logs := &bytes.Buffer{}
		previous := slog.Default()
		t.Cleanup(func() { slog.SetDefault(previous) })
		slog.SetDefault(slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
		return logs
	}

	t.Run("an opaque token warns once, however many requests arrive", func(t *testing.T) {
		server := newOIDCTestServer(t, http.StatusOK, `{"groups": ["group1"]}`, nil, false)
		defer server.Close()
		service := newService(t, server)
		logs := captureWarnings(t)

		for range 3 {
			ok, err := service.Validate("not-a-jwt")
			require.NoError(t, err)
			require.True(t, ok)
		}

		assert.Equal(t, 1, strings.Count(logs.String(), "accepting tokens without checking"))
		assert.Contains(t, logs.String(), "the access token is not a JWT")
	})

	t.Run("a JWT naming nobody warns with its own reason", func(t *testing.T) {
		server := newOIDCTestServer(t, http.StatusOK, `{"groups": ["group1"]}`, nil, false)
		defer server.Close()
		service := newService(t, server)
		logs := captureWarnings(t)

		ok, err := service.Validate(signedToken(t, jwt.MapClaims{"sub": "user"}))
		require.NoError(t, err)
		require.True(t, ok)

		assert.Contains(t, logs.String(), "names no client and carries no aud claim")
	})
}

// TestOIDCAuthService_Identify covers the sole source of the created_by attribution
// stored on every issued application deploy token.
func TestOIDCAuthService_Identify(t *testing.T) {
	t.Run("returns the username the provider reports", func(t *testing.T) {
		server, _ := newCountingOIDCServer(t, `["privileged"]`)
		service := &OIDCAuthService{}
		require.NoError(t, service.Init(server.URL, "test", []string{"privileged"}, time.Minute))
		service.client = server.Client()

		username, err := service.Identify("token")

		require.NoError(t, err)
		assert.Equal(t, "someone", username)
	})

	t.Run("answers a privileged action from the cache Validate just filled", func(t *testing.T) {
		// Identify passes allowCached=true precisely so attributing an action costs no
		// extra round trip. Flipping that adds one userinfo call per issue and revoke.
		server, hits := newCountingOIDCServer(t, `["privileged"]`)
		service := &OIDCAuthService{}
		require.NoError(t, service.Init(server.URL, "test", []string{"privileged"}, time.Minute))
		service.client = server.Client()

		valid, err := service.Validate("token")
		require.NoError(t, err)
		require.True(t, valid)
		before := atomic.LoadInt32(hits)

		_, err = service.Identify("token")
		require.NoError(t, err)

		assert.Equal(t, before, atomic.LoadInt32(hits), "the cached identity must be reused")
	})

	t.Run("reports a rejected token without naming a user", func(t *testing.T) {
		server := newOIDCTestServer(t, http.StatusUnauthorized, "", nil, false)
		service := &OIDCAuthService{}
		require.NoError(t, service.Init(server.URL, "test", []string{"privileged"}, 0))
		service.client = server.Client()

		username, err := service.Identify("token")

		require.Error(t, err)
		assert.Empty(t, username)
	})

	t.Run("surfaces an unreachable provider as such", func(t *testing.T) {
		service := &OIDCAuthService{}
		require.NoError(t, service.Init("http://127.0.0.1:1", "test", nil, 0))

		username, err := service.Identify("token")

		require.ErrorIs(t, err, ErrProviderUnavailable)
		assert.Empty(t, username)
	})
}

// TestAuthenticatorIdentifyRequest covers the branch that keeps attribution from
// panicking when nothing can name a user.
func TestAuthenticatorIdentifyRequest(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/app-tokens", nil)
	request.Header.Set("Oidc-Authorization", "Bearer token")

	t.Run("no strategy registered under the header", func(t *testing.T) {
		authenticator := NewAuthenticator(map[string]AuthStrategy{})

		username, err := authenticator.IdentifyRequest(request, "Oidc-Authorization")

		assert.NoError(t, err)
		assert.Empty(t, username)
	})

	t.Run("a strategy that cannot name a user", func(t *testing.T) {
		// The deploy token has no identity behind it, so the caller falls back to
		// recording the action against an unnamed operator rather than failing it.
		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"Oidc-Authorization": NewDeployTokenAuthService("shared"),
		})

		username, err := authenticator.IdentifyRequest(request, "Oidc-Authorization")

		assert.NoError(t, err)
		assert.Empty(t, username)
	})

	t.Run("no token sent", func(t *testing.T) {
		bare := httptest.NewRequest(http.MethodPost, "/api/v1/app-tokens", nil)
		authenticator := NewAuthenticator(map[string]AuthStrategy{
			"Oidc-Authorization": &OIDCAuthService{},
		})

		username, err := authenticator.IdentifyRequest(bare, "Oidc-Authorization")

		assert.NoError(t, err)
		assert.Empty(t, username)
	})
}
