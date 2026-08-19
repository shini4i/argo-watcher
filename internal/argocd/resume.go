package argocd

import (
	"log/slog"
	"time"

	"github.com/shini4i/argo-watcher/internal/helpers"
	"github.com/shini4i/argo-watcher/internal/models"
)

// StaleResumedTaskReason is the status reason recorded for a task whose rollout
// window ran out while no replica was monitoring it.
const StaleResumedTaskReason = "Deployment window elapsed while the task was unattended; marked aborted by argo-watcher."

// ResumeRollout takes over a task claimed from a replica that stopped monitoring
// it — one that crashed, was rolled, or released its claim on shutdown — and
// watches the rollout through to its outcome.
//
// The task keeps the deadline it was accepted with rather than starting a fresh
// one, so passing through any number of replicas cannot extend how long a
// deployment polls. A task whose window has already elapsed is aborted here
// instead of being resumed, which is the outcome it would reach on the first
// poll anyway.
func (updater *ArgoStatusUpdater) ResumeRollout(task models.Task) {
	remaining, resumable := updater.monitor.remainingWindow(task, time.Now())
	if !resumable {
		slog.Info("Not resuming a deployment whose window already elapsed", "id", task.Id, "app", task.App)
		updater.monitor.abortStaleTask(&task)
		// The replica that accepted this deployment announced it as started, and this
		// is where it ends, so the terminal notification is owed here as much as on
		// any other final status.
		sendNotification(task, updater.notifier)
		return
	}

	slog.Info("Resuming a deployment abandoned by another replica",
		"id", task.Id, "app", task.App, "remaining", remaining)

	task.Timeout = int(remaining.Seconds())
	updater.WaitForRollout(task, true)
}

// remainingWindow returns how much of the task's rollout window is left at now,
// and whether enough remains to be worth resuming. The window is the per-task
// timeout when the client set one, and this instance's default otherwise; it is
// measured from the task's creation, which is the instant the deployment was
// accepted.
//
// Unlike the lease deadlines, which Postgres computes so replica clock skew
// cannot alter them, this compares the resuming replica's clock against a
// creation timestamp written by whichever replica accepted the task. Hosts kept
// in sync leave that difference far below the granularity that matters here.
func (monitor *DeploymentMonitor) remainingWindow(task models.Task, now time.Time) (time.Duration, bool) {
	window := monitor.defaultWindow()
	if task.Timeout > 0 {
		window = time.Duration(task.Timeout) * time.Second
	}

	elapsed := now.Sub(time.Unix(int64(task.Created), 0))
	remaining := window - elapsed

	// Anything under a second is not worth resuming, and must not be: the rollout
	// deadline is configured in whole seconds, where a sub-second remainder rounds
	// down to zero — which reads as "no per-task timeout" and would hand the task a
	// fresh instance-default window, the very thing this bound exists to prevent.
	return remaining, remaining >= time.Second
}

// defaultWindow is how long this instance polls a rollout when the task carries
// no timeout of its own. It shares resolvedDelay and resolvedDefaultAttempts with
// configureRetryOptions so a resumed task is measured against the same window the
// rollout would actually be given.
func (monitor *DeploymentMonitor) defaultWindow() time.Duration {
	delay := monitor.resolvedDelay()

	return helpers.MulDurationSaturating(monitor.resolvedDefaultAttempts(delay), delay)
}

// abortStaleTask records the terminal status for a task that outlived its
// rollout window while unattended.
func (monitor *DeploymentMonitor) abortStaleTask(task *models.Task) {
	monitor.argo.metrics.AddFailedDeployment(task.MetricApp())

	if err := monitor.argo.State.SetTaskStatus(task.Id, models.StatusAborted, StaleResumedTaskReason); err != nil {
		slog.Error("Failed to change task status", "error", err, "id", task.Id)
	}
	task.Status = models.StatusAborted
}
