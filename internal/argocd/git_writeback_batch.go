package argocd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/updater"
)

// batchWriteRequest is a single application's pending git write-back, queued for
// coalesced flushing by the Batcher. Exactly one result is delivered on resultCh.
type batchWriteRequest struct {
	app          *models.Application
	task         *models.Task
	gitopsRepo   *models.GitopsRepo
	isSuperseded func() bool
	// resultCh is buffered (size 1) so the flush goroutine never blocks delivering
	// a result even if the waiting task goroutine has already gone away.
	resultCh chan error
}

// runBatchWriteBack applies every request in batch to a single shared clone of the
// repository and pushes once, with the same retry/backoff/supersede semantics as
// the single-app path (runGitUpdateWithRetry). Each request is resolved to exactly
// one outcome in the returned map:
//   - nil                        — committed and pushed, or nothing to write;
//   - ErrDeploymentSuperseded    — a newer deployment for that app won;
//   - a generation/commit error  — that app's config is invalid (isolated: it does
//     not fail the rest of the batch);
//   - the shared push error      — the batch's push ultimately failed.
//
// The push is the only contended operation; N apps therefore cost one clone plus
// one push instead of N of each. On a transient push failure the whole attempt is
// retried: Clone hard-resets to the remote tip (discarding the batch's local
// commits) and every still-unresolved app is re-checked, re-applied, and re-pushed
// together — reusing the existing reclone-reapply-repush recovery.
func runBatchWriteBack(parentCtx context.Context, repo *updater.GitRepo, batch []*batchWriteRequest) map[*batchWriteRequest]error {
	outcomes := make(map[*batchWriteRequest]error, len(batch))
	maxAttempts := repo.GitMaxAttempts()
	opTimeout := repo.GitOpTimeout()

	var lastErr error
	for attempt := uint(1); attempt <= maxAttempts; attempt++ {
		active := unresolvedRequests(batch, outcomes)
		if len(active) == 0 {
			break
		}

		invalidateBatchCacheOnFinalAttempt(repo, len(active), attempt, maxAttempts)

		committed, err := runBatchAttempt(parentCtx, repo, opTimeout, active, outcomes)
		if err == nil {
			// Clone succeeded and (if anything was committed) the push succeeded.
			for _, req := range committed {
				outcomes[req] = nil
			}
			break
		}
		lastErr = err

		if updater.IsPermanent(err) {
			// A permanent clone/push failure will recur on every attempt; stop and
			// fail every app that is not already terminally resolved.
			slog.Error("Batch git update failed with permanent error; not retrying",
				"attempt", attempt, "max_attempts", maxAttempts, "error", err)
			for _, req := range unresolvedRequests(batch, outcomes) {
				outcomes[req] = err
			}
			break
		}

		if waitErr := backoffBeforeBatchRetry(parentCtx, err, attempt, maxAttempts); waitErr != nil {
			lastErr = waitErr
			break
		}
	}

	// Resolve anything still pending (clone failures, exhausted retries, or a
	// cancelled backoff) with the last error seen.
	for _, req := range batch {
		if _, ok := outcomes[req]; !ok {
			outcomes[req] = fmt.Errorf("batch git update failed after %d attempts: %w", maxAttempts, lastErr)
		}
	}

	return outcomes
}

// runBatchAttempt performs one clone + per-app commit + single push cycle. It
// resolves outcomes[req] for terminally-resolved apps (superseded, generation or
// commit error, or nothing-to-write) and returns the requests that produced a
// local commit this attempt, whose fate depends on the push. The returned error
// is non-nil when the clone or push failed; the caller decides whether it is
// permanent or retriable. On a nil error every returned committed request pushed
// successfully.
func runBatchAttempt(parentCtx context.Context, repo *updater.GitRepo, opTimeout time.Duration, active []*batchWriteRequest, outcomes map[*batchWriteRequest]error) ([]*batchWriteRequest, error) {
	ctx, cancel := context.WithTimeout(parentCtx, opTimeout)
	defer cancel()

	if err := repo.Clone(ctx); err != nil {
		return nil, fmt.Errorf("clone failed: %w", err)
	}

	var committed []*batchWriteRequest
	for _, req := range active {
		// Re-check supersede every attempt so a task that keeps retrying under
		// contention aborts the moment a newer deployment for the same app wins.
		if req.isSuperseded != nil && req.isSuperseded() {
			slog.Info("Git update aborted: task superseded by a newer deployment", "id", req.task.Id)
			outcomes[req] = ErrDeploymentSuperseded
			continue
		}

		// An unsupported configuration or missing managed images is a success with
		// nothing to write — mirrors the single-app UpdateGitImageTag early returns.
		if req.gitopsRepo.Path == "" {
			slog.Warn("No path found for app, unsupported Application configuration", "app", req.app.Metadata.Name, "id", req.task.Id)
			outcomes[req] = nil
			continue
		}
		content, genErr := generateOverrideFileContent(req.app.Metadata.Annotations, req.task)
		if genErr != nil {
			// A per-app misconfiguration must not poison the rest of the batch.
			outcomes[req] = genErr
			continue
		}
		if content == nil {
			slog.Warn("No release overrides found for app", "app", req.app.Metadata.Name, "id", req.task.Id)
			outcomes[req] = nil
			continue
		}

		didCommit, commitErr := repo.CommitAppLocal(req.app.Metadata.Name, req.gitopsRepo.Path, req.gitopsRepo.Filename, content, req.task)
		if commitErr != nil {
			// A per-app commit error (e.g. a path-traversal-rejected write-back
			// location) is resolved terminally rather than retried, deliberately
			// diverging from the single-app path: it isolates one misconfigured app
			// so it cannot block or destabilise the rest of the batch, and the retry
			// loop cannot heal it anyway — Clone already reset the worktree cleanly
			// at the top of this attempt, so a failing local commit is a per-app or
			// environment problem, not transient contention.
			outcomes[req] = commitErr
			continue
		}
		if !didCommit {
			// On-disk content already matches: nothing to push for this app.
			outcomes[req] = nil
			continue
		}
		committed = append(committed, req)
	}

	if len(committed) == 0 {
		// Everything resolved terminally; no push needed.
		return nil, nil
	}

	if err := repo.Push(ctx); err != nil {
		return committed, fmt.Errorf("push failed: %w", err)
	}
	return committed, nil
}

// unresolvedRequests returns the requests in batch that do not yet have an outcome.
// A nil value is a valid (successful) outcome, so resolution is tracked by key
// presence, not by the value.
func unresolvedRequests(batch []*batchWriteRequest, outcomes map[*batchWriteRequest]error) []*batchWriteRequest {
	var active []*batchWriteRequest
	for _, req := range batch {
		if _, ok := outcomes[req]; !ok {
			active = append(active, req)
		}
	}
	return active
}

// invalidateBatchCacheOnFinalAttempt clears the on-disk cache before the final
// attempt so a poisoned cache self-heals with a fresh clone, mirroring the
// single-app invalidateCacheOnFinalAttempt.
func invalidateBatchCacheOnFinalAttempt(repo *updater.GitRepo, batchSize int, attempt, maxAttempts uint) {
	if attempt != maxAttempts {
		return
	}
	slog.Warn("Final batch attempt: invalidating cache and performing fresh clone",
		"attempt", attempt, "max_attempts", maxAttempts, "batch_size", batchSize)
	if invErr := repo.InvalidateCache(); invErr != nil {
		slog.Warn("Failed to invalidate cache before final batch attempt; proceeding anyway", "error", invErr)
	}
}

// backoffBeforeBatchRetry waits (jittered) before the next batch attempt, unless
// this was the final one. It returns a non-nil error only if parentCtx is
// cancelled during the wait, signalling the caller to stop retrying. It reuses
// gitUpdateBackoff so batch and single-app retries share the same anti-thundering-
// herd behaviour.
func backoffBeforeBatchRetry(parentCtx context.Context, attemptErr error, attempt, maxAttempts uint) error {
	if attempt >= maxAttempts {
		return nil
	}
	backoff := gitUpdateBackoff(attempt)
	slog.Warn("Batch git update attempt failed; retrying",
		"attempt", attempt, "max_attempts", maxAttempts, "backoff", backoff, "error", attemptErr)
	select {
	case <-parentCtx.Done():
		return fmt.Errorf("batch git update cancelled during backoff: %w", parentCtx.Err())
	case <-time.After(backoff):
		return nil
	}
}
