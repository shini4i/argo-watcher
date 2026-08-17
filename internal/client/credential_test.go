package client

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/models"
)

const testJWT = "eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjF9.signature"

// The "Bearer " prefix is normalized away so the wire format never depends on how
// BEARER_TOKEN was set.
func TestCredentialFrom(t *testing.T) {
	cases := []struct {
		name   string
		config *Config
		want   credential
	}{
		{
			name:   "no credential configured",
			config: &Config{},
			want:   credential{},
		},
		{
			name:   "deploy token",
			config: &Config{Token: "s3cr3t-deploy-token"},
			want:   credential{header: deployTokenHeader, value: "s3cr3t-deploy-token"},
		},
		{
			name:   "raw JWT",
			config: &Config{JsonWebToken: testJWT},
			want:   credential{header: jwtHeader, value: testJWT},
		},
		{
			name:   "Bearer-prefixed JWT is normalized",
			config: &Config{JsonWebToken: "Bearer " + testJWT},
			want:   credential{header: jwtHeader, value: testJWT},
		},
		{
			name:   "JWT wins over a deploy token",
			config: &Config{JsonWebToken: testJWT, Token: "s3cr3t-deploy-token"},
			want:   credential{header: jwtHeader, value: testJWT},
		},
		{
			name:   "a deploy token that looks like a JWT is sent verbatim",
			config: &Config{Token: "Bearer not-a-jwt"},
			want:   credential{header: deployTokenHeader, value: "Bearer not-a-jwt"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, credentialFrom(tc.config))
		})
	}
}

// TestReadsCarryCredential is the point of the feature: the reads a deployment
// performs — the status poll that runs for its whole length, and the config lookup
// behind the app URL — must present the same credential the submission does, so a
// server with OIDC_REQUIRE_TASK_READ_AUTH set still serves this client.
func TestReadsCarryCredential(t *testing.T) {
	cases := []struct {
		name       string
		config     *Config
		wantHeader string
		wantValue  string
	}{
		{"deploy token", &Config{Token: "s3cr3t-deploy-token"}, deployTokenHeader, "s3cr3t-deploy-token"},
		{"JWT", &Config{JsonWebToken: testJWT}, jwtHeader, testJWT},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen := map[string]string{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen[r.URL.Path] = r.Header.Get(tc.wantHeader)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(models.TaskStatus{Status: models.StatusDeployedMessage, Id: taskId})
			}))
			defer srv.Close()

			tc.config.Url = srv.URL
			tc.config.Timeout = 30 * time.Second
			watcher := setupWatcher(tc.config)

			_, err := watcher.getTaskStatus(taskId)
			require.NoError(t, err)
			_, err = watcher.getWatcherConfig()
			require.NoError(t, err)

			assert.Equal(t, tc.wantValue, seen["/api/v1/tasks/"+taskId], "the status poll must carry the credential")
			assert.Equal(t, tc.wantValue, seen["/api/v1/config"], "the config lookup must carry the credential")
		})
	}
}

// TestCredentialDroppedOnCrossHostRedirect covers the asymmetry between the two
// credentials: net/http strips Authorization when a redirect leaves the original host,
// but not a custom header, so the deploy token would otherwise be handed to whatever
// host argo-watcher's URL redirects to — on every poll, for the whole deployment.
//
// Note the vocabulary these tests keep apart: a deploy token is dropped on any change of
// URL.Host, the PORT INCLUDED, because this client deletes it; Authorization is dropped
// only when the hostname changes, since net/http compares hostnames with the port
// excluded. The subtests below are port-only changes between two 127.0.0.1 servers, which
// is the weaker of the two conditions and therefore the one worth pinning here.
func TestCredentialDroppedOnCrossHostRedirect(t *testing.T) {
	body := func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(models.TaskStatus{Status: models.StatusDeployedMessage, Id: taskId})
	}

	t.Run("drops the deploy token when only the port changes", func(t *testing.T) {
		var elsewhereSaw string
		elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			elsewhereSaw = r.Header.Get(deployTokenHeader)
			body(w)
		}))
		defer elsewhere.Close()

		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, elsewhere.URL, http.StatusFound)
		}))
		defer origin.Close()

		watcher := setupWatcher(&Config{Url: origin.URL, Token: "s3cr3t-deploy-token", Timeout: 30 * time.Second})

		_, err := watcher.getTaskStatus(taskId)

		require.NoError(t, err)
		assert.Empty(t, elsewhereSaw, "the deploy token must not follow a redirect to another host")
	})

	t.Run("keeps the deploy token on a same-host redirect", func(t *testing.T) {
		var seen string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/moved" {
				http.Redirect(w, r, "/moved", http.StatusFound)
				return
			}
			seen = r.Header.Get(deployTokenHeader)
			body(w)
		}))
		defer srv.Close()

		watcher := setupWatcher(&Config{Url: srv.URL, Token: "s3cr3t-deploy-token", Timeout: 30 * time.Second})

		_, err := watcher.getTaskStatus(taskId)

		require.NoError(t, err)
		assert.Equal(t, "s3cr3t-deploy-token", seen)
	})
}

// TestCrossHostRedirectIsReported covers the diagnosability of the drop above. A
// credential lost to a redirect turns off git write-back, and the resulting failure blames
// the image or the timeout rather than the credential, so the client says so at the point
// where the cause is still known.
func TestCrossHostRedirectIsReported(t *testing.T) {
	body := func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(models.TaskStatus{Status: models.StatusDeployedMessage, Id: taskId})
	}

	// Restores whatever writer was installed, so a nested capture is not discarded.
	captureLog := func(t *testing.T, fn func()) string {
		t.Helper()
		var buf bytes.Buffer
		original := log.Writer()
		log.SetOutput(&buf)
		t.Cleanup(func() { log.SetOutput(original) })
		fn()
		return buf.String()
	}

	redirectTo := func(t *testing.T, target string) *httptest.Server {
		t.Helper()
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target, http.StatusFound)
		}))
	}

	t.Run("warns when a port-only change drops the deploy token", func(t *testing.T) {
		elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			body(w)
		}))
		defer elsewhere.Close()

		origin := redirectTo(t, elsewhere.URL)
		defer origin.Close()

		watcher := setupWatcher(&Config{Url: origin.URL, Token: "s3cr3t-deploy-token", Timeout: 30 * time.Second})

		output := captureLog(t, func() {
			_, err := watcher.getTaskStatus(taskId)
			require.NoError(t, err)
		})

		assert.Contains(t, output, "git write-back")
		// Both hosts are named so the operator can see which hop dropped it.
		assert.Contains(t, output, mustHost(t, origin.URL))
		assert.Contains(t, output, mustHost(t, elsewhere.URL))
		// The secret itself must never reach a CI log.
		assert.NotContains(t, output, "s3cr3t-deploy-token")
	})

	t.Run("stays quiet on a same-host redirect", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/moved" {
				http.Redirect(w, r, "/moved", http.StatusFound)
				return
			}
			body(w)
		}))
		defer srv.Close()

		watcher := setupWatcher(&Config{Url: srv.URL, Token: "s3cr3t-deploy-token", Timeout: 30 * time.Second})

		output := captureLog(t, func() {
			_, err := watcher.getTaskStatus(taskId)
			require.NoError(t, err)
		})

		assert.Empty(t, output)
	})

	// A client with no credential expects no write-back, so a host change costs it
	// nothing and a warning would be noise on every poll.
	t.Run("stays quiet when no credential is configured", func(t *testing.T) {
		elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			body(w)
		}))
		defer elsewhere.Close()

		origin := redirectTo(t, elsewhere.URL)
		defer origin.Close()

		watcher := setupWatcher(&Config{Url: origin.URL, Timeout: 30 * time.Second})

		output := captureLog(t, func() {
			_, err := watcher.getTaskStatus(taskId)
			require.NoError(t, err)
		})

		assert.Empty(t, output)
	})

	// net/http strips Authorization when the redirect leaves the hostname, so the JWT is
	// already gone by the time this hook runs. The warning must still fire.
	t.Run("warns for a JWT when the hostname changes", func(t *testing.T) {
		var jwtSeen string
		elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			jwtSeen = r.Header.Get(jwtHeader)
			body(w)
		}))
		defer elsewhere.Close()

		// 127.0.0.1 -> localhost: a real hostname change (neither is a subdomain of the
		// other), which is what makes net/http drop Authorization. Both are loopback.
		origin := redirectTo(t, "http://localhost:"+mustPort(t, elsewhere.URL)+"/")
		defer origin.Close()

		watcher := setupWatcher(&Config{Url: origin.URL, JsonWebToken: testJWT, Timeout: 30 * time.Second})

		output := captureLog(t, func() {
			_, err := watcher.getTaskStatus(taskId)
			require.NoError(t, err)
		})

		require.Empty(t, jwtSeen, "net/http must have dropped the JWT for this case to be meaningful")
		assert.Contains(t, output, "git write-back")
		assert.NotContains(t, output, testJWT)
	})

	// net/http compares hostnames with the port excluded, so a port-only redirect keeps
	// Authorization and write-back still works. Warning here would be a false diagnosis.
	t.Run("stays quiet for a JWT when only the port changes", func(t *testing.T) {
		var jwtSeen string
		elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			jwtSeen = r.Header.Get(jwtHeader)
			body(w)
		}))
		defer elsewhere.Close()

		origin := redirectTo(t, elsewhere.URL) // both 127.0.0.1, differing ports
		defer origin.Close()

		watcher := setupWatcher(&Config{Url: origin.URL, JsonWebToken: testJWT, Timeout: 30 * time.Second})

		output := captureLog(t, func() {
			_, err := watcher.getTaskStatus(taskId)
			require.NoError(t, err)
		})

		require.Equal(t, testJWT, jwtSeen, "net/http keeps Authorization across a port-only change")
		assert.Empty(t, output, "the credential survived, so there is nothing to warn about")
	})

	// A deployment polls the same redirect for its whole length, so the warning is capped
	// at one per run. Without the cap a long rollout buries its own CI log; with the cap
	// hoisted to a package-level var it would fire once per process and go missing for
	// every later Watcher — hence one assertion per direction.
	t.Run("warns once per run, not once per request", func(t *testing.T) {
		elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			body(w)
		}))
		defer elsewhere.Close()

		origin := redirectTo(t, elsewhere.URL)
		defer origin.Close()

		watcher := setupWatcher(&Config{Url: origin.URL, Token: "s3cr3t-deploy-token", Timeout: 30 * time.Second})

		output := captureLog(t, func() {
			_, err := watcher.getTaskStatus(taskId)
			require.NoError(t, err)
			_, err = watcher.getTaskStatus(taskId)
			require.NoError(t, err)
		})

		assert.Equal(t, 1, strings.Count(output, "without git write-back"),
			"two credential-losing requests on one watcher must warn once")

		// A second watcher is a second run: suppression must not be process-global.
		second := setupWatcher(&Config{Url: origin.URL, Token: "s3cr3t-deploy-token", Timeout: 30 * time.Second})
		secondOutput := captureLog(t, func() {
			_, err := second.getTaskStatus(taskId)
			require.NoError(t, err)
		})
		assert.Equal(t, 1, strings.Count(secondOutput, "without git write-back"),
			"a fresh watcher must warn again")
	})

	// Both credentials set: credentialFrom prefers the JWT, so apply never sends the
	// deploy-token header and the Del above is a no-op. Only the JWT's survival decides
	// whether a warning is truthful — hard-coding deployTokenHeader in the gate would
	// reintroduce the false warning on a port-only change.
	t.Run("follows the preferred credential when both are configured", func(t *testing.T) {
		both := func(url string) *Config {
			return &Config{
				Url:          url,
				JsonWebToken: testJWT,
				Token:        "s3cr3t-deploy-token",
				Timeout:      30 * time.Second,
			}
		}

		t.Run("quiet on a port-only change", func(t *testing.T) {
			var jwtSeen, tokenSeen string
			elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				jwtSeen, tokenSeen = r.Header.Get(jwtHeader), r.Header.Get(deployTokenHeader)
				body(w)
			}))
			defer elsewhere.Close()

			origin := redirectTo(t, elsewhere.URL)
			defer origin.Close()

			watcher := setupWatcher(both(origin.URL))
			output := captureLog(t, func() {
				_, err := watcher.getTaskStatus(taskId)
				require.NoError(t, err)
			})

			require.Equal(t, testJWT, jwtSeen, "the JWT survives a port-only change")
			assert.Empty(t, tokenSeen, "the deploy token is never sent when a JWT is configured")
			assert.Empty(t, output)
		})

		t.Run("warns on a hostname change", func(t *testing.T) {
			var jwtSeen string
			elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				jwtSeen = r.Header.Get(jwtHeader)
				body(w)
			}))
			defer elsewhere.Close()

			origin := redirectTo(t, "http://localhost:"+mustPort(t, elsewhere.URL)+"/")
			defer origin.Close()

			watcher := setupWatcher(both(origin.URL))
			output := captureLog(t, func() {
				_, err := watcher.getTaskStatus(taskId)
				require.NoError(t, err)
			})

			require.Empty(t, jwtSeen, "net/http drops the JWT across a hostname change")
			assert.Contains(t, output, "git write-back")
			assert.NotContains(t, output, testJWT)
			assert.NotContains(t, output, "s3cr3t-deploy-token")
		})
	})
}

// TestDropCredentialOnHostChangeRedirectLimit drives the hook directly rather than through
// httptest, so the redirect-limit arm is not paid for at getJSON's transient-retry
// backoff. Setting CheckRedirect replaces net/http's own limit, making this branch the only
// thing bounding a redirect loop — and it must return before the credential is touched, or
// a request that is never sent would still report write-back as lost.
func TestDropCredentialOnHostChangeRedirectLimit(t *testing.T) {
	newRequest := func(t *testing.T, rawURL string) *http.Request {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		require.NoError(t, err)
		return req
	}

	via := func(t *testing.T, n int) []*http.Request {
		t.Helper()
		chain := make([]*http.Request, n)
		for i := range chain {
			chain[i] = newRequest(t, "http://origin.example.com/")
		}
		return chain
	}

	capture := func(t *testing.T, fn func()) string {
		t.Helper()
		var buf bytes.Buffer
		original := log.Writer()
		log.SetOutput(&buf)
		t.Cleanup(func() { log.SetOutput(original) })
		fn()
		return buf.String()
	}

	t.Run("stops at the limit without touching the credential", func(t *testing.T) {
		watcher := setupWatcher(&Config{Url: "http://origin.example.com", Token: "s3cr3t-deploy-token", Timeout: time.Second})
		request := newRequest(t, "http://elsewhere.example.com/")
		request.Header.Set(deployTokenHeader, "s3cr3t-deploy-token")

		var err error
		output := capture(t, func() {
			err = watcher.dropCredentialOnHostChange(request, via(t, maxRedirects))
		})

		require.EqualError(t, err, "stopped after 10 redirects")
		assert.Equal(t, "s3cr3t-deploy-token", request.Header.Get(deployTokenHeader),
			"the guard must return before the credential is deleted")
		assert.Empty(t, output, "a request that is never sent must not report lost write-back")
	})

	t.Run("the last permitted hop still strips and warns", func(t *testing.T) {
		watcher := setupWatcher(&Config{Url: "http://origin.example.com", Token: "s3cr3t-deploy-token", Timeout: time.Second})
		request := newRequest(t, "http://elsewhere.example.com/")
		request.Header.Set(deployTokenHeader, "s3cr3t-deploy-token")

		var err error
		output := capture(t, func() {
			err = watcher.dropCredentialOnHostChange(request, via(t, maxRedirects-1))
		})

		require.NoError(t, err)
		assert.Empty(t, request.Header.Get(deployTokenHeader))
		assert.Contains(t, output, "without git write-back")
	})
}

func mustPort(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	return parsed.Port()
}

func mustHost(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	return parsed.Host
}

func TestReadsWithoutCredentialStayBare(t *testing.T) {
	var headers http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(models.TaskStatus{Status: models.StatusDeployedMessage, Id: taskId})
	}))
	defer srv.Close()

	watcher := setupWatcher(&Config{Url: srv.URL, Timeout: 30 * time.Second})

	_, err := watcher.getTaskStatus(taskId)
	require.NoError(t, err)

	assert.Empty(t, headers.Get(jwtHeader))
	assert.Empty(t, headers.Get(deployTokenHeader))
}
