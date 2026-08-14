//go:build integration

package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/auth"
	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/lock"
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

// newKeycloakEnv builds an Env wired with the real OIDC auth strategy pointed at
// the live Keycloak. The issuer is synthesized as "<url>/realms/<realm>" — exactly
// what the KEYCLOAK_* backward-compat shim produces — so this test also proves
// that OIDC discovery against a real Keycloak resolves the same userinfo endpoint
// the pre-refactor code targeted directly.
func newKeycloakEnv(t *testing.T) *Env {
	t.Helper()
	// TokenValidationInterval is left at zero on purpose: every request then
	// reaches Keycloak, so an authorization assertion here can never pass on a
	// cached decision. The caching behaviour itself is covered by unit tests.
	cfg := &config.ServerConfig{
		StateType: "in-memory",
		OIDC: config.OIDCConfig{
			Enabled:          true,
			IssuerURL:        keycloakBaseURL + "/realms/" + keycloakRealm,
			ClientId:         keycloakClientID,
			PrivilegedGroups: []string{"privileged"},
		},
	}

	oidcService, err := auth.NewOIDCAuthService(cfg)
	require.NoError(t, err)

	lockdown, err := NewLockdown("", lock.NewInMemoryDeployLockStore())
	require.NoError(t, err)

	// Register under both headers, mirroring production wiring (NewEnv).
	strategies := map[string]auth.AuthStrategy{
		oidcHeader:           oidcService,
		legacyKeycloakHeader: oidcService,
	}

	return &Env{
		config:        cfg,
		lockdown:      lockdown,
		strategies:    strategies,
		authenticator: auth.NewAuthenticator(strategies),
	}
}

// deployLockServer exposes the OIDC-gated deploy-lock handlers over real HTTP so
// the test drives the full request → requireOIDCAuth → provider userinfo path.
func deployLockServer(t *testing.T, env *Env) *httptest.Server {
	t.Helper()
	router := chi.NewRouter()
	router.Route("/api/v1", func(r chi.Router) {
		r.Post("/deploy-lock", env.SetDeployLock)
		r.Delete("/deploy-lock", env.ReleaseDeployLock)
	})

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv
}

// callDeployLock issues a deploy-lock request, optionally carrying a token in the
// deprecated Keycloak-Authorization header — proving the legacy header still
// authenticates end to end — and returns the HTTP status code.
func callDeployLock(t *testing.T, srv *httptest.Server, method, token string) int {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+"/api/v1/deploy-lock", nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set(legacyKeycloakHeader, "Bearer "+token)
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
// Status mapping reflects current server behavior (requireOIDCAuth, router.go):
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

// protectedReadServer exposes one authenticated read behind the real middleware.
// /version is used because it needs no state or ArgoCD wiring, keeping the test
// focused on the authentication decision.
func protectedReadServer(t *testing.T, env *Env) *httptest.Server {
	t.Helper()
	router := chi.NewRouter()
	router.Route("/api/v1", func(r chi.Router) {
		r.With(env.requireAuthenticatedRead()).Get("/version", env.getVersion)
	})

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv
}

func callProtectedRead(t *testing.T, srv *httptest.Server, token string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/version", nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set(oidcHeader, "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// TestKeycloakReadAuthn proves against a real provider that read access is gated
// on authentication alone. The non-privileged user is the case unit tests cannot
// fully vouch for: the very token Keycloak issues for a user outside
// OIDC_PRIVILEGED_GROUPS — rejected by the deploy-lock endpoints above — must be
// accepted here, or enabling OIDC would lock ordinary users out of the UI.
func TestKeycloakReadAuthn(t *testing.T) {
	waitForKeycloak(t)
	srv := protectedReadServer(t, newKeycloakEnv(t))

	t.Run("a non-privileged user may read", func(t *testing.T) {
		token := keycloakToken(t, "regular-user", "regular-pass")
		assert.Equal(t, http.StatusOK, callProtectedRead(t, srv, token))
	})

	t.Run("a privileged user may read", func(t *testing.T) {
		token := keycloakToken(t, "priv-user", "priv-pass")
		assert.Equal(t, http.StatusOK, callProtectedRead(t, srv, token))
	})

	t.Run("a garbage token is rejected", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, callProtectedRead(t, srv, "not-a-real-token"))
	})

	t.Run("no token is rejected", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, callProtectedRead(t, srv, ""))
	})
}

// TestKeycloakWebSocketHandshake proves the browser's transport against a real
// provider: a token Keycloak issued, offered as a subprotocol rather than a header,
// establishes the socket — and the server echoes only the plain protocol name, which is
// what a browser needs to keep the connection.
func TestKeycloakWebSocketHandshake(t *testing.T) {
	waitForKeycloak(t)

	env := newKeycloakEnv(t)
	env.config.StaticFilePath = t.TempDir()
	env.config.DevEnvironment = true // accept the httptest origin
	srv := httptest.NewServer(env.CreateRouter())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		env.Shutdown(ctx)
		srv.Close()
		connectionsMutex.Lock()
		connections = nil
		connectionsMutex.Unlock()
	})

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	t.Run("a token offered as a subprotocol establishes the socket", func(t *testing.T) {
		token := keycloakToken(t, "regular-user", "regular-pass")

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
			Subprotocols: []string{wsSubprotocol, wsTokenSubprotocolPrefix + token},
		})
		require.NoError(t, err)
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "test done") }()

		assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
		assert.Equal(t, wsSubprotocol, resp.Header.Get("Sec-WebSocket-Protocol"))
	})

	t.Run("a handshake with no credential is refused", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		conn, resp, err := websocket.Dial(ctx, wsURL, nil)
		if conn != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "test done")
		}

		require.Error(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
}
