//go:build integration

package argocd

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	toxiclient "github.com/Shopify/toxiproxy/v2/client"
	gogit "github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/models"
)

// appBTagLine matches app-b's marked line in inlineAppsManifest.
var appBTagLine = regexp.MustCompile(`tag: \S+ # \{"\$argo-watcher": "app-b:app"\}`)

// seedInlineManifest commits inlineAppsManifest as apps/apps.yaml through the Gitea API.
func seedInlineManifest(t *testing.T, env *giteaEnv) {
	t.Helper()
	giteaAPIPost(t, env.User, env.Password,
		fmt.Sprintf("/api/v1/repos/%s/%s/contents/apps/apps.yaml", env.User, env.RepoName),
		map[string]any{
			"message": "seed application manifest",
			"content": base64.StdEncoding.EncodeToString([]byte(inlineAppsManifest)),
			"branch":  "master",
		})
}

// competitorEditsAppB rewrites app-b's marked tag in apps/apps.yaml and publishes
// it directly (bypassing toxiproxy), as a human or other CI editing the same file would.
func competitorEditsAppB(t *testing.T, env *giteaEnv, tag string) {
	t.Helper()
	auth := testSSHAuth(t, env.SSHKeyPath)
	dir := t.TempDir()
	repo, err := gogit.PlainCloneContext(context.Background(), dir, false, &gogit.CloneOptions{
		URL: env.DirectRepoURL, ReferenceName: "refs/heads/master", SingleBranch: true, Auth: auth,
	})
	require.NoError(t, err)

	file := filepath.Join(dir, "apps", "apps.yaml")
	content, err := os.ReadFile(file) // #nosec G304 -- test-controlled path
	require.NoError(t, err)
	edited := appBTagLine.ReplaceAllString(string(content), `tag: `+tag+` # {"$argo-watcher": "app-b:app"}`)
	require.NoError(t, os.WriteFile(file, []byte(edited), 0o644)) // #nosec G306

	wt, err := repo.Worktree()
	require.NoError(t, err)
	_, err = wt.Add("apps/apps.yaml")
	require.NoError(t, err)
	_, err = wt.Commit("external edit of app-b", &gogit.CommitOptions{
		Author: &object.Signature{Name: "competitor", Email: "c@test.example", When: time.Now()},
	})
	require.NoError(t, err)
	require.NoError(t, repo.Push(&gogit.PushOptions{
		Auth:     auth,
		RefSpecs: []gogitconfig.RefSpec{"refs/heads/master:refs/heads/master"},
	}))
}

// TestIntegration_InlineWriteBack_SharedManifestPushRace lands an external edit of
// another application's line in the same manifest between our fetch and our push.
// The retry must re-apply on the new tip so both edits survive and the rest of the
// file stays byte-identical. The race is probabilistic, as in the override-mode test.
func TestIntegration_InlineWriteBack_SharedManifestPushRace(t *testing.T) {
	waitForGitea(t, 60*time.Second)
	env := setupGitea(t)
	proxy := setupToxiproxy(t)
	seedInlineManifest(t, env)

	t.Setenv("SSH_KEY_PATH", env.SSHKeyPath)
	t.Setenv("GIT_OP_TIMEOUT", "60s")
	t.Setenv("GIT_MAX_ATTEMPTS", "3")

	_, err := proxy.AddToxic("delay", "latency", "upstream", 1.0,
		toxiclient.Attributes{"latency": 300})
	require.NoError(t, err)

	const maxAttempts = 3
	var lastOpens int32
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		ourTag := fmt.Sprintf("v%d.0.0", attempt)
		theirTag := fmt.Sprintf("v9.%d.0", attempt)

		handler := &countingHandler{}
		done := make(chan error, 1)
		go func() {
			done <- UpdateGitImageTag(
				context.Background(),
				newInlineApp("app-a"),
				&models.Task{Id: "inline-race", Images: []models.Image{{Image: "myimage", Tag: ourTag}}},
				&models.GitopsRepo{
					RepoUrl: env.ProxyRepoURL, BranchName: "master", Path: "apps", Filename: "apps.yaml",
					RepoCachePath: t.TempDir(), Inline: true,
				},
				handler,
			)
		}()

		time.Sleep(2500 * time.Millisecond)
		competitorEditsAppB(t, env, theirTag)

		select {
		case err := <-done:
			require.NoError(t, err, "inline write-back should succeed after recovery (attempt %d)", attempt)
			_, got := cloneRemoteState(t, env.DirectRepoURL, env.SSHKeyPath, "master", "apps/apps.yaml")
			want := appBTagLine.ReplaceAllString(inlineAppsManifest, `tag: `+theirTag+` # {"$argo-watcher": "app-b:app"}`)
			want = regexp.MustCompile(`tag: \S+ # \{"\$argo-watcher": "app-a:app"\}`).
				ReplaceAllString(want, `tag: `+ourTag+` # {"$argo-watcher": "app-a:app"}`)
			assert.Equal(t, want, got, "both edits must survive and nothing else may change (attempt %d)", attempt)
		case <-time.After(90 * time.Second):
			t.Fatalf("inline write-back did not complete in 90s (attempt %d)", attempt)
		}

		lastOpens = atomic.LoadInt32(&handler.openCount)
		if lastOpens >= 2 {
			t.Logf("attempt %d: race window fired and retry executed (PlainOpen called %d times)", attempt, lastOpens)
			return
		}
		t.Logf("attempt %d: race window did not fire (openCount=%d); retrying", attempt, lastOpens)
	}

	require.GreaterOrEqual(t, lastOpens, int32(2),
		"retry not observed after %d attempts: PlainOpen called %d times on final attempt", maxAttempts, lastOpens)
}
