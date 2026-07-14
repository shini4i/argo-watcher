//go:build integration

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/auth"
	"github.com/shini4i/argo-watcher/internal/config"
)

// Keycloak coordinates for the docker-compose `integration` profile. The realm,
// client, privileged group and users are provisioned from
// test/keycloak/argo-watcher-e2e-realm.json on container startup.
const (
	keycloakBaseURL  = "http://localhost:8090"
	keycloakRealm    = "argo-watcher-e2e"
	keycloakClientID = "argo-watcher"
)

// waitForKeycloak polls the realm's OIDC discovery document until Keycloak has
// finished importing the realm and is serving it, mirroring waitForGitea in the
// updater integration suite.
func waitForKeycloak(t *testing.T) {
	t.Helper()
	discovery := keycloakBaseURL + "/realms/" + keycloakRealm + "/.well-known/openid-configuration"
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(discovery) // #nosec G107 - fixed local test URL
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("keycloak realm %q not ready at %s", keycloakRealm, keycloakBaseURL)
}

// keycloakToken obtains an access token for the given user via the direct access
// grant (password) flow against the test realm's public client.
func keycloakToken(t *testing.T, username, password string) string {
	t.Helper()
	tokenURL := keycloakBaseURL + "/realms/" + keycloakRealm + "/protocol/openid-connect/token"
	form := url.Values{
		"grant_type": {"password"},
		"client_id":  {keycloakClientID},
		"username":   {username},
		"password":   {password},
		// Keycloak 26's userinfo endpoint rejects tokens without the openid
		// scope (403), so request it explicitly — the same scope a real OIDC
		// login obtains. argo-watcher validates by calling userinfo.
		"scope": {"openid"},
	}

	resp, err := http.PostForm(tokenURL, form) // #nosec G107 - fixed local test URL
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equalf(t, http.StatusOK, resp.StatusCode, "token request failed: %s", body)

	var out struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.NotEmpty(t, out.AccessToken)
	return out.AccessToken
}

// newKeycloakEnv builds an Env wired with the real Keycloak auth strategy (which
// calls the live Keycloak userinfo endpoint) plus a fresh lockdown — the only
// collaborators the deploy-lock handlers touch.
func newKeycloakEnv(t *testing.T) *Env {
	t.Helper()
	cfg := &config.ServerConfig{
		StateType: "in-memory",
		Keycloak: config.KeycloakConfig{
			Enabled:          true,
			Url:              keycloakBaseURL,
			Realm:            keycloakRealm,
			ClientId:         keycloakClientID,
			PrivilegedGroups: []string{"privileged"},
		},
	}

	keycloakService, err := auth.NewKeycloakAuthService(cfg)
	require.NoError(t, err)

	lockdown, err := NewLockdown("")
	require.NoError(t, err)

	strategies := map[string]auth.AuthStrategy{keycloakHeader: keycloakService}

	return &Env{
		config:        cfg,
		lockdown:      lockdown,
		strategies:    strategies,
		authenticator: auth.NewAuthenticator(strategies),
	}
}

// deployLockServer exposes the Keycloak-gated deploy-lock handlers over real
// HTTP so the test drives the full request → requireKeycloakAuth → Keycloak path.
func deployLockServer(t *testing.T, env *Env) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	v1.POST("/deploy-lock", env.SetDeployLock)
	v1.DELETE("/deploy-lock", env.ReleaseDeployLock)

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv
}

// callDeployLock issues a deploy-lock request, optionally carrying a token in the
// Keycloak-Authorization header, and returns the HTTP status code.
func callDeployLock(t *testing.T, srv *httptest.Server, method, token string) int {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+"/api/v1/deploy-lock", nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set(keycloakHeader, "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// TestKeycloakDeployLockAuthz exercises the deploy-lock endpoints against a real
// Keycloak (docker-compose `integration` profile). It proves the userinfo
// round-trip and group-based authorization that the unit tests only mock.
//
// Status mapping reflects current server behavior (requireKeycloakAuth, router.go):
// every rejection — a strategy error (unprivileged user, garbage/expired token,
// Keycloak unreachable) or a missing token — is mapped to 401. The error case
// surfaces the strategy's sanitized reason; the missing-token case reports that
// authentication is required.
func TestKeycloakDeployLockAuthz(t *testing.T) {
	waitForKeycloak(t)
	srv := deployLockServer(t, newKeycloakEnv(t))

	t.Run("privileged user may set and release the deploy lock", func(t *testing.T) {
		token := keycloakToken(t, "priv-user", "priv-pass")
		assert.Equal(t, http.StatusOK, callDeployLock(t, srv, http.MethodPost, token))
		assert.Equal(t, http.StatusOK, callDeployLock(t, srv, http.MethodDelete, token))
	})

	t.Run("valid token for a non-privileged user is rejected", func(t *testing.T) {
		token := keycloakToken(t, "regular-user", "regular-pass")
		assert.Equal(t, http.StatusUnauthorized, callDeployLock(t, srv, http.MethodPost, token))
	})

	t.Run("garbage token is rejected", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, callDeployLock(t, srv, http.MethodPost, "not-a-real-token"))
	})

	t.Run("missing token is unauthorized", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, callDeployLock(t, srv, http.MethodPost, ""))
	})
}
