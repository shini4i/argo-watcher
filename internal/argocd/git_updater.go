package argocd

import (
	"context"
	"log/slog"
	"time"

	"github.com/shini4i/argo-watcher/internal/lock"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/prometheus"
	"github.com/shini4i/argo-watcher/internal/updater"
)

// GitUpdater encapsulates the logic required to update Git repositories watched by ArgoCD.
type GitUpdater struct {
	locker        lock.Locker
	repoCachePath string
	// metrics records lock-wait and write-back durations. May be nil in tests, in which
	// case observation is skipped.
	metrics prometheus.MetricsInterface
	// batcher coalesces concurrent write-backs to the same repo into a single
	// clone + push. When nil (the default) each app is written back on its own via
	// the serialized per-repo-lock path.
	batcher *Batcher
}

// NewGitUpdater creates a GitUpdater instance.
func NewGitUpdater(locker lock.Locker, repoCachePath string, metrics prometheus.MetricsInterface, batcher *Batcher) *GitUpdater {
	return &GitUpdater{
		locker:        locker,
		repoCachePath: repoCachePath,
		metrics:       metrics,
		batcher:       batcher,
	}
}

// Close releases resources held by the updater, draining any in-flight batch
// write-backs bounded by ctx.
func (gitUpdater *GitUpdater) Close(ctx context.Context) {
	if gitUpdater.batcher != nil {
		gitUpdater.batcher.Close(ctx)
	}
}

func (gitUpdater *GitUpdater) observeLockWait(app string, d time.Duration) {
	if gitUpdater.metrics != nil {
		gitUpdater.metrics.ObserveGitLockWaitDuration(app, d.Seconds())
	}
}

// countSkippedWriteback records that a managed application's write-back was skipped for
// want of a credential. task.App is safe as a label here: reaching this point means
// ArgoCD already resolved the application, so the value is not caller-chosen (see
// models.Task.MetricApp).
func (gitUpdater *GitUpdater) countSkippedWriteback(app string) {
	if gitUpdater.metrics != nil {
		gitUpdater.metrics.AddSkippedWriteback(app)
	}
}

// observeWriteback records how long the git write-back took while holding the lock.
func (gitUpdater *GitUpdater) observeWriteback(app string, d time.Duration) {
	if gitUpdater.metrics != nil {
		gitUpdater.metrics.ObserveGitWritebackDuration(app, d.Seconds())
	}
}

// UpdateIfNeeded updates the git repository if the application is managed by the
// watcher and has valid credentials. isSuperseded is an optional (at most one)
// predicate forwarded to the write-back retry loop so a task superseded by a
// newer deployment aborts instead of committing a stale image tag.
func (gitUpdater *GitUpdater) UpdateIfNeeded(app *models.Application, task models.Task, isSuperseded ...func() bool) error {
	if !app.IsManagedByWatcher() {
		slog.Debug("Skipping git repo update: application is not managed by the watcher.", "id", task.Id)
		return nil
	}

	// The managed annotation asks for write-back, so a task that cannot authorize it is a
	// misconfiguration, not a routine skip. Warn rather than debug because the rollout
	// that follows fails blaming the image or the timeout — never the missing credential —
	// so this is the only place the real cause is still known.
	if !task.Validated {
		slog.Warn("Skipping git repo update: application is managed by the watcher but the task presented no valid credential.",
			"app", task.App, "id", task.Id)
		gitUpdater.countSkippedWriteback(task.App)
		return nil
	}

	gitopsRepo, err := models.NewGitopsRepo(app, gitUpdater.repoCachePath)
	if err != nil {
		slog.Error("Failed to get gitops repo info", "app", task.App, "error", err, "id", task.Id)
		return err
	}

	// In batch mode the batcher owns the lock, clone, commit and push for the
	// whole batch, so the per-repo lock is not taken here.
	if gitUpdater.batcher != nil {
		return gitUpdater.updateViaBatcher(app, &task, &gitopsRepo, isSuperseded...)
	}

	// Timed from just before the lock request so lock-wait captures the full queueing
	// delay behind concurrent write-backs to the same repo, and the write-back timer
	// captures only the work done once the lock is held.
	lockRequested := time.Now()
	gitUpdateFunc := func() error {
		gitUpdater.observeLockWait(task.App, time.Since(lockRequested))
		workStart := time.Now()
		defer func() { gitUpdater.observeWriteback(task.App, time.Since(workStart)) }()
		slog.Debug("Application managed by watcher. Initiating git repo update.", "id", task.Id)
		return gitUpdater.updateGitRepo(app, &task, &gitopsRepo, isSuperseded...)
	}

	err = gitUpdater.locker.WithLock(gitopsRepo.RepoUrl, gitUpdateFunc)
	if err != nil {
		slog.Error("Failed git repo update", "app", task.App, "error", err, "id", task.Id)
		return err
	}

	return nil
}

// updateViaBatcher enqueues the write-back into the coalescing batcher and blocks
// until the batch it is folded into has been flushed, returning that app's
// individual outcome. task is passed by pointer; it stays alive for the duration
// of this call, which does not return until the batcher delivers the result.
func (gitUpdater *GitUpdater) updateViaBatcher(app *models.Application, task *models.Task, gitopsRepo *models.GitopsRepo, isSuperseded ...func() bool) error {
	var supersededCheck func() bool
	if len(isSuperseded) > 0 {
		supersededCheck = isSuperseded[0]
	}

	req := &batchWriteRequest{
		app:          app,
		task:         task,
		gitopsRepo:   gitopsRepo,
		isSuperseded: supersededCheck,
		resultCh:     make(chan error, 1),
	}

	if err := gitUpdater.batcher.Submit(req); err != nil {
		slog.Error("Failed batch git repo update", "app", task.App, "error", err, "id", task.Id)
		return err
	}
	return nil
}

func (gitUpdater *GitUpdater) updateGitRepo(app *models.Application, task *models.Task, gitopsRepo *models.GitopsRepo, isSuperseded ...func() bool) error {
	// context.Background() is intentional: the call stack above this point does
	// not carry a context.
	err := UpdateGitImageTag(context.Background(), app, task, gitopsRepo, updater.GitClient{}, isSuperseded...)
	if err != nil {
		slog.Error("Failed to update git repo", "error", err, "id", task.Id)
		return err
	}
	return nil
}
