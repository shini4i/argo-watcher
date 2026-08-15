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
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/mocks"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/updater"
)

// retryingGitHandler returns a MockGitHandler whose PlainClone always fails with
// cloneErr. The Times(wantClones) expectation fails the test at controller finish
// unless exactly that many attempts reached the clone.
func retryingGitHandler(ctrl *gomock.Controller, cloneErr error, wantClones int) *mocks.MockGitHandler {
	h := mocks.NewMockGitHandler(ctrl)
	h.EXPECT().PlainOpen(gomock.Any()).Return(nil, git.ErrRepositoryNotExists).AnyTimes()
	h.EXPECT().AddSSHKey(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ssh.PublicKeys{}, nil).AnyTimes()
	h.EXPECT().PlainClone(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, cloneErr).Times(wantClones)
	return h
}

func gitTestRepo(t *testing.T) *models.GitopsRepo {
	return &models.GitopsRepo{
		RepoUrl:       "git@example.com:test/repo.git",
		BranchName:    "main",
		Path:          "apps",
		RepoCachePath: t.TempDir(),
	}
}

// TestGitUpdateSupersededOnLaterAttempt proves the supersession guard is
// re-checked on a LATER attempt, not just once up front.
func TestGitUpdateSupersededOnLaterAttempt(t *testing.T) {
	t.Setenv("SSH_KEY_PATH", "/nonexistent/key")
	t.Setenv("GIT_OP_TIMEOUT", "5s")
	t.Setenv("GIT_MAX_ATTEMPTS", "5")

	h := retryingGitHandler(gomock.NewController(t), errors.New("transient clone failure"), 1)
	checks := 0
	supersede := func() bool { checks++; return checks >= 2 }

	err := UpdateGitImageTag(
		context.Background(), newAppWithImages("test-app"), newImageTask(), gitTestRepo(t), h, supersede,
	)

	require.ErrorIs(t, err, ErrDeploymentSuperseded)
}

func TestGitUpdateExhaustsRetries(t *testing.T) {
	t.Setenv("SSH_KEY_PATH", "/nonexistent/key")
	t.Setenv("GIT_OP_TIMEOUT", "5s")
	t.Setenv("GIT_MAX_ATTEMPTS", "3")

	h := retryingGitHandler(gomock.NewController(t), errors.New("transient clone failure"), 3)

	err := UpdateGitImageTag(
		context.Background(), newAppWithImages("test-app"), newImageTask(), gitTestRepo(t), h,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "git update failed after 3 attempts")
}

// TestUpdateGitImageTagSupersededGuard covers the guard that keeps a larger retry
// budget from letting an older deployment overwrite a newer one.
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
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrDeploymentSuperseded)
	})
}

// TestGitUpdateBackoff verifies the retry backoff is capped-exponential with full
// jitter. Fast early retries are what let the write-back win a git push race
// against a competing writer before it advances the branch again.
func TestGitUpdateBackoff(t *testing.T) {
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

	if ceiling(1) >= ceiling(4) {
		t.Fatalf("ceiling should increase: ceiling(1)=%s ceiling(4)=%s", ceiling(1), ceiling(4))
	}
	if ceiling(12) != gitUpdateMaxBackoff {
		t.Fatalf("ceiling(12)=%s, want cap %s", ceiling(12), gitUpdateMaxBackoff)
	}
}
