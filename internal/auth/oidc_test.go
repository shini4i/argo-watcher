package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
		err := service.Init(issuer, "test", []string{})

		assert.NoError(t, err)
		assert.Equal(t, issuer, service.IssuerURL)
		assert.Equal(t, "test", service.ClientId)
		assert.IsType(t, &http.Client{}, service.client)
		// Discovery is lazy: no userinfo URL is resolved during Init.
		assert.Empty(t, service.userinfoURL)
	})

	t.Run("should set http client with timeout", func(t *testing.T) {
		service := &OIDCAuthService{}

		err := service.Init("http://localhost:8080", "test", []string{})

		assert.NoError(t, err)
		assert.Equal(t, 10*time.Second, service.client.Timeout)
	})

	t.Run("should return error for invalid URL scheme", func(t *testing.T) {
		service := &OIDCAuthService{}

		err := service.Init("ftp://localhost:8080", "test", []string{})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid OIDC issuer URL scheme")
	})

	t.Run("should return error for missing host", func(t *testing.T) {
		service := &OIDCAuthService{}

		err := service.Init("http:///realms/test", "test", []string{})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing host")
	})

	t.Run("should return error for URL with query parameters", func(t *testing.T) {
		service := &OIDCAuthService{}

		err := service.Init("https://oidc.example.com?x=1", "test", []string{})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "query and fragment are not allowed")
	})

	t.Run("should return error for URL with fragment", func(t *testing.T) {
		service := &OIDCAuthService{}

		err := service.Init("https://oidc.example.com#frag", "test", []string{})

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
		require.NoError(t, service.Init(server.URL, "test", groups))
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
		require.NoError(t, service.Init("http://127.0.0.1:1", "test", []string{"group1"}))

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

// TestValidateUserinfoURL guards the SSRF check applied to the endpoint the
// discovery document advertises.
func TestValidateUserinfoURL(t *testing.T) {
	assert.Error(t, validateUserinfoURL(""))
	assert.Error(t, validateUserinfoURL("ftp://example.com/userinfo"))
	assert.Error(t, validateUserinfoURL("https:///userinfo"))
	assert.NoError(t, validateUserinfoURL("https://example.com/userinfo"))
}
