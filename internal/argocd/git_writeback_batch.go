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
//   - a generation error         — that app's config is invalid (isolated: it does
//     not fail the rest of the batch);
//   - a per-app commit error      — that app's commit failed on every attempt;
//   - the shared push error      — the batch's push ultimately failed.
//
// The push is the only contended operation; N apps therefore cost one clone plus
// one push instead of N of each. The loop retries until every request is resolved
// or the attempt budget is exhausted: a transient push failure reclones and
// re-applies the whole surviving batch, and a transient per-app commit failure is
// left unresolved so the next attempt retries just that app — matching the
// single-app path, where a commit failure also consumes the retry budget. A
// permanently misconfigured app (e.g. path-traversal write-back location) fails
// only itself after the budget is spent, never blocking the others.
func runBatchWriteBack(parentCtx context.Context, repo *updater.GitRepo, batch []*batchWriteRequest) map[*batchWriteRequest]error {
	outcomes := make(map[*batchWriteRequest]error, len(batch))
	// commitErrs holds each app's most recent per-app commit error while it is still
	// being retried; consulted only when resolving a leftover unresolved request so
	// it reports its own cause rather than a generic batch error.
	commitErrs := make(map[*batchWriteRequest]error, len(batch))
	maxAttempts := repo.GitMaxAttempts()
	opTimeout := repo.GitOpTimeout()

	var lastErr error
	for attempt := uint(1); attempt <= maxAttempts; attempt++ {
		active := unresolvedRequests(batch, outcomes)
		if len(active) == 0 {
			break
		}

		invalidateBatchCacheOnFinalAttempt(repo, len(active), attempt, maxAttempts)

		committed, err := runBatchAttempt(parentCtx, repo, opTimeout, active, outcomes, commitErrs)
		if err != nil {
			lastErr = err
			if updater.IsPermanent(err) {
				// A permanent clone/push failure recurs on every attempt; fail every
				// still-unresolved app now instead of burning the budget.
				slog.Error("Batch git update failed with permanent error; not retrying",
					"attempt", attempt, "max_attempts", maxAttempts, "error", err)
				failUnresolved(batch, outcomes, err)
				break
			}
		} else {
			for _, req := range committed {
				outcomes[req] = nil
			}
		}

		// Keep retrying while anything is unresolved — a failed/pending push or a
		// per-app commit error awaiting another attempt. Stop once all are resolved.
		if len(unresolvedRequests(batch, outcomes)) == 0 {
			break
		}
		if waitErr := backoffBeforeBatchRetry(parentCtx, attempt, maxAttempts); waitErr != nil {
			lastErr = waitErr
			break
		}
	}

	resolveRemaining(batch, outcomes, commitErrs, lastErr, maxAttempts)
	return outcomes
}

// runBatchAttempt performs one clone + per-app commit + single push cycle. It
// resolves outcomes[req] for terminally-resolved apps and records retriable per-app
// commit errors in commitErrs (leaving those requests unresolved), returning the
// requests that produced a local commit this attempt — whose fate depends on the
// push. The returned error is non-nil when the clone or push failed; the caller
// decides whether it is permanent or retriable. On a nil error every returned
// committed request pushed successfully.
func runBatchAttempt(parentCtx context.Context, repo *updater.GitRepo, opTimeout time.Duration, active []*batchWriteRequest, outcomes, commitErrs map[*batchWriteRequest]error) ([]*batchWriteRequest, error) {
	ctx, cancel := context.WithTimeout(parentCtx, opTimeout)
	defer cancel()

	if err := repo.Clone(ctx); err != nil {
		return nil, fmt.Errorf("clone failed: %w", err)
	}

	var committed []*batchWriteRequest
	for _, req := range active {
		if applyRequest(repo, req, outcomes, commitErrs) {
			committed = append(committed, req)
		}
	}

	if len(committed) == 0 {
		// Everything resolved terminally (or is awaiting a commit retry); no push.
		return nil, nil
	}

	if err := repo.Push(ctx); err != nil {
		return committed, fmt.Errorf("push failed: %w", err)
	}
	return committed, nil
}

// applyRequest resolves one request within a single attempt. It returns true when
// the request produced a local commit (its fate then rides on the shared push).
// Terminal outcomes are set for superseded / unsupported / generation cases; a
// per-app commit error is recorded in commitErrs and the request is left
// unresolved so the next attempt retries it — mirroring the single-app path, where
// a commit failure is also retriable rather than fatal.
func applyRequest(repo *updater.GitRepo, req *batchWriteRequest, outcomes, commitErrs map[*batchWriteRequest]error) bool {
	// Re-check supersede every attempt so a task that keeps retrying under
	// contention aborts the moment a newer deployment for the same app wins.
	if req.isSuperseded != nil && req.isSuperseded() {
		slog.Info("Git update aborted: task superseded by a newer deployment", "id", req.task.Id)
		outcomes[req] = ErrDeploymentSuperseded
		return false
	}

	// An unsupported configuration or missing managed images is a success with
	// nothing to write — mirrors the single-app UpdateGitImageTag early returns.
	if req.gitopsRepo.Path == "" {
		slog.Warn("No path found for app, unsupported Application configuration", "app", req.app.Metadata.Name, "id", req.task.Id)
		outcomes[req] = nil
		return false
	}
	content, genErr := generateOverrideFileContent(req.app.Metadata.Annotations, req.task)
	if genErr != nil {
		// A per-app misconfiguration is permanent; resolve it terminally so it does
		// not poison the rest of the batch.
		outcomes[req] = genErr
		return false
	}
	if content == nil {
		slog.Warn("No release overrides found for app", "app", req.app.Metadata.Name, "id", req.task.Id)
		outcomes[req] = nil
		return false
	}

	didCommit, commitErr := repo.CommitAppLocal(req.app.Metadata.Name, req.gitopsRepo.Path, req.gitopsRepo.Filename, content, req.task)
	if commitErr != nil {
		// Retriable per-app failure: record it and leave the request unresolved so
		// the next attempt reclones and re-applies just this app. A transient
		// filesystem/worktree error can then recover; a persistent one surfaces via
		// resolveRemaining once the budget is spent — without blocking other apps.
		commitErrs[req] = commitErr
		return false
	}
	if !didCommit {
		// On-disk content already matches: nothing to push for this app.
		outcomes[req] = nil
		return false
	}
	delete(commitErrs, req) // committed cleanly; drop any earlier recorded error
	return true
}

// failUnresolved assigns err as the terminal outcome of every request without one.
func failUnresolved(batch []*batchWriteRequest, outcomes map[*batchWriteRequest]error, err error) {
	for _, req := range unresolvedRequests(batch, outcomes) {
		outcomes[req] = err
	}
}

// resolveRemaining assigns a final error to every request still unresolved after
// the retry loop ends (exhausted attempts or a cancelled backoff). A per-app commit
// error takes precedence over the batch-level error so a persistently
// misconfigured app reports its own cause.
func resolveRemaining(batch []*batchWriteRequest, outcomes, commitErrs map[*batchWriteRequest]error, lastErr error, maxAttempts uint) {
	for _, req := range batch {
		if _, ok := outcomes[req]; ok {
			continue
		}
		cause := lastErr
		if ce, ok := commitErrs[req]; ok {
			cause = ce
		}
		outcomes[req] = fmt.Errorf("batch git update failed after %d attempts: %w", maxAttempts, cause)
	}
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
func backoffBeforeBatchRetry(parentCtx context.Context, attempt, maxAttempts uint) error {
	if attempt >= maxAttempts {
		return nil
	}
	backoff := gitUpdateBackoff(attempt)
	slog.Warn("Batch git update attempt left work unresolved; retrying",
		"attempt", attempt, "max_attempts", maxAttempts, "backoff", backoff)
	select {
	case <-parentCtx.Done():
		return fmt.Errorf("batch git update cancelled during backoff: %w", parentCtx.Err())
	case <-time.After(backoff):
		return nil
	}
}
