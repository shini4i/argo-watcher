package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newOIDCTestServer spins up an httptest server that serves both the OIDC
// discovery document and a userinfo endpoint. The discovery document advertises
// this same server's /userinfo path, so the service resolves it via discovery
// exactly as it would against a real provider. discoveryHits (optional) counts
// discovery requests so tests can assert caching / retry behaviour.
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
		_, err := rw.Write([]byte(fmt.Sprintf(`{"userinfo_endpoint": %q}`, server.URL+"/userinfo")))
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
		// Discovery is lazy: no userinfo URL is resolved during Init.
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

// TestOIDCAuthService_Validate covers token validation through discovery-resolved
// userinfo: privileged-group membership, invalid tokens, malformed responses, and
// unreachable providers.
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

		// First attempt: discovery returns 500, so validation fails but nothing is cached.
		ok, err := service.Validate("test")
		assert.Error(t, err)
		assert.False(t, ok)

		// Second attempt: discovery succeeds and the token validates.
		ok, err = service.Validate("test")
		assert.NoError(t, err)
		assert.True(t, ok)
	})
}

// newCountingOIDCServer serves a working discovery document and a userinfo
// endpoint that reports the given groups, counting userinfo requests so tests can
// assert how often the provider is actually consulted.
func newCountingOIDCServer(t *testing.T, groups string) (*httptest.Server, *int32) {
	t.Helper()

	var hits int32
	var server *httptest.Server
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(rw http.ResponseWriter, _ *http.Request) {
		_, err := rw.Write([]byte(fmt.Sprintf(`{"userinfo_endpoint": %q}`, server.URL+"/userinfo")))
		if err != nil {
			t.Error(err)
		}
	})
	mux.HandleFunc("/userinfo", func(rw http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, err := rw.Write([]byte(fmt.Sprintf(`{"preferred_username": "someone", "groups": %s}`, groups)))
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
			_, _ = rw.Write([]byte(fmt.Sprintf(`{"userinfo_endpoint": %q}`, server.URL+"/userinfo")))
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
			_, _ = rw.Write([]byte(fmt.Sprintf(`{"userinfo_endpoint": %q}`, server.URL+"/userinfo")))
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
			_, _ = rw.Write([]byte(fmt.Sprintf(`{"userinfo_endpoint": %q}`, server.URL+"/userinfo")))
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
