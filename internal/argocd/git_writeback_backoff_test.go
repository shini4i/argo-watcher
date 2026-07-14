package argocd

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/updater"
)

// fakeGitHandler drives runGitUpdateWithRetry without a real remote: PlainOpen
// reports no cache so Clone takes the fresh-clone path, and PlainClone returns a
// (non-permanent) error on every call, so every attempt fails and the loop
// retries. cloneCalls counts how many attempts actually reached the clone.
type fakeGitHandler struct {
	cloneCalls int
	cloneErr   error
}

func (h *fakeGitHandler) PlainOpen(string) (*git.Repository, error) {
	return nil, git.ErrRepositoryNotExists
}

func (h *fakeGitHandler) PlainClone(_ context.Context, _ string, _ bool, _ *git.CloneOptions) (*git.Repository, error) {
	h.cloneCalls++
	return nil, h.cloneErr
}

func (h *fakeGitHandler) AddSSHKey(_, _, _ string) (*ssh.PublicKeys, error) {
	return &ssh.PublicKeys{}, nil
}

// gitTestRepo builds a GitopsRepo pointing at a throwaway cache dir.
func gitTestRepo(t *testing.T) *models.GitopsRepo {
	return &models.GitopsRepo{
		RepoUrl:       "git@example.com:test/repo.git",
		BranchName:    "main",
		Path:          "apps",
		RepoCachePath: t.TempDir(),
	}
}

// TestGitUpdateSupersededOnLaterAttempt proves the supersession guard is
// re-checked on a LATER attempt, not just once up front: attempt 1 proceeds
// (reaches the clone) and fails transiently; the predicate flips to true before
// attempt 2, which must abort with ErrDeploymentSuperseded before cloning again.
func TestGitUpdateSupersededOnLaterAttempt(t *testing.T) {
	t.Setenv("SSH_KEY_PATH", "/nonexistent/key")
	t.Setenv("GIT_OP_TIMEOUT", "5s")
	t.Setenv("GIT_MAX_ATTEMPTS", "5")

	h := &fakeGitHandler{cloneErr: errors.New("transient clone failure")}
	checks := 0
	supersede := func() bool { checks++; return checks >= 2 } // false on attempt 1, true on attempt 2

	err := UpdateGitImageTag(
		context.Background(), newAppWithImages("test-app"), newImageTask(), gitTestRepo(t), h, supersede,
	)

	require.ErrorIs(t, err, ErrDeploymentSuperseded)
	assert.Equal(t, 1, h.cloneCalls, "attempt 1 must reach the clone; the guard fires on attempt 2, not up front")
}

// TestGitUpdateExhaustsRetries covers the multi-attempt retry mechanics on the
// changed loop: a transient error on every attempt exhausts the budget and the
// error wraps with the attempt count.
func TestGitUpdateExhaustsRetries(t *testing.T) {
	t.Setenv("SSH_KEY_PATH", "/nonexistent/key")
	t.Setenv("GIT_OP_TIMEOUT", "5s")
	t.Setenv("GIT_MAX_ATTEMPTS", "3")

	h := &fakeGitHandler{cloneErr: errors.New("transient clone failure")}

	err := UpdateGitImageTag(
		context.Background(), newAppWithImages("test-app"), newImageTask(), gitTestRepo(t), h,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "git update failed after 3 attempts")
	assert.Equal(t, 3, h.cloneCalls, "all attempts should run when every one fails transiently")
}

// TestUpdateGitImageTagSupersededGuard verifies the write-back aborts with
// ErrDeploymentSuperseded — before touching git — when the supersede predicate
// returns true, and does NOT abort with that error when it returns false. This
// is what keeps a larger retry budget from letting an older deployment overwrite
// a newer one.
func TestUpdateGitImageTagSupersededGuard(t *testing.T) {
	// SSH_KEY_PATH need only be set (not exist) for config load; the guard fires
	// before the key is ever read, so a nonexistent path is fine here.
	repo := func(t *testing.T) *models.GitopsRepo {
		return &models.GitopsRepo{
			RepoUrl:       "git@example.com:test/repo.git",
			BranchName:    "main",
			Path:          "apps",
			RepoCachePath: t.TempDir(),
		}
	}

	t.Run("superseded → aborts before any git operation", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/nonexistent/key")
		err := UpdateGitImageTag(
			context.Background(), newAppWithImages("test-app"), newImageTask(), repo(t), updater.GitClient{},
			func() bool { return true },
		)
		require.ErrorIs(t, err, ErrDeploymentSuperseded)
	})

	t.Run("not superseded → proceeds (fails later, not with the guard error)", func(t *testing.T) {
		t.Setenv("SSH_KEY_PATH", "/nonexistent/key")
		err := UpdateGitImageTag(
			context.Background(), newAppWithImages("test-app"), newImageTask(), repo(t), updater.GitClient{},
			func() bool { return false },
		)
		require.Error(t, err) // fails on the missing SSH key, having passed the guard
		assert.NotErrorIs(t, err, ErrDeploymentSuperseded)
	})
}

// TestGitUpdateBackoff verifies the retry backoff is capped-exponential with
// full jitter: every sample stays within [0, ceiling] where the ceiling grows
// with the attempt number until it saturates at gitUpdateMaxBackoff. Fast early
// retries are what let the write-back win a git push race against a competing
// writer before it advances the branch again.
func TestGitUpdateBackoff(t *testing.T) {
	// ceiling(attempt) = min(gitUpdateMaxBackoff, base * 2^(attempt-1))
	ceiling := func(attempt uint) time.Duration {
		c := gitUpdateBaseBackoff << (attempt - 1)
		if c <= 0 || c > gitUpdateMaxBackoff {
			c = gitUpdateMaxBackoff
		}
		return c
	}

	for attempt := uint(1); attempt <= 12; attempt++ {
		want := ceiling(attempt)
		var sawHigh bool
		for i := 0; i < 2000; i++ {
			b := gitUpdateBackoff(attempt)
			if b < 0 || b > want {
				t.Fatalf("attempt %d: backoff %s out of range [0,%s]", attempt, b, want)
			}
			if b > want/2 {
				sawHigh = true
			}
		}
		// With full jitter over 2000 samples we expect to see values in the
		// upper half of the range — guards against a broken (always-tiny) jitter.
		if !sawHigh {
			t.Errorf("attempt %d: never saw backoff > %s over 2000 samples (jitter looks broken)", attempt, want/2)
		}
	}

	// The ceiling must actually grow early on (fast first retry, slower later).
	if ceiling(1) >= ceiling(4) {
		t.Fatalf("ceiling should increase: ceiling(1)=%s ceiling(4)=%s", ceiling(1), ceiling(4))
	}
	// And saturate at the cap.
	if ceiling(12) != gitUpdateMaxBackoff {
		t.Fatalf("ceiling(12)=%s, want cap %s", ceiling(12), gitUpdateMaxBackoff)
	}
}
