package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/auth"
)

// wsAuthServer serves the full router over real HTTP, which the handshake needs for
// hijacking, and returns the ws:// URL of /ws.
func wsAuthServer(t *testing.T, oidcEnabled bool, strategies map[string]auth.AuthStrategy) (*Env, string) {
	t.Helper()

	connectionsMutex.Lock()
	connections = nil
	connectionsMutex.Unlock()

	env, _ := readAuthEnv(t, oidcEnabled, strategies)
	env.config.DevEnvironment = true // accept the httptest origin
	server := httptest.NewServer(env.CreateRouter())

	t.Cleanup(func() {
		shutdownEnv(env)
		server.Close()
		connectionsMutex.Lock()
		connections = nil
		connectionsMutex.Unlock()
	})

	return env, "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
}

func dialWS(t *testing.T, url string, opts *websocket.DialOptions) (int, string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, url, opts)
	if conn != nil {
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "test done") }()
	}

	status := 0
	subprotocol := ""
	if resp != nil {
		status = resp.StatusCode
		subprotocol = resp.Header.Get("Sec-WebSocket-Protocol")
	}
	if err == nil {
		require.Equal(t, http.StatusSwitchingProtocols, status)
	}

	return status, subprotocol
}

func activeConnections() int {
	connectionsMutex.RLock()
	defer connectionsMutex.RUnlock()
	return len(connections)
}

func TestWebSocketAuthDisabled(t *testing.T) {
	_, url := wsAuthServer(t, false, nil)

	status, _ := dialWS(t, url, nil)

	assert.Equal(t, http.StatusSwitchingProtocols, status)
}

// TestWebSocketAuthRejectsUncredentialed covers the gap this change closes: with OIDC
// enabled, an anonymous socket could read every deploy-lock and reachability
// transition even though the REST equivalents required a credential.
func TestWebSocketAuthRejectsUncredentialed(t *testing.T) {
	env, url := wsAuthServer(t, true, map[string]auth.AuthStrategy{
		oidcHeader: oidcLikeStrategy{authenticated: true},
	})

	status, _ := dialWS(t, url, nil)

	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Zero(t, activeConnections(), "a rejected handshake must not register a connection")

	// The rejection happens before the hijack, so shutdown has nothing to drain.
	shutdownEnv(env)
}

// TestAuthorizeWebSocket covers the credential matrix — including the header path used by
// non-browser clients such as the CLI and wsprobe — without opening sockets: closing
// an established connection waits out a close-handshake timeout, so a real handshake per
// case would dominate the package's runtime. The two tests around this one prove the
// decision is actually wired into the handshake.
func TestAuthorizeWebSocket(t *testing.T) {

	strategies := map[string]auth.AuthStrategy{
		oidcHeader:                  oidcLikeStrategy{authenticated: true},
		"ARGO_WATCHER_DEPLOY_TOKEN": auth.NewDeployTokenAuthService("deploy-token"),
	}

	cases := map[string]struct {
		header      string
		value       string
		subprotocol string
		want        bool
		wantStatus  int
	}{
		"canonical OIDC header":   {header: oidcHeader, value: "Bearer token", want: true},
		"removed Keycloak header": {header: "Keycloak-Authorization", value: "Bearer token", want: false, wantStatus: http.StatusUnauthorized},
		"deploy token":            {header: "ARGO_WATCHER_DEPLOY_TOKEN", value: "deploy-token", want: true},
		"subprotocol token":       {subprotocol: wsTokenSubprotocolPrefix + "token", want: true},
		"no credential":           {want: false, wantStatus: http.StatusUnauthorized},
		"wrong deploy token": {
			header:     "ARGO_WATCHER_DEPLOY_TOKEN",
			value:      "nope",
			want:       false,
			wantStatus: http.StatusUnauthorized,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			env, _ := readAuthEnv(t, true, strategies)

			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/ws", http.NoBody)
			if tc.header != "" {
				// Set, not a raw map write: only Set canonicalizes the key the way the
				// net/http server does when the header arrives over the wire.
				req.Header.Set(tc.header, tc.value)
			}
			if tc.subprotocol != "" {
				req.Header.Set("Sec-WebSocket-Protocol", wsSubprotocol+", "+tc.subprotocol)
			}

			assert.Equal(t, tc.want, env.authorizeWebSocket(recorder, req))
			if tc.wantStatus != 0 {
				assert.Equal(t, tc.wantStatus, recorder.Code)
			}
		})
	}

	t.Run("passes straight through when OIDC is disabled", func(t *testing.T) {
		env, _ := readAuthEnv(t, false, nil)

		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/ws", http.NoBody)

		assert.True(t, env.authorizeWebSocket(recorder, req))
	})
}

// TestWebSocketAuthAcceptsSubprotocolCredential covers the browser, which cannot set a
// header on a handshake and must pass its token as a subprotocol instead.
func TestWebSocketAuthAcceptsSubprotocolCredential(t *testing.T) {
	_, url := wsAuthServer(t, true, map[string]auth.AuthStrategy{
		oidcHeader: oidcLikeStrategy{authenticated: true},
	})

	status, negotiated := dialWS(t, url, &websocket.DialOptions{
		Subprotocols: []string{wsSubprotocol, wsTokenSubprotocolPrefix + "browser-token"},
	})

	assert.Equal(t, http.StatusSwitchingProtocols, status)
	// The browser fails the connection unless the server echoes one of the offered
	// protocols, and it must never echo the one carrying the token.
	assert.Equal(t, wsSubprotocol, negotiated)
}

func TestWebSocketAuthRejectsBadSubprotocolCredential(t *testing.T) {
	_, url := wsAuthServer(t, true, map[string]auth.AuthStrategy{
		oidcHeader: oidcLikeStrategy{authenticated: false},
	})

	status, _ := dialWS(t, url, &websocket.DialOptions{
		Subprotocols: []string{wsSubprotocol, wsTokenSubprotocolPrefix + "rejected-token"},
	})

	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Zero(t, activeConnections())
}

// TestWebSocketAuthProviderUnavailable keeps the handshake consistent with the reads:
// a provider outage is 503, so a reconnecting tab does not treat it as a dead session.
func TestWebSocketAuthProviderUnavailable(t *testing.T) {
	_, url := wsAuthServer(t, true, map[string]auth.AuthStrategy{
		oidcHeader: oidcLikeStrategy{unavailable: true},
	})

	status, _ := dialWS(t, url, &websocket.DialOptions{
		HTTPHeader: http.Header{oidcHeader: []string{"Bearer token"}},
	})

	assert.Equal(t, http.StatusServiceUnavailable, status)
	assert.Zero(t, activeConnections())
}

// TestWebSocketSubprotocolTokenParsing covers the wire format directly, including the
// shapes a proxy or an older client might produce.
func TestWebSocketSubprotocolTokenParsing(t *testing.T) {
	cases := map[string]struct {
		header string
		want   string
	}{
		"token after the protocol name": {wsSubprotocol + ", " + wsTokenSubprotocolPrefix + "abc", "abc"},
		"token alone":                   {wsTokenSubprotocolPrefix + "abc", "abc"},
		"no token entry":                {wsSubprotocol, ""},
		"empty header":                  {"", ""},
		"prefix with no value":          {wsTokenSubprotocolPrefix, ""},
		"unrelated protocols":           {"chat, superchat", ""},
		"whitespace around entries":     {"  " + wsTokenSubprotocolPrefix + "abc  ", "abc"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodGet, "http://example.com/ws", http.NoBody)
			require.NoError(t, err)
			if tc.header != "" {
				request.Header.Set("Sec-WebSocket-Protocol", tc.header)
			}

			assert.Equal(t, tc.want, wsSubprotocolToken(request))
		})
	}
}
