//go:build integration

package models

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	toxiclient "github.com/Shopify/toxiproxy/v2/client"
	gogit "github.com/go-git/go-git/v5"
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
		RefSpecs: []string{"refs/heads/master:refs/heads/master"},
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
// UpdateGitImageTag returns within GIT_TIMEOUT even when every git op stalls
// past the deadline — proving the total-budget context (commit e2ab9fe) is
// wired through all go-git network calls.
//
// The locker wrapper demonstrates the downstream effect: because the function
// returns within budget, the per-repo lock is released promptly, allowing the
// queued second task to acquire it and also fail fast rather than waiting for
// the full stall duration.
func TestIntegration_GitTimeoutEnforcement_ReturnsWithinBudget(t *testing.T) {
	waitForGitea(t, 60*time.Second)
	env := setupGitea(t)
	proxy := setupToxiproxy(t)

	const budget = 3 * time.Second
	t.Setenv("SSH_KEY_PATH", env.SSHKeyPath)
	t.Setenv("GIT_TIMEOUT", budget.String())

	// Latency far exceeding GIT_TIMEOUT — every git op will hit the deadline.
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

	require.Error(t, a.err, "A should fail with deadline-exceeded due to latency toxin")
	require.Error(t, b.err, "B should also fail with deadline-exceeded once it gets the lock")

	assert.True(t, errors.Is(a.err, context.DeadlineExceeded) || isDeadlineMessage(a.err),
		"A error should be deadline-exceeded, got: %v", a.err)
	assert.True(t, errors.Is(b.err, context.DeadlineExceeded) || isDeadlineMessage(b.err),
		"B error should be deadline-exceeded, got: %v", b.err)

	// Both A and B should fail within ~budget. B also waits for A to release
	// the lock (100ms stagger ensures A enters first — best-effort ordering).
	// B's total time is: queue wait (≤ A's budget) + its own budget ≈ 2×budget.
	// Allowing 2×budget as the ceiling; if the budget context were not wired,
	// B would hang for the full 30s stall before returning.
	assert.Less(t, a.dur, 2*budget,
		"A took %s — UpdateGitImageTag did not return within budget", a.dur)
	assert.Less(t, b.dur, 2*budget,
		"B took %s — lock or timeout not released promptly after A's deadline", b.dur)
}

// isDeadlineMessage is a soft fallback for cases where the error has been
// wrapped via fmt.Errorf (which can break errors.Is identity but keeps the
// substring).
func isDeadlineMessage(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "budget exhausted")
}
