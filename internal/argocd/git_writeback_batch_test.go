package argocd

import (
	"context"
	"errors"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/updater"
)

// batchReqFor builds a batch write-back request for one app. path is that app's
// write-back path (empty simulates an unsupported/unmanaged app). superseded may
// be nil.
func batchReqFor(app *models.Application, path string, superseded func() bool) *batchWriteRequest {
	return &batchWriteRequest{
		app:          app,
		task:         newImageTask(),
		gitopsRepo:   &models.GitopsRepo{RepoUrl: "git@example.com:test/repo.git", BranchName: "main", Path: path},
		isSuperseded: superseded,
		resultCh:     make(chan error, 1),
	}
}

// newBatchTestRepo builds a real *updater.GitRepo backed by the given mock handler,
// so the batch retry loop runs against controllable Clone/Push behaviour without a
// live remote.
func newBatchTestRepo(t *testing.T, handler updater.GitHandler) *updater.GitRepo {
	t.Helper()
	repo, err := updater.NewGitRepo("git@example.com:test/repo.git", "main", "", "", t.TempDir(), handler)
	require.NoError(t, err)
	return repo
}

// TestRunBatchWriteBack_ExhaustsRetriesResolvesEveryRequest proves the core
// invariant: when every attempt fails with a retriable error, the retry budget is
// exhausted and EVERY request is resolved to an error — none is left without an
// outcome. An unresolved request would hang its deploying goroutine forever on the
// result channel, so this guards a real shippable hang.
func TestRunBatchWriteBack_ExhaustsRetriesResolvesEveryRequest(t *testing.T) {
	t.Setenv("SSH_KEY_PATH", "/nonexistent/key")
	t.Setenv("GIT_OP_TIMEOUT", "5s")
	t.Setenv("GIT_MAX_ATTEMPTS", "2")

	// Clone fails transiently on both attempts (Times(2) enforced at ctrl finish).
	h := retryingGitHandler(gomock.NewController(t), errors.New("transient clone failure"), 2)
	repo := newBatchTestRepo(t, h)

	batch := []*batchWriteRequest{
		batchReqFor(newAppWithImages("app-a"), "apps", nil),
		batchReqFor(newAppWithImages("app-b"), "apps", nil),
	}

	outcomes := runBatchWriteBack(context.Background(), repo, batch)

	require.Len(t, outcomes, len(batch), "every request must be resolved exactly once")
	for _, req := range batch {
		require.Contains(t, outcomes, req, "request left unresolved would hang its goroutine")
		require.Error(t, outcomes[req])
		assert.Contains(t, outcomes[req].Error(), "batch git update failed after 2 attempts")
	}
}

// TestRunBatchWriteBack_PermanentErrorFailsAllImmediately verifies a permanent
// error (auth failure) is not retried and fails every request in the batch at once.
func TestRunBatchWriteBack_PermanentErrorFailsAllImmediately(t *testing.T) {
	t.Setenv("SSH_KEY_PATH", "/nonexistent/key")
	t.Setenv("GIT_OP_TIMEOUT", "5s")
	t.Setenv("GIT_MAX_ATTEMPTS", "5")

	// A permanent auth error must stop after a single clone (Times(1)); if the loop
	// retried, gomock would fail the unexpected extra PlainClone calls.
	h := retryingGitHandler(gomock.NewController(t), transport.ErrAuthorizationFailed, 1)
	repo := newBatchTestRepo(t, h)

	batch := []*batchWriteRequest{
		batchReqFor(newAppWithImages("app-a"), "apps", nil),
		batchReqFor(newAppWithImages("app-b"), "apps", nil),
	}

	outcomes := runBatchWriteBack(context.Background(), repo, batch)

	require.Len(t, outcomes, len(batch))
	for _, req := range batch {
		require.ErrorIs(t, outcomes[req], transport.ErrAuthorizationFailed)
	}
}

// TestRunBatchWriteBack_MixedPerAppOutcomes proves per-app isolation and the
// one-outcome-per-request invariant in a single successful clone attempt: a
// superseded app aborts, an unmanaged (no path) app is a no-op success, and a
// misconfigured app fails only itself — none poisons the others.
func TestRunBatchWriteBack_MixedPerAppOutcomes(t *testing.T) {
	t.Setenv("SSH_KEY_PATH", "/nonexistent/key")
	t.Setenv("GIT_OP_TIMEOUT", "5s")
	t.Setenv("GIT_MAX_ATTEMPTS", "3")

	// Clone succeeds (returns a nil repo, never dereferenced because every app
	// resolves before any commit) and no push happens, so exactly one clone.
	h := retryingGitHandler(gomock.NewController(t), nil, 1)
	repo := newBatchTestRepo(t, h)

	superseded := batchReqFor(newAppWithImages("app-super"), "apps", func() bool { return true })
	unmanaged := batchReqFor(newAppWithImages("app-nopath"), "", nil)

	badApp := newAppWithImages("app-bad")
	badApp.Metadata.Annotations["argo-watcher/managed-images"] = "broken" // no "=" → invalid format
	misconfigured := batchReqFor(badApp, "apps", nil)

	batch := []*batchWriteRequest{superseded, unmanaged, misconfigured}

	outcomes := runBatchWriteBack(context.Background(), repo, batch)

	require.Len(t, outcomes, 3, "each request must have exactly one outcome")
	assert.ErrorIs(t, outcomes[superseded], ErrDeploymentSuperseded)
	assert.NoError(t, outcomes[unmanaged], "an app with no write-back path is a no-op success")
	require.Error(t, outcomes[misconfigured])
	assert.Contains(t, outcomes[misconfigured].Error(), "invalid format",
		"a misconfigured app fails only itself")
}

// TestRunBatchWriteBack_CommitErrorIsRetried verifies that a per-app commit error
// is retried across attempts (mirroring the single-app path) rather than failing
// the app on the first attempt. A path-traversal write-back filename makes
// CommitAppLocal fail on every attempt; the app must consume the full retry budget
// (clone runs GIT_MAX_ATTEMPTS times) and then surface its own cause.
func TestRunBatchWriteBack_CommitErrorIsRetried(t *testing.T) {
	t.Setenv("SSH_KEY_PATH", "/nonexistent/key")
	t.Setenv("GIT_OP_TIMEOUT", "5s")
	t.Setenv("GIT_MAX_ATTEMPTS", "2")

	// Clone succeeds every attempt; the commit failure (not clone/push) drives the
	// retries, so the handler must see one clone per attempt — Times(2).
	h := retryingGitHandler(gomock.NewController(t), nil, 2)
	repo := newBatchTestRepo(t, h)

	badApp := newAppWithImages("app-bad")
	req := &batchWriteRequest{
		app:  badApp,
		task: newImageTask(),
		// A malicious write-back-filename escapes the repo root; CommitAppLocal
		// rejects it (before touching the worktree) on every attempt.
		gitopsRepo: &models.GitopsRepo{BranchName: "main", Path: "apps", Filename: "../../../../etc/passwd"},
		resultCh:   make(chan error, 1),
	}

	outcomes := runBatchWriteBack(context.Background(), repo, []*batchWriteRequest{req})

	require.Len(t, outcomes, 1)
	require.Error(t, outcomes[req])
	assert.Contains(t, outcomes[req].Error(), "not inside repository root", "the app's own commit error must surface")
	assert.Contains(t, outcomes[req].Error(), "after 2 attempts", "the commit error must be retried, not terminal on attempt 1")
}

// TestRunBatchWriteBack_CancelledDuringBackoffResolvesEveryRequest verifies that a
// context cancelled during the inter-attempt backoff stops the loop early and still
// resolves every request with the cancellation error (never leaving one hanging).
func TestRunBatchWriteBack_CancelledDuringBackoffResolvesEveryRequest(t *testing.T) {
	t.Setenv("SSH_KEY_PATH", "/nonexistent/key")
	t.Setenv("GIT_OP_TIMEOUT", "5s")
	t.Setenv("GIT_MAX_ATTEMPTS", "3")

	// Attempt 1 clones and fails transiently; the cancelled context then aborts the
	// backoff before attempt 2, so only ONE clone happens (Times(1)).
	h := retryingGitHandler(gomock.NewController(t), errors.New("transient clone failure"), 1)
	repo := newBatchTestRepo(t, h)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled up front; the backoff select observes it before attempt 2

	batch := []*batchWriteRequest{
		batchReqFor(newAppWithImages("app-a"), "apps", nil),
		batchReqFor(newAppWithImages("app-b"), "apps", nil),
	}

	outcomes := runBatchWriteBack(ctx, repo, batch)

	require.Len(t, outcomes, len(batch))
	for _, req := range batch {
		require.Error(t, outcomes[req])
		assert.Contains(t, outcomes[req].Error(), "cancelled during backoff")
	}
}
