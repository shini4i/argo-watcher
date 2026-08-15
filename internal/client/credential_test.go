package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
func TestCredentialDroppedOnCrossHostRedirect(t *testing.T) {
	body := func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(models.TaskStatus{Status: models.StatusDeployedMessage, Id: taskId})
	}

	t.Run("drops the deploy token when the host changes", func(t *testing.T) {
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
