//go:build integration

package models

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	gogitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	cryptossh "golang.org/x/crypto/ssh"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cloneRemoteState clones the remote directly (full history, bypassing any
// cache) into a throwaway dir and returns the current HEAD hash plus the
// contents of the given repo-relative path. Tests use it to observe what
// actually landed on the server, independent of the system-under-test's cache.
func cloneRemoteState(t *testing.T, repoURL, sshKeyPath, branch, relPath string) (headHash string, fileContent string) {
	t.Helper()

	auth, err := gogitssh.NewPublicKeysFromFile("git", sshKeyPath, "")
	require.NoError(t, err)
	auth.HostKeyCallback = cryptossh.InsecureIgnoreHostKey()

	dir := t.TempDir()
	repo, err := gogit.PlainCloneContext(context.Background(), dir, false, &gogit.CloneOptions{
		URL:           repoURL,
		ReferenceName: plumbing.ReferenceName("refs/heads/" + branch),
		SingleBranch:  true,
		Auth:          auth,
	})
	require.NoError(t, err)

	head, err := repo.Head()
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, relPath)) // #nosec G304 -- test-controlled path
	if err != nil {
		return head.Hash().String(), ""
	}
	return head.Hash().String(), string(content)
}

// localPresentCommitCount opens the on-disk cache and counts commits actually
// present locally, walking from HEAD until the walk runs off the shallow
// boundary (go-git surfaces the missing parent as an "object not found" error
// rather than stopping cleanly — that error IS the boundary, so we stop and
// return the count gathered so far). On a shallow cache this stays small; a
// repo silently deepened to full history would walk its entire commit graph.
func localPresentCommitCount(t *testing.T, localRepoPath string) int {
	t.Helper()
	repo, err := gogit.PlainOpen(localRepoPath)
	require.NoError(t, err)
	iter, err := repo.Log(&gogit.LogOptions{})
	require.NoError(t, err)
	count := 0
	walkErr := iter.ForEach(func(_ *object.Commit) error {
		count++
		return nil
	})
	// The ONLY tolerated terminal error is running off the shallow boundary
	// (the parent commit is absent locally). Any other error is a real failure
	// that must not be silently swallowed into a falsely-low count.
	if walkErr != nil {
		require.ErrorIs(t, walkErr, plumbing.ErrObjectNotFound,
			"commit walk failed for a reason other than the shallow boundary")
	}
	return count
}

// remoteCommitCount full-clones the remote (complete history, so the walk never
// hits a shallow boundary) and counts commits reachable from the branch HEAD.
// Tests use it to prove that write-backs stack linearly on the remote — a
// clobbering force-push would leave the count flat or lower instead of growing
// by one per commit.
func remoteCommitCount(t *testing.T, repoURL, sshKeyPath, branch string) int {
	t.Helper()
	auth, err := gogitssh.NewPublicKeysFromFile("git", sshKeyPath, "")
	require.NoError(t, err)
	auth.HostKeyCallback = cryptossh.InsecureIgnoreHostKey()

	dir := t.TempDir()
	repo, err := gogit.PlainCloneContext(context.Background(), dir, false, &gogit.CloneOptions{
		URL:           repoURL,
		ReferenceName: plumbing.ReferenceName("refs/heads/" + branch),
		SingleBranch:  true,
		Auth:          auth,
	})
	require.NoError(t, err)
	iter, err := repo.Log(&gogit.LogOptions{})
	require.NoError(t, err)
	count := 0
	require.NoError(t, iter.ForEach(func(_ *object.Commit) error {
		count++
		return nil
	}))
	return count
}

// localTagCount opens the on-disk cache and counts local tag references. A
// shallow clone/fetch with Tags:NoTags must never populate any, regardless of
// how many tags the remote carries.
func localTagCount(t *testing.T, localRepoPath string) int {
	t.Helper()
	repo, err := gogit.PlainOpen(localRepoPath)
	require.NoError(t, err)
	iter, err := repo.Tags()
	require.NoError(t, err)
	count := 0
	require.NoError(t, iter.ForEach(func(_ *plumbing.Reference) error {
		count++
		return nil
	}))
	return count
}

// seedFullClone creates a NON-shallow clone of the remote at exactly the cache
// path GitRepo.Clone will use, simulating a cache left behind by a pre-upgrade
// (full-history) version of argo-watcher.
func seedFullClone(t *testing.T, repoURL, sshKeyPath, branch, cachePath, localRepoPath string) {
	t.Helper()
	auth, err := gogitssh.NewPublicKeysFromFile("git", sshKeyPath, "")
	require.NoError(t, err)
	auth.HostKeyCallback = cryptossh.InsecureIgnoreHostKey()
	require.NoError(t, os.MkdirAll(cachePath, 0o755))
	_, err = gogit.PlainCloneContext(context.Background(), localRepoPath, false, &gogit.CloneOptions{
		URL:           repoURL,
		ReferenceName: plumbing.ReferenceName("refs/heads/" + branch),
		SingleBranch:  true,
		Auth:          auth,
	})
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(localRepoPath, ".git", "shallow"),
		"precondition: the seeded cache must be a full (non-shallow) clone")
}

// TestIntegration_ShallowClone_UpgradeFromFullCache is the migration test: an
// existing FULL-history cache on a persistent volume, left by a pre-upgrade
// argo-watcher, must keep working after the switch to shallow fetch.
//
// Verified behavior (this test locks it in): a Depth:1 fetch does NOT
// retroactively shallow an existing full repo. The write-back succeeds and the
// commit lands, the cache is handled IN PLACE (no self-heal re-clone — the .git
// sentinel survives), and the cache stays full. So a pre-upgrade full cache is
// grandfathered: correct and no slower on the warm path (the expensive full
// clone already happened and does not recur), it just keeps its larger on-disk
// footprint until independently invalidated — at which point the self-heal path
// re-clones it shallow (see TestIntegration_ShallowClone_SelfHealFreshClone).
func TestIntegration_ShallowClone_UpgradeFromFullCache(t *testing.T) {
	waitForGitea(t, 60*time.Second)
	env := setupGitea(t)

	t.Setenv("SSH_KEY_PATH", env.SSHKeyPath)
	t.Setenv("GIT_OP_TIMEOUT", "60s")
	// 2 so attempt 1 exercises the warm path on the pre-seeded full cache; if it
	// fails, attempt 2 (final) self-heals via InvalidateCache + fresh clone.
	t.Setenv("GIT_MAX_ATTEMPTS", "2")

	const app = "upgrade-app"
	cachePath := t.TempDir()
	overrideRel := fmt.Sprintf("apps/.argocd-source-%s.yaml", app)
	localRepoPath := computeRepoCachePath(cachePath, env.DirectRepoURL, "master")

	// Give the remote some real history so "full" is meaningful, then seed a
	// full-history cache at the code's cache path.
	competitorPush(t, env.DirectRepoURL, env.SSHKeyPath, "hist1")
	competitorPush(t, env.DirectRepoURL, env.SSHKeyPath, "hist2")
	seedFullClone(t, env.DirectRepoURL, env.SSHKeyPath, "master", cachePath, localRepoPath)

	// Sentinel in .git: survives the in-place warm path; removed by a self-heal
	// (InvalidateCache does os.RemoveAll on the whole cache dir).
	sentinel := filepath.Join(localRepoPath, ".git", "UPGRADE_SENTINEL")
	require.NoError(t, os.WriteFile(sentinel, []byte("pre-upgrade cache"), 0o600))

	gitopsRepo := &GitopsRepo{
		RepoUrl:       env.DirectRepoURL,
		BranchName:    "master",
		Path:          "apps",
		RepoCachePath: cachePath,
	}
	err := newAppWithImages(app).UpdateGitImageTag(
		context.Background(),
		&Task{Id: "task-upgrade", Images: []Image{{Image: "myimage", Tag: "v1"}}},
		gitopsRepo,
		testGitHandler{},
	)

	// Invariant 1: the write-back succeeds against a pre-existing full cache.
	require.NoError(t, err, "write-back must succeed against a pre-upgrade full-history cache")

	// Invariant 2: the commit lands correctly on the remote.
	_, content := cloneRemoteState(t, env.DirectRepoURL, env.SSHKeyPath, "master", overrideRel)
	assert.Contains(t, content, "v1", "the upgrade write-back must commit the tag")

	// Invariant 3: the full cache was reused IN PLACE — no self-heal re-clone was
	// needed (the sentinel in .git survives only if the cache dir was never wiped).
	assert.FileExists(t, sentinel,
		"a pre-upgrade full cache must be reused in place, not wiped and re-cloned")

	// Invariant 4 (characterization): a Depth:1 fetch does NOT retroactively
	// shallow an existing full repo — the grandfathered cache stays full. If a
	// future go-git version changes this, update the migration note in the
	// docstring rather than treating it as a regression.
	assert.NoFileExists(t, filepath.Join(localRepoPath, ".git", "shallow"),
		"Depth:1 fetch is not expected to shallow an already-full cache")
}

// TestIntegration_ShallowClone_WarmFetchOfExternallyAdvancedTip covers the case
// the plain warm-cache test cannot: a Depth:1 warm fetch must retrieve a branch
// tip that an EXTERNAL writer advanced by more than one commit, and our commit
// must stack on that advanced tip as a fast-forward — never clobber the
// intervening commits. This is the correctness property that most matters for a
// shallow cache: it must not silently rewrite history it cannot see.
func TestIntegration_ShallowClone_WarmFetchOfExternallyAdvancedTip(t *testing.T) {
	waitForGitea(t, 60*time.Second)
	env := setupGitea(t)

	t.Setenv("SSH_KEY_PATH", env.SSHKeyPath)
	t.Setenv("GIT_OP_TIMEOUT", "60s")
	t.Setenv("GIT_MAX_ATTEMPTS", "2") // keep the warm cache; no self-heal reclone

	const app = "advanced-tip-app"
	cachePath := t.TempDir()
	overrideRel := fmt.Sprintf("apps/.argocd-source-%s.yaml", app)
	gitopsRepo := &GitopsRepo{
		RepoUrl:       env.DirectRepoURL,
		BranchName:    "master",
		Path:          "apps",
		RepoCachePath: cachePath,
	}
	writeBack := func(tag string) error {
		return newAppWithImages(app).UpdateGitImageTag(
			context.Background(),
			&Task{Id: "task-" + tag, Images: []Image{{Image: "myimage", Tag: tag}}},
			gitopsRepo,
			testGitHandler{},
		)
	}
	localRepoPath := computeRepoCachePath(cachePath, env.DirectRepoURL, "master")

	// (1) Cold shallow clone + commit v1, seeding the warm cache.
	require.NoError(t, writeBack("v1"), "cold write-back should succeed")

	// (2) An external writer advances master by TWO commits while our cache sits
	// at the old tip. Our next warm fetch must jump the full distance.
	competitorPush(t, env.DirectRepoURL, env.SSHKeyPath, "c1")
	competitorPush(t, env.DirectRepoURL, env.SSHKeyPath, "c2")

	// (3) Warm write-back v2: Depth:1 fetch of the moved tip + hard reset + commit + push.
	require.NoError(t, writeBack("v2"),
		"warm write-back must succeed against an externally-advanced tip")

	// The pushed HEAD's tree must contain BOTH our v2 override AND the
	// competitor's file — proving we committed on top of their tip rather than
	// force-pushing over it.
	_, overrideContent := cloneRemoteState(t, env.DirectRepoURL, env.SSHKeyPath, "master", overrideRel)
	_, competitorContent := cloneRemoteState(t, env.DirectRepoURL, env.SSHKeyPath, "master", "competitor.txt")
	assert.Contains(t, overrideContent, "v2", "our v2 tag must be committed")
	assert.Contains(t, competitorContent, "c2",
		"competitor commits must survive — our write-back must fast-forward, not clobber")

	require.FileExists(t, filepath.Join(localRepoPath, ".git", "shallow"),
		"cache must stay shallow after fetching a multi-commit advance")
}

// TestIntegration_ShallowClone_SelfHealFreshClone exercises the final-attempt
// self-heal path: with GIT_MAX_ATTEMPTS=1 every write-back invalidates the cache
// and performs a fresh shallow clone before committing. Successive write-backs
// must still produce correctly-parented commits that stack on the remote, i.e.
// the fresh shallow re-clone must not lose or rewrite prior history.
func TestIntegration_ShallowClone_SelfHealFreshClone(t *testing.T) {
	waitForGitea(t, 60*time.Second)
	env := setupGitea(t)

	t.Setenv("SSH_KEY_PATH", env.SSHKeyPath)
	t.Setenv("GIT_OP_TIMEOUT", "60s")
	// 1 => invalidateCacheOnFinalAttempt fires on the (only) attempt, so every
	// write-back wipes the cache and re-clones shallow from scratch.
	t.Setenv("GIT_MAX_ATTEMPTS", "1")

	const app = "selfheal-app"
	cachePath := t.TempDir()
	overrideRel := fmt.Sprintf("apps/.argocd-source-%s.yaml", app)
	gitopsRepo := &GitopsRepo{
		RepoUrl:       env.DirectRepoURL,
		BranchName:    "master",
		Path:          "apps",
		RepoCachePath: cachePath,
	}
	writeBack := func(tag string) error {
		return newAppWithImages(app).UpdateGitImageTag(
			context.Background(),
			&Task{Id: "task-" + tag, Images: []Image{{Image: "myimage", Tag: tag}}},
			gitopsRepo,
			testGitHandler{},
		)
	}
	localRepoPath := computeRepoCachePath(cachePath, env.DirectRepoURL, "master")

	base := remoteCommitCount(t, env.DirectRepoURL, env.SSHKeyPath, "master")

	require.NoError(t, writeBack("v1"), "first self-heal write-back should succeed")
	require.NoError(t, writeBack("v2"), "second self-heal write-back should succeed")

	// Two commits landed, stacked linearly on top of the base — the fresh
	// re-clone between them neither lost v1 nor rewrote history.
	assert.Equal(t, base+2, remoteCommitCount(t, env.DirectRepoURL, env.SSHKeyPath, "master"),
		"each self-heal write-back must add exactly one commit, preserving prior history")

	_, content := cloneRemoteState(t, env.DirectRepoURL, env.SSHKeyPath, "master", overrideRel)
	assert.Contains(t, content, "v2", "latest self-heal commit must carry v2")

	require.FileExists(t, filepath.Join(localRepoPath, ".git", "shallow"),
		"the self-heal re-clone must itself be shallow")
}

// TestIntegration_ShallowClone_WarmCacheWriteBacks is the gating test for
// switching GitRepo.Clone to a shallow (Depth:1) clone. It exercises the exact
// production sequence that a shallow cache must survive on a persistent volume:
// a cold clone followed by repeated warm write-backs (fetch + hard reset +
// commit + push) against the SAME cache directory.
//
// It asserts two independent things:
//   - correctness: every write-back's commit lands on the remote, and an
//     unchanged write-back is skipped — the warm path must work on a shallow repo;
//   - shallowness: the cache is shallow after the cold clone AND stays shallow
//     across warm fetches (go-git must not silently deepen to full history,
//     which would defeat the whole point on a 100k-commit repo).
//
// Before Depth:1 is implemented this fails on the shallowness assertion (a full
// clone has no shallow boundary); after, all assertions must hold, or shallow
// clone does not fit our flow and must not ship.
func TestIntegration_ShallowClone_WarmCacheWriteBacks(t *testing.T) {
	waitForGitea(t, 60*time.Second)
	env := setupGitea(t)

	t.Setenv("SSH_KEY_PATH", env.SSHKeyPath)
	t.Setenv("GIT_OP_TIMEOUT", "60s")
	// >=2 so a successful first attempt never hits invalidateCacheOnFinalAttempt;
	// otherwise every call would wipe the cache and re-clone, defeating the
	// warm-cache reuse this test exists to exercise.
	t.Setenv("GIT_MAX_ATTEMPTS", "2")

	// Seed a tag on the remote so the Tags:NoTags half of the change has teeth:
	// a regression re-enabling tag fetching would pull this tag into the cache.
	giteaAPIPost(t, env.User, env.Password,
		fmt.Sprintf("/api/v1/repos/%s/%s/tags", env.User, env.RepoName),
		map[string]any{"tag_name": "seed-tag", "target": "master"})

	const app = "shallow-app"
	// One shared cache across every write-back — the persistent-volume model.
	cachePath := t.TempDir()
	overrideRel := fmt.Sprintf("apps/.argocd-source-%s.yaml", app)

	// Direct SSH URL (no toxiproxy): this test injects no latency, so it needs
	// only Gitea. Keeping the SUT on the direct URL avoids a toxiproxy dependency.
	gitopsRepo := &GitopsRepo{
		RepoUrl:       env.DirectRepoURL,
		BranchName:    "master",
		Path:          "apps",
		RepoCachePath: cachePath,
	}

	writeBack := func(tag string) error {
		return newAppWithImages(app).UpdateGitImageTag(
			context.Background(),
			&Task{Id: "task-" + tag, Images: []Image{{Image: "myimage", Tag: tag}}},
			gitopsRepo,
			testGitHandler{},
		)
	}

	// The deterministic on-disk cache path the code will actually use.
	localRepoPath := computeRepoCachePath(cachePath, env.DirectRepoURL, "master")

	// (1) Cold clone + commit v1.
	require.NoError(t, writeBack("v1"), "cold write-back should succeed")

	shallowMarker := filepath.Join(localRepoPath, ".git", "shallow")
	require.FileExists(t, shallowMarker,
		"cache must be a shallow clone (.git/shallow present) after the cold clone")
	assert.Zero(t, localTagCount(t, localRepoPath),
		"cold clone must not fetch tags (Tags:NoTags), even though the remote has one")

	head1, content1 := cloneRemoteState(t, env.DirectRepoURL, env.SSHKeyPath, "master", overrideRel)
	assert.Contains(t, content1, "v1", "v1 tag must be committed to the remote")

	// (2) Warm write-back on the shallow cache: fetch + hard reset + commit v2 + push.
	// This is the path go-git shallow support must not break.
	require.NoError(t, writeBack("v2"), "warm write-back on a shallow cache must succeed")

	head2, content2 := cloneRemoteState(t, env.DirectRepoURL, env.SSHKeyPath, "master", overrideRel)
	assert.NotEqual(t, head1, head2, "warm write-back must advance the remote branch")
	assert.Contains(t, content2, "v2", "v2 tag must be committed to the remote")

	require.FileExists(t, shallowMarker,
		"cache must STAY shallow after a warm fetch (go-git must not deepen to full history)")
	assert.LessOrEqual(t, localPresentCommitCount(t, localRepoPath), 3,
		"warm fetch must not pull full remote history into the shallow cache")
	assert.Zero(t, localTagCount(t, localRepoPath),
		"warm fetch must not fetch tags (Tags:NoTags)")

	// (3) Identical content is a no-op: byte-compare skip must still work on the
	// shallow warm cache.
	require.NoError(t, writeBack("v2"), "no-op write-back should succeed")
	head3, _ := cloneRemoteState(t, env.DirectRepoURL, env.SSHKeyPath, "master", overrideRel)
	assert.Equal(t, head2, head3, "unchanged content must not create a new commit")
}
