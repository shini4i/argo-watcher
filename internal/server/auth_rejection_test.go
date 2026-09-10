package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/auth"
	"github.com/shini4i/argo-watcher/internal/models"
)

// authGate is one of the three surfaces that turn a credential verdict into a
// response: the privileged-action check, the read middleware and the WebSocket
// handshake. They answer the same three states and must not drift apart.
type authGate struct {
	name string
	// allow reports whether the gate let the request through, having written the
	// rejection itself when it did not.
	allow func(env *Env, w http.ResponseWriter, r *http.Request) bool
}

func authGates() []authGate {
	return []authGate{
		{
			name:  "privileged action",
			allow: func(env *Env, w http.ResponseWriter, r *http.Request) bool { return env.requireOIDCAuth(w, r) },
		},
		{
			name: "read middleware",
			allow: func(env *Env, w http.ResponseWriter, r *http.Request) bool {
				passed := false
				next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { passed = true })
				env.requireAuthenticatedRead()(next).ServeHTTP(w, r)
				return passed
			},
		},
		{
			name:  "websocket handshake",
			allow: func(env *Env, w http.ResponseWriter, r *http.Request) bool { return env.authorizeWebSocket(w, r) },
		},
	}
}

// TestAuthRejectionContract pins the status and client-facing status text every
// gate must produce for the same credential state. The three share one rejection
// path; this is what that path owes the caller.
func TestAuthRejectionContract(t *testing.T) {
	cases := []struct {
		name           string
		strategy       *oidcLikeStrategy
		sendCredential bool
		expectedStatus int
		expectedText   string
		// expectedReason separates the helper's two 401 branches: a caller that sent
		// nothing is told how to present a credential, one whose credential was
		// refused is told why.
		expectedReason string
	}{
		{
			name:           "no credential",
			strategy:       &oidcLikeStrategy{},
			expectedStatus: http.StatusUnauthorized,
			expectedText:   unauthorizedMessage,
			expectedReason: "authentication required",
		},
		{
			name:           "credential rejected",
			strategy:       &oidcLikeStrategy{},
			sendCredential: true,
			expectedStatus: http.StatusUnauthorized,
			expectedText:   unauthorizedMessage,
			expectedReason: "token validation failed",
		},
		{
			name:           "provider unavailable",
			strategy:       &oidcLikeStrategy{unavailable: true},
			sendCredential: true,
			expectedStatus: http.StatusServiceUnavailable,
			expectedText:   providerUnavailableMessage,
			expectedReason: auth.ErrProviderUnavailable.Error(),
		},
	}

	for _, tc := range cases {
		for _, gate := range authGates() {
			t.Run(tc.name+"/"+gate.name, func(t *testing.T) {
				env, _ := readAuthEnv(t, true, map[string]auth.AuthStrategy{oidcHeader: *tc.strategy})

				request := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
				if tc.sendCredential {
					request.Header.Set(oidcHeader, "token")
				}
				recorder := httptest.NewRecorder()

				assert.False(t, gate.allow(env, recorder, request))
				assert.Equal(t, tc.expectedStatus, recorder.Code)

				var body models.TaskStatus
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
				assert.Equal(t, tc.expectedText, body.Status)
				assert.Contains(t, body.Error, tc.expectedReason, "a rejection must say why")
			})
		}
	}
}

// TestAuthRejectionNamesTheCredentialToSend pins the part the gates deliberately
// do not share: an uncredentialed caller is told how to present one on the
// transport it is using, which for a browser WebSocket is not a header.
func TestAuthRejectionNamesTheCredentialToSend(t *testing.T) {
	env, _ := readAuthEnv(t, true, map[string]auth.AuthStrategy{oidcHeader: oidcLikeStrategy{}})

	headerRecorder := httptest.NewRecorder()
	require.False(t, env.requireOIDCAuth(headerRecorder, httptest.NewRequest(http.MethodGet, "/", nil)))
	assert.Contains(t, headerRecorder.Body.String(), oidcHeader)

	wsRecorder := httptest.NewRecorder()
	require.False(t, env.authorizeWebSocket(wsRecorder, httptest.NewRequest(http.MethodGet, "/ws", nil)))
	assert.Contains(t, wsRecorder.Body.String(), wsTokenSubprotocolPrefix)
}

// TestWebSocketRejectionCarriesTheStrategyReason pins that a rejected handshake
// reports why, as the HTTP gates already did. It previously answered every 401
// with the subprotocol hint, which misleads a client that did offer a credential.
func TestWebSocketRejectionCarriesTheStrategyReason(t *testing.T) {
	env, _ := readAuthEnv(t, true, map[string]auth.AuthStrategy{oidcHeader: oidcLikeStrategy{}})

	request := httptest.NewRequest(http.MethodGet, "/ws", nil)
	request.Header.Set(oidcHeader, "token")
	recorder := httptest.NewRecorder()

	require.False(t, env.authorizeWebSocket(recorder, request))

	var body models.TaskStatus
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.Contains(t, body.Error, "token validation failed")
	assert.NotContains(t, body.Error, wsTokenSubprotocolPrefix)
}
