package argocd

import (
	"context"
	"fmt"
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
}

// NewGitUpdater creates a GitUpdater instance. metrics may be nil, in which case
// duration observation is skipped.
func NewGitUpdater(locker lock.Locker, repoCachePath string, metrics prometheus.MetricsInterface) *GitUpdater {
	return &GitUpdater{
		locker:        locker,
		repoCachePath: repoCachePath,
		metrics:       metrics,
	}
}

// observeLockWait records how long a task waited to acquire the per-repo lock.
func (gitUpdater *GitUpdater) observeLockWait(app string, d time.Duration) {
	if gitUpdater.metrics != nil {
		gitUpdater.metrics.ObserveGitLockWaitDuration(app, d.Seconds())
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
	if !app.IsManagedByWatcher() || !task.Validated {
		slog.Debug("Skipping git repo update: application not managed by watcher or task not validated.", "id", task.Id)
		return nil
	}

	gitopsRepo, err := models.NewGitopsRepo(app, gitUpdater.repoCachePath)
	if err != nil {
		slog.Error(fmt.Sprintf("Failed to get gitops repo info for app %s: %s", task.App, err), "id", task.Id)
		return err
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
		slog.Error(fmt.Sprintf("Failed git repo update for app %s: %s", task.App, err), "id", task.Id)
		return err
	}

	return nil
}

func (gitUpdater *GitUpdater) updateGitRepo(app *models.Application, task *models.Task, gitopsRepo *models.GitopsRepo, isSuperseded ...func() bool) error {
	// context.Background() is intentional: the call stack above this point
	// does not carry a context. Propagating a cancellable context from
	// WaitForRollout is a future improvement.
	err := UpdateGitImageTag(context.Background(), app, task, gitopsRepo, updater.GitClient{}, isSuperseded...)
	if err != nil {
		slog.Error(fmt.Sprintf("Failed to update git repo. Error: %s", err.Error()), "id", task.Id)
		return err
	}
	return nil
}
