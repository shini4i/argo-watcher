//go:build integration

package argocd

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	toxiclient "github.com/Shopify/toxiproxy/v2/client"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	gogitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	cryptossh "golang.org/x/crypto/ssh"
	"strings"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/updater"
)

// testSSHAuth builds go-git SSH auth from a key file with host-key verification
// disabled (the Gitea instance is ephemeral), matching the other integration helpers.
func testSSHAuth(t *testing.T, sshKeyPath string) *gogitssh.PublicKeys {
	t.Helper()
	auth, err := gogitssh.NewPublicKeysFromFile("git", sshKeyPath, "")
	require.NoError(t, err)
	auth.HostKeyCallback = cryptossh.InsecureIgnoreHostKey()
	return auth
}

// newBatchReqFor builds a batch request for one app writing a single image tag
// into its own override file under apps/. Distinct app names produce disjoint
// override files (.argocd-source-<name>.yaml), so a batch never has intra-file
// conflicts. superseded may be nil.
func newBatchReqFor(name, tag string, superseded func() bool) *batchWriteRequest {
	return &batchWriteRequest{
		app:  newAppWithImages(name),
		task: &models.Task{Id: "task-" + name, Images: []models.Image{{Image: "myimage", Tag: tag}}},
		// RepoUrl/BranchName/RepoCachePath are unused here: runBatchWriteBack operates
		// on the shared GitRepo passed to it. Only Path/Filename are read per-app.
		gitopsRepo:   &models.GitopsRepo{BranchName: "master", Path: "apps"},
		isSuperseded: superseded,
		resultCh:     make(chan error, 1),
	}
}

// countArgoCommits clones the remote and counts commits whose message marks an
// argo-watcher image-tag update, so a test can assert one commit landed per app.
func countArgoCommits(t *testing.T, repoURL, sshKeyPath, branch string) int {
	t.Helper()
	dir := t.TempDir()
	auth := testSSHAuth(t, sshKeyPath)
	repo, err := gogit.PlainCloneContext(context.Background(), dir, false, &gogit.CloneOptions{
		URL:          repoURL,
		SingleBranch: true,
		Auth:         auth,
	})
	require.NoError(t, err)

	iter, err := repo.Log(&gogit.LogOptions{})
	require.NoError(t, err)
	count := 0
	require.NoError(t, iter.ForEach(func(c *object.Commit) error {
		if strings.Contains(c.Message, "update image tag") {
			count++
		}
		return nil
	}))
	return count
}

// TestIntegration_BatchWriteBack_SingleCloneCommitPerApp verifies the core batch
// promise: N apps sharing a repo are written with one clone and one commit per
// app, and all their override files land on the remote after a single flush.
func TestIntegration_BatchWriteBack_SingleCloneCommitPerApp(t *testing.T) {
	waitForGitea(t, 60*time.Second)
	env := setupGitea(t)

	t.Setenv("SSH_KEY_PATH", env.SSHKeyPath)
	t.Setenv("GIT_OP_TIMEOUT", "60s")
	t.Setenv("GIT_MAX_ATTEMPTS", "3")

	handler := &countingHandler{}
	repo, err := updater.NewGitRepo(env.DirectRepoURL, "master", "", "", t.TempDir(), handler)
	require.NoError(t, err)

	batch := []*batchWriteRequest{
		newBatchReqFor("app-a", "v1", nil),
		newBatchReqFor("app-b", "v2", nil),
		newBatchReqFor("app-c", "v3", nil),
	}

	outcomes := runBatchWriteBack(context.Background(), repo, batch)

	for _, req := range batch {
		assert.NoError(t, outcomes[req], "app %s should succeed", req.app.Metadata.Name)
	}

	// Exactly one clone served the whole batch (PlainOpen is the first call in
	// Clone; a value >1 would mean a retry re-cloned).
	assert.Equal(t, int32(1), atomic.LoadInt32(&handler.openCount), "the whole batch must share a single clone")

	// One commit per app landed on the remote.
	assert.Equal(t, 3, countArgoCommits(t, env.DirectRepoURL, env.SSHKeyPath, "master"),
		"expected one commit per app in the batch")

	// Each app's override file carries its own tag.
	for _, tc := range []struct{ name, tag string }{{"app-a", "v1"}, {"app-b", "v2"}, {"app-c", "v3"}} {
		_, content := cloneRemoteState(t, env.DirectRepoURL, env.SSHKeyPath, "master",
			fmt.Sprintf("apps/.argocd-source-%s.yaml", tc.name))
		assert.Contains(t, content, tc.tag, "override for %s must contain its tag", tc.name)
	}
}

// TestIntegration_BatchWriteBack_PartialSupersede verifies per-app isolation: a
// superseded app is dropped from the batch (its file is never written) while the
// rest are committed and pushed normally.
func TestIntegration_BatchWriteBack_PartialSupersede(t *testing.T) {
	waitForGitea(t, 60*time.Second)
	env := setupGitea(t)

	t.Setenv("SSH_KEY_PATH", env.SSHKeyPath)
	t.Setenv("GIT_OP_TIMEOUT", "60s")
	t.Setenv("GIT_MAX_ATTEMPTS", "3")

	repo, err := updater.NewGitRepo(env.DirectRepoURL, "master", "", "", t.TempDir(), testGitHandler{})
	require.NoError(t, err)

	superseded := newBatchReqFor("app-super", "v9", func() bool { return true })
	batch := []*batchWriteRequest{
		newBatchReqFor("app-a", "v1", nil),
		superseded,
		newBatchReqFor("app-b", "v2", nil),
	}

	outcomes := runBatchWriteBack(context.Background(), repo, batch)

	assert.ErrorIs(t, outcomes[superseded], ErrDeploymentSuperseded, "superseded app must abort")
	assert.NoError(t, outcomes[batch[0]])
	assert.NoError(t, outcomes[batch[2]])

	// The two live apps landed; the superseded one did not.
	assert.Equal(t, 2, countArgoCommits(t, env.DirectRepoURL, env.SSHKeyPath, "master"))
	_, superContent := cloneRemoteState(t, env.DirectRepoURL, env.SSHKeyPath, "master",
		"apps/.argocd-source-app-super.yaml")
	assert.Empty(t, superContent, "superseded app's override file must not be committed")
}

// TestIntegration_BatchWriteBack_PushRaceRecovery drives a batch through a
// toxiproxy-delayed connection while a competitor advances the branch, exercising
// the batch's reclone-reapply-repush recovery. On recovery every app in the batch
// must still land and the competitor's commit must be preserved (no clobber).
func TestIntegration_BatchWriteBack_PushRaceRecovery(t *testing.T) {
	waitForGitea(t, 60*time.Second)
	env := setupGitea(t)
	proxy := setupToxiproxy(t)

	t.Setenv("SSH_KEY_PATH", env.SSHKeyPath)
	t.Setenv("GIT_OP_TIMEOUT", "60s")
	t.Setenv("GIT_MAX_ATTEMPTS", "3")

	_, err := proxy.AddToxic("delay", "latency", "upstream", 1.0,
		toxiclient.Attributes{"latency": 300})
	require.NoError(t, err)

	const maxAttempts = 3
	var lastOpens int32
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		handler := &countingHandler{}
		// Each attempt uses a fresh cache so the race window is reproducible.
		repo, rErr := updater.NewGitRepo(env.ProxyRepoURL, "master", "", "", t.TempDir(), handler)
		require.NoError(t, rErr)

		batch := []*batchWriteRequest{
			newBatchReqFor(fmt.Sprintf("race-a-%d", attempt), "v1", nil),
			newBatchReqFor(fmt.Sprintf("race-b-%d", attempt), "v2", nil),
		}

		done := make(chan map[*batchWriteRequest]error, 1)
		go func() { done <- runBatchWriteBack(context.Background(), repo, batch) }()

		// Let the batch's fetch land, then land a competing commit before its push.
		time.Sleep(2500 * time.Millisecond)
		competitorPush(t, env.DirectRepoURL, env.SSHKeyPath, fmt.Sprintf("batch-race-%d", attempt))

		select {
		case outcomes := <-done:
			for _, req := range batch {
				require.NoError(t, outcomes[req], "app %s should succeed after recovery (attempt %d)", req.app.Metadata.Name, attempt)
			}
			// The competitor's commit must survive the recovery.
			_, competitorContent := cloneRemoteState(t, env.DirectRepoURL, env.SSHKeyPath, "master", "competitor.txt")
			assert.Contains(t, competitorContent, "batch-race",
				"recovery must preserve the competitor's commit, not force-push over it (attempt %d)", attempt)
		case <-time.After(120 * time.Second):
			t.Fatalf("batch write-back did not complete in 120s (attempt %d)", attempt)
		}

		lastOpens = atomic.LoadInt32(&handler.openCount)
		if lastOpens >= 2 {
			t.Logf("attempt %d: race window fired and batch retry executed (PlainOpen called %d times)", attempt, lastOpens)
			return
		}
		t.Logf("attempt %d: race window did not fire (openCount=%d); retrying", attempt, lastOpens)
	}

	require.GreaterOrEqual(t, lastOpens, int32(2),
		"batch retry not observed after %d attempts: PlainOpen called %d times on final attempt", maxAttempts, lastOpens)
}
