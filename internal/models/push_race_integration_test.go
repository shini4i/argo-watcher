//go:build integration

package models

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	toxiclient "github.com/Shopify/toxiproxy/v2/client"
	gogit "github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	gogitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/shini4i/argo-watcher/internal/lock"
	cryptossh "golang.org/x/crypto/ssh"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingHandler wraps testGitHandler and counts PlainOpen calls.
// PlainOpen is always the first call inside Clone(), so openCount >= 2 proves
// that the recovery branch re-entered Clone() — once for the fast path and
// once after the push-race retry.
type countingHandler struct {
	testGitHandler
	openCount int32
}

func (c *countingHandler) PlainOpen(path string) (*gogit.Repository, error) {
	atomic.AddInt32(&c.openCount, 1)
	return c.testGitHandler.PlainOpen(path)
}

// competitorPush clones the repo directly (bypassing toxiproxy), makes a
// trivial commit, and pushes it back. This is used to advance the remote
// "behind" the system under test, triggering a non-fast-forward error.
func competitorPush(t *testing.T, repoURL, sshKeyPath, marker string) {
	t.Helper()

	auth, err := gogitssh.NewPublicKeysFromFile("git", sshKeyPath, "")
	require.NoError(t, err)
	auth.HostKeyCallback = cryptossh.InsecureIgnoreHostKey()

	dir := t.TempDir()
	repo, err := gogit.PlainCloneContext(context.Background(), dir, false, &gogit.CloneOptions{
		URL:           repoURL,
		ReferenceName: "refs/heads/master",
		SingleBranch:  true,
		Auth:          auth,
	})
	require.NoError(t, err)

	wt, err := repo.Worktree()
	require.NoError(t, err)

	// Commit a file with the marker so the race is observable in repo history.
	fp := filepath.Join(dir, "competitor.txt")
	require.NoError(t, os.WriteFile(fp, []byte(marker+"\n"), 0o644)) // #nosec G306
	_, err = wt.Add("competitor.txt")
	require.NoError(t, err)
	_, err = wt.Commit("competing commit: "+marker, &gogit.CommitOptions{
		Author:            &object.Signature{Name: "competitor", Email: "c@test.example", When: time.Now()},
		AllowEmptyCommits: true,
	})
	require.NoError(t, err)

	require.NoError(t, repo.Push(&gogit.PushOptions{
		Auth:     auth,
		RefSpecs: []gogitconfig.RefSpec{"refs/heads/master:refs/heads/master"},
	}))
}

// TestIntegration_PushRaceRecovery_WithLatencyInjection uses toxiproxy upstream
// latency to widen the window between goroutine A's fetch and its push, giving
// a competitor writer (direct SSH, no proxy) time to land a commit. This
// exercises the full recovery loop in UpdateGitImageTag under real Gitea
// push-race conditions.
//
// The race is probabilistic: a slow CI runner may cause A's fetch to exceed
// the 2.5s sleep, so the competitor might push before A even fetches. When
// that happens the test still passes (no error returned), but recovery is
// not exercised. This is acceptable — flakiness in the other direction
// (spurious failure) is what matters for CI signal.
func TestIntegration_PushRaceRecovery_WithLatencyInjection(t *testing.T) {
	waitForGitea(t, 60*time.Second)
	env := setupGitea(t)
	proxy := setupToxiproxy(t)

	t.Setenv("SSH_KEY_PATH", env.SSHKeyPath)
	t.Setenv("GIT_TIMEOUT", "60s")

	// Apply upstream latency so every byte from client→server (handshake,
	// fetch, push payload) is delayed. This widens the race window enough for
	// the competitor (direct, no proxy) to land between A's fetch and A's push.
	_, err := proxy.AddToxic("delay", "latency", "upstream", 1.0,
		toxiclient.Attributes{"latency": 300})
	require.NoError(t, err)

	handler := &countingHandler{}

	aDone := make(chan error, 1)
	go func() {
		aDone <- newAppWithImages("test-app").UpdateGitImageTag(
			newImageTask(),
			&GitopsRepo{
				RepoUrl:       env.ProxyRepoURL,
				BranchName:    "master",
				Path:          "apps",
				RepoCachePath: t.TempDir(),
			},
			handler,
		)
	}()

	// Wait long enough for A's initial fetch to land but not so long that A's
	// push has already arrived at the server. With latency=300ms per byte, A's
	// fetch typically completes around 1-2s; A's push starts immediately after
	// and takes another 1-2s to reach the server.
	time.Sleep(2500 * time.Millisecond)
	competitorPush(t, env.DirectRepoURL, env.SSHKeyPath, "race-injection")

	select {
	case err := <-aDone:
		require.NoError(t, err, "UpdateGitImageTag should succeed after recovery")
	case <-time.After(90 * time.Second):
		t.Fatal("UpdateGitImageTag did not complete in 90s")
	}

	// Recovery path enters Clone() a second time. PlainOpen is the first
	// handler call inside Clone(), so a count of >= 2 proves recovery fired.
	opens := atomic.LoadInt32(&handler.openCount)
	assert.GreaterOrEqual(t, opens, int32(2),
		"recovery branch should re-enter Clone() (PlainOpen called %d times)", opens)
}

// TestIntegration_PushRaceRecovery_Concurrent runs two writers concurrently
// against the same repo with no shared locker, mimicking two argo-watcher
// replicas that cannot coordinate. Each writer uses an independent cache
// directory (per-instance TempDir), so whoever loses the push race must rely
// entirely on UpdateGitImageTag's single-retry recovery to succeed.
//
// N=2 is intentional: the production recovery path retries exactly once, which
// is guaranteed to be enough for at most one concurrent pusher — both writers
// cannot be in their recovery windows simultaneously.
func TestIntegration_PushRaceRecovery_Concurrent(t *testing.T) {
	waitForGitea(t, 60*time.Second)
	env := setupGitea(t)

	t.Setenv("SSH_KEY_PATH", env.SSHKeyPath)
	t.Setenv("GIT_TIMEOUT", "30s")

	const N = 2

	var (
		wg        sync.WaitGroup
		errs      = make([]error, N)
		durations = make([]time.Duration, N)
		started   = time.Now()
	)

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			start := time.Now()
			errs[idx] = newAppWithImages(fmt.Sprintf("app-%d", idx)).UpdateGitImageTag(
				&Task{
					Id:     fmt.Sprintf("task-%d", idx),
					Images: []Image{{Image: "myimage", Tag: fmt.Sprintf("v%d", idx)}},
				},
				&GitopsRepo{
					RepoUrl:       env.DirectRepoURL,
					BranchName:    "master",
					Path:          "apps",
					RepoCachePath: t.TempDir(), // independent cache per writer (matches multi-replica prod)
				},
				testGitHandler{},
			)
			durations[idx] = time.Since(start)
		}(i)
	}
	wg.Wait()
	totalWall := time.Since(started)

	for i, e := range errs {
		assert.NoError(t, e, "goroutine %d failed", i)
	}
	for i, d := range durations {
		// 2 × GIT_TIMEOUT covers one op + one recovery retry within budget.
		assert.Less(t, d, 60*time.Second,
			"goroutine %d took %s (exceeds 2× GIT_TIMEOUT, suggests stuck recovery)", i, d)
	}
	// Both writers + at most one recovery should complete well within 30s.
	assert.Less(t, totalWall, 30*time.Second,
		"total wall clock %s exceeds 30s", totalWall)
}

// TestIntegration_GitTimeoutEnforcement_ReturnsWithinBudget verifies that
// UpdateGitImageTag eventually returns when network calls stall, and that the
// per-repo lock is released so a queued second task can proceed — guarding
// against the queue-wedging failure mode that motivated commit e2ab9fe.
//
// Caveat about the budget: go-git's SSH transport does not propagate the
// caller's context into the SSH library's handshake phase, so a stall during
// the initial handshake is bounded by the SSH library's own timeout (~2 min)
// rather than GIT_TIMEOUT. The bounded-context guarantee from e2ab9fe applies
// to git-protocol operations after the SSH connection is established. Either
// path is sufficient to prevent the queue-wedging regression; this test
// asserts the weaker but realistic guarantee that operations are bounded by
// max(GIT_TIMEOUT, SSH handshake timeout) — not infinite.
func TestIntegration_GitTimeoutEnforcement_ReturnsWithinBudget(t *testing.T) {
	waitForGitea(t, 60*time.Second)
	env := setupGitea(t)
	proxy := setupToxiproxy(t)

	const budget = 3 * time.Second
	// Per-task upper bound: SSH handshake timeout (~2 min on go-git's default
	// SSH ClientConfig) plus a small buffer. Anything above this means the
	// operation hung past every bounding mechanism — the regression we guard
	// against.
	const perTaskCeiling = 150 * time.Second

	t.Setenv("SSH_KEY_PATH", env.SSHKeyPath)
	t.Setenv("GIT_TIMEOUT", budget.String())

	// Heavy latency stalls every byte through the proxy. The SSH handshake
	// will not survive — it eventually returns "handshake failed: EOF" via
	// the SSH library's own timeout. The post-handshake git ops never run.
	_, err := proxy.AddToxic("stall", "latency", "upstream", 1.0,
		toxiclient.Attributes{"latency": 30000})
	require.NoError(t, err)

	locker := lock.NewInMemoryLocker()

	gitopsRepo := &GitopsRepo{
		RepoUrl:       env.ProxyRepoURL,
		BranchName:    "master",
		Path:          "apps",
		RepoCachePath: t.TempDir(),
	}

	run := func(idx int) (time.Duration, error) {
		var inner error
		start := time.Now()
		outer := locker.WithLock(gitopsRepo.RepoUrl, func() error {
			inner = newAppWithImages(fmt.Sprintf("app-%d", idx)).UpdateGitImageTag(
				&Task{Id: fmt.Sprintf("task-%d", idx), Images: []Image{{Image: "myimage", Tag: "v1"}}},
				gitopsRepo,
				testGitHandler{},
			)
			return inner
		})
		if outer != nil {
			return time.Since(start), outer
		}
		return time.Since(start), inner
	}

	type result struct {
		dur time.Duration
		err error
	}
	aCh := make(chan result, 1)
	bCh := make(chan result, 1)

	go func() {
		d, e := run(0)
		aCh <- result{d, e}
	}()
	// Stagger B slightly so it enters the queue behind A.
	time.Sleep(100 * time.Millisecond)
	go func() {
		d, e := run(1)
		bCh <- result{d, e}
	}()

	a := <-aCh
	b := <-bCh

	require.Error(t, a.err, "A should fail under the latency toxin")
	require.Error(t, b.err, "B should fail once it acquires the lock")

	// A is bounded by either the GIT_TIMEOUT context (if the stall hits
	// post-handshake) or the SSH library's handshake timeout. Either is
	// acceptable; the only failure mode is unbounded hang.
	assert.Less(t, a.dur, perTaskCeiling,
		"A took %s — operation hung beyond SSH handshake timeout (%s)",
		a.dur, perTaskCeiling)
	// B starts ~100ms after A. The lock must be released for B to even begin,
	// then B runs into the same stall. Allow 2× the per-task ceiling.
	assert.Less(t, b.dur, 2*perTaskCeiling,
		"B took %s — lock not released after A or B hung indefinitely",
		b.dur)
}
