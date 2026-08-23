package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/lock"
)

// newHeaderRouter builds a router over a static directory holding an index.html and
// a swagger page, so the SPA fallback and the swagger route both answer 200.
func newHeaderRouter(t *testing.T, cfg config.ServerConfig) *chi.Mux {
	t.Helper()

	staticPath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(staticPath, "index.html"), []byte("<html></html>"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(staticPath, "swagger"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(staticPath, "swagger", "index.html"), []byte("<html></html>"), 0o600))

	cfg.StaticFilePath = staticPath
	env := &Env{config: &cfg}

	var err error
	env.lockdown, err = NewLockdown("", lock.NewInMemoryDeployLockStore())
	require.NoError(t, err)

	return env.CreateRouter()
}

// cspDirectives parses a policy into directive name -> source list.
func cspDirectives(t *testing.T, policy string) map[string]string {
	t.Helper()
	require.NotEmpty(t, policy, "no Content-Security-Policy header")

	directives := map[string]string{}
	for _, directive := range strings.Split(policy, ";") {
		name, sources, _ := strings.Cut(strings.TrimSpace(directive), " ")
		if name != "" {
			directives[name] = strings.TrimSpace(sources)
		}
	}
	return directives
}

// assertSecurityHeaders asserts the three single-purpose headers and that a policy
// was set at all. Its content is pinned by TestContentSecurityPolicyDirectives.
func assertSecurityHeaders(t *testing.T, header http.Header) {
	t.Helper()
	assert.NotEmpty(t, header.Get("Content-Security-Policy"))
	assert.Equal(t, "nosniff", header.Get("X-Content-Type-Options"))
	// SAMEORIGIN, not DENY: a silent token renewal with no refresh token falls back
	// to an iframe of this application's own origin.
	assert.Equal(t, "SAMEORIGIN", header.Get("X-Frame-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", header.Get("Referrer-Policy"))
}

func TestSecurityHeadersOnEveryRoute(t *testing.T) {
	router := newHeaderRouter(t, config.ServerConfig{})

	// One request per kind of response the server produces: a probe, an API payload,
	// the metrics endpoint, a served file, the SPA fallback, the swagger page, and
	// the websocket route — the one route the CORS gate deliberately exempts.
	routes := map[string]int{
		"/livez":                 http.StatusOK,
		"/api/v1/config":         http.StatusOK,
		"/metrics":               http.StatusOK,
		"/index.html":            http.StatusOK,
		"/deep/link/into/the/ui": http.StatusOK,
		"/swagger/":              http.StatusOK,
		"/ws":                    http.StatusBadRequest,
	}

	for path, status := range routes {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, status, w.Code)
			assertSecurityHeaders(t, w.Header())
		})
	}
}

func TestSecurityHeadersOnPanic(t *testing.T) {
	// The middleware sets the headers before delegating, which is what carries them
	// onto a response it never sees written.
	env := &Env{config: &config.ServerConfig{}}

	router := chi.NewRouter()
	router.Use(middleware.Recoverer)
	router.Use(env.securityHeaders())
	router.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("boom") })

	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	assertSecurityHeaders(t, w.Header())
}

func TestSecurityHeadersOnRefusedRequest(t *testing.T) {
	// The origin allowlist is only non-empty in a dev environment; production allows
	// any origin, so nothing to refuse there.
	router := newHeaderRouter(t, config.ServerConfig{DevEnvironment: true})

	// The CORS gate answers before any handler; a response it writes itself must still
	// carry the headers.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
	assertSecurityHeaders(t, w.Header())
}

func TestContentSecurityPolicyDirectives(t *testing.T) {
	router := newHeaderRouter(t, config.ServerConfig{})

	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	req.Host = "watcher.example.com:8080"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	directives := cspDirectives(t, w.Header().Get("Content-Security-Policy"))

	// Asserted whole, not by substring: for a security policy the source list IS the
	// contract, and a widened one must fail here.
	assert.Equal(t, map[string]string{
		"default-src": "'self'",
		"script-src":  "'self'",
		"object-src":  "'none'",
		"base-uri":    "'self'",
		"form-action": "'self'",
		// 'self' rather than 'none': a silent token renewal without a refresh token
		// frames this application's own origin.
		"frame-ancestors": "'self'",
		"frame-src":       "'self'",
		// emotion (MUI) and swagger-ui both inject stylesheets and style attributes,
		// and the Web UI loads its font from Google Fonts.
		"style-src": "'self' 'unsafe-inline' https://fonts.googleapis.com",
		"font-src":  "'self' https://fonts.gstatic.com",
		// Avatars come from the OIDC `picture` claim or Gravatar, and swagger-ui's
		// stylesheet embeds data: images.
		"img-src": "'self' data: https:",
		// A provider may serve its token and userinfo endpoints off other hosts. The
		// handshake host is named explicitly because 'self' does not resolve to a
		// websocket scheme in every browser.
		"connect-src": "'self' https: ws://watcher.example.com:8080 wss://watcher.example.com:8080",
	}, directives)

	assert.NotContains(t, w.Header().Get("Content-Security-Policy"), "unsafe-eval")
}

func TestContentSecurityPolicyRejectsHostileHost(t *testing.T) {
	router := newHeaderRouter(t, config.ServerConfig{})

	// Go accepts ";" and "'" in a Host header, so an unvalidated host would let a
	// request append its own directives to the policy it is served.
	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	req.Host = "evil.example.com;script-src 'unsafe-inline'"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	policy := w.Header().Get("Content-Security-Policy")
	directives := cspDirectives(t, policy)

	assert.Equal(t, "'self'", directives["script-src"])
	assert.NotContains(t, policy, "evil.example.com")
	// The websocket sources are dropped rather than guessed.
	assert.NotContains(t, directives["connect-src"], "ws://")
}

func TestContentSecurityPolicyAllowsTheIssuer(t *testing.T) {
	// An issuer on plain http matches neither 'self' nor https:, and the browser
	// fetches discovery, the token exchange and userinfo from it. The lab's Keycloak
	// (web/e2e) is exactly that shape.
	router := newHeaderRouter(t, config.ServerConfig{
		OIDC: config.OIDCConfig{Enabled: true, IssuerURL: "http://localhost:8090/realms/argo-watcher-e2e"},
	})

	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	directives := cspDirectives(t, w.Header().Get("Content-Security-Policy"))
	assert.Contains(t, directives["connect-src"], "http://localhost:8090")
	assert.Contains(t, directives["frame-src"], "http://localhost:8090")
	// The path is not part of an origin.
	assert.NotContains(t, w.Header().Get("Content-Security-Policy"), "realms")
}

func TestContentSecurityPolicyFrameSrc(t *testing.T) {
	tests := []struct {
		name  string
		oidc  config.OIDCConfig
		frame string
	}{
		{
			name:  "without OIDC only this origin may be framed",
			oidc:  config.OIDCConfig{},
			frame: "'self'",
		},
		{
			// A deployment that turned OIDC off without clearing the issuer must not
			// widen the policy to a third-party origin with authentication disabled.
			name:  "an issuer left behind with OIDC off is ignored",
			oidc:  config.OIDCConfig{IssuerURL: "https://sso.example.com/realms/watcher"},
			frame: "'self'",
		},
		{
			// A silent renewal without a refresh token navigates an iframe to the
			// provider's authorization endpoint, which lives on the issuer origin.
			name:  "with OIDC the issuer origin is allowed",
			oidc:  config.OIDCConfig{Enabled: true, IssuerURL: " https://sso.example.com/realms/watcher "},
			frame: "'self' https://sso.example.com",
		},
		{
			name:  "an unusable issuer is left out",
			oidc:  config.OIDCConfig{Enabled: true, IssuerURL: "not a url"},
			frame: "'self'",
		},
		{
			name:  "an issuer host that cannot be spelled in a policy is left out",
			oidc:  config.OIDCConfig{Enabled: true, IssuerURL: "https://sso.example.com;script-src 'unsafe-inline'"},
			frame: "'self'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newHeaderRouter(t, config.ServerConfig{OIDC: tt.oidc})

			req := httptest.NewRequest(http.MethodGet, "/livez", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			policy := w.Header().Get("Content-Security-Policy")
			directives := cspDirectives(t, policy)
			assert.Equal(t, tt.frame, directives["frame-src"])
			assert.Equal(t, "'self'", directives["script-src"])
			// The issuer feeds connect-src too, so frame-src alone would not prove a
			// rejected issuer stayed out of the policy.
			if tt.frame == "'self'" {
				assert.NotContains(t, policy, "sso.example.com")
			}
		})
	}
}

func TestIsPolicySafeHost(t *testing.T) {
	// The sole barrier between an attacker-controlled Host header and the policy this
	// server serves, so its accepted set is pinned rather than described.
	for _, host := range []string{"host", "host.example.com:8080", "[::1]:8080", "127.0.0.1:30080"} {
		assert.True(t, isPolicySafeHost(host), host)
	}

	for _, host := range []string{"", "a;b", "a'b", "a b", "a,b", "a/b", "a\"b", "a*b", "host\n"} {
		assert.False(t, isPolicySafeHost(host), host)
	}
}
