//go:build integration

package models

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	toxiclient "github.com/Shopify/toxiproxy/v2/client"
	gogit "github.com/go-git/go-git/v5"
	gogitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/stretchr/testify/require"
	cryptossh "golang.org/x/crypto/ssh"
)

// Integration test endpoints. Ports match the docker-compose `integration`
// profile and the equivalent GH Actions `services:` block.
const (
	giteaAPI         = "http://localhost:3000"
	giteaSSHHost     = "localhost"
	giteaSSHPort     = "2222"
	toxiproxyAdmin   = "http://localhost:8474"
	proxiedSSHListen = "0.0.0.0:12222"
	proxiedSSHHost   = "localhost"
	proxiedSSHPort   = "12222"
	toxiproxyUpstrm  = "gitea:22" // resolved on the docker network shared by both services
)

// giteaEnv captures everything a test needs to push to an isolated Gitea repo
// over SSH, with both direct (port 2222) and toxiproxy-fronted (port 12222) URLs.
type giteaEnv struct {
	User          string
	Password      string
	RepoName      string
	DirectRepoURL string // bypasses toxiproxy — used by the competitor writer
	ProxyRepoURL  string // routed through toxiproxy — used by the system under test
	SSHKeyPath    string
}

// setupGitea creates a unique user + SSH key + repo (seeded with apps/.gitkeep)
// in the running Gitea instance. SSH keys live in t.TempDir; Gitea-side state is
// left in the container (ephemeral in CI, prunable via `docker compose down -v` locally).
func setupGitea(t *testing.T) *giteaEnv {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	sshPub, err := cryptossh.NewPublicKey(pub)
	require.NoError(t, err)
	pubKeyStr := strings.TrimSpace(string(cryptossh.MarshalAuthorizedKey(sshPub)))

	pemBlock, err := cryptossh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)
	privKeyPEM := pem.EncodeToMemory(pemBlock)

	keyDir := t.TempDir()
	keyPath := filepath.Join(keyDir, "id_ed25519")
	require.NoError(t, os.WriteFile(keyPath, privKeyPEM, 0600))

	// Unique per-test identifiers to avoid cross-test interference in the
	// shared Gitea instance.
	stamp := time.Now().UnixNano()
	user := fmt.Sprintf("u%d", stamp)
	password := "Password123!" // #nosec G101 — test credential for ephemeral Gitea container only
	repoName := fmt.Sprintf("repo-%d", stamp)

	signupForm := url.Values{
		"user_name": {user},
		"email":     {user + "@test.example"},
		"password":  {password},
		"retype":    {password},
	}
	// /user/sign_up redirects on success; non-redirecting client avoids following into /dashboard.
	cli := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := cli.PostForm(giteaAPI+"/user/sign_up", signupForm)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Less(t, resp.StatusCode, 400, "gitea signup unexpected status %d", resp.StatusCode)

	giteaAPIPost(t, user, password, "/api/v1/user/keys",
		map[string]any{"title": "test", "key": pubKeyStr})

	giteaAPIPost(t, user, password, "/api/v1/user/repos",
		map[string]any{"name": repoName, "auto_init": true, "default_branch": "master"})

	// Seed apps/.gitkeep so the recovery test's hard-reset preserves the dir.
	// Content must be base64. A single newline keeps the file non-empty (Gitea
	// rejects truly empty content via this endpoint).
	giteaAPIPost(t,
		user, password,
		fmt.Sprintf("/api/v1/repos/%s/%s/contents/apps/.gitkeep", user, repoName),
		map[string]any{
			"message": "seed apps directory",
			"content": base64.StdEncoding.EncodeToString([]byte("\n")),
			"branch":  "master",
		})

	return &giteaEnv{
		User:          user,
		Password:      password,
		RepoName:      repoName,
		DirectRepoURL: fmt.Sprintf("ssh://git@%s:%s/%s/%s.git", giteaSSHHost, giteaSSHPort, user, repoName),
		ProxyRepoURL:  fmt.Sprintf("ssh://git@%s:%s/%s/%s.git", proxiedSSHHost, proxiedSSHPort, user, repoName),
		SSHKeyPath:    keyPath,
	}
}

// giteaAPIPost sends a JSON POST to Gitea with basic auth and asserts a 2xx response.
func giteaAPIPost(t *testing.T, user, pass, path string, body map[string]any) {
	t.Helper()
	buf, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, giteaAPI+path, strings.NewReader(string(buf)))
	require.NoError(t, err)
	req.SetBasicAuth(user, pass)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Less(t, resp.StatusCode, 300, "gitea %s returned %d", path, resp.StatusCode)
}

// setupToxiproxy creates an SSH proxy listening on 12222 and forwarding to
// gitea:22. Returns the proxy handle so tests can add/remove toxins.
func setupToxiproxy(t *testing.T) *toxiclient.Proxy {
	t.Helper()
	cli := toxiclient.NewClient(toxiproxyAdmin)
	// ResetState clears any stale proxies left by a previous test.
	require.NoError(t, cli.ResetState())
	proxy, err := cli.CreateProxy("gitea-ssh", proxiedSSHListen, toxiproxyUpstrm)
	require.NoError(t, err)
	t.Cleanup(func() { _ = proxy.Delete() })
	return proxy
}

// testGitHandler delegates real git operations to go-git but disables SSH
// host-key verification — the Gitea instance is ephemeral and we do not seed
// known_hosts. Production code paths (Clone / FetchContext / PushContext) are
// exercised identically; only the auth construction differs.
type testGitHandler struct{}

func (testGitHandler) PlainClone(ctx context.Context, path string, isBare bool, o *gogit.CloneOptions) (*gogit.Repository, error) {
	return gogit.PlainCloneContext(ctx, path, isBare, o)
}

func (testGitHandler) PlainOpen(path string) (*gogit.Repository, error) {
	return gogit.PlainOpen(path)
}

func (testGitHandler) AddSSHKey(user, path, passphrase string) (*gogitssh.PublicKeys, error) {
	auth, err := gogitssh.NewPublicKeysFromFile(user, path, passphrase)
	if err != nil {
		return nil, err
	}
	auth.HostKeyCallback = cryptossh.InsecureIgnoreHostKey()
	return auth, nil
}

// waitForGitea blocks until the Gitea HTTP healthcheck returns 200, or the test
// fails after the timeout. Required because `go test` may start before the
// service is fully ready under some compose-up timings.
func waitForGitea(t *testing.T, timeout time.Duration) {
	t.Helper()
	cli := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	var lastStatus int
	for {
		resp, err := cli.Get(giteaAPI + "/api/healthz")
		if err == nil {
			lastStatus = resp.StatusCode
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("gitea not ready after %s (last status: %d)", timeout, lastStatus)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
