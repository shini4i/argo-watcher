package server

import (
	"log/slog"
	"time"

	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/state"
)

// StartTaskReaper launches a background goroutine that takes over deployments
// abandoned by another replica — one that crashed, was rolled, or released its
// claims on shutdown — and resumes monitoring them here.
//
// Without it a deployment whose replica disappeared is watched by nobody: no
// status is ever written, no git write-back happens, and the task sits in
// progress until the obsolete-task sweep gives up on it an hour later.
//
// With in-memory state there is nothing to take over — the tasks died with the
// process that held them — so the sweeps simply come back empty. The goroutine is
// tracked by connWg and stops when the shutdown channel is closed.
func (env *Env) StartTaskReaper() {
	env.connWg.Add(1)
	go func() {
		defer env.connWg.Done()
		reapAbandonedTasks(env.shutdownCh, env.isDraining, state.TaskReapInterval, env.argo.State, env.updater.ResumeRollout)
	}()
}

// reapAbandonedTasks claims lapsed tasks on every tick and hands each one to
// resume. A sweep that fails is logged and retried on the next tick: an
// unreachable database is an outage to ride out, not a reason to stop taking
// over abandoned deployments. Dependencies are parameters so the loop can be
// unit-tested. It runs until stop is closed.
func reapAbandonedTasks(stop <-chan struct{}, draining func() bool, interval time.Duration, repository state.TaskRepository, resume func(models.Task)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			// Draining begins several seconds before the shutdown channel closes.
			// Claiming in that window takes deployments this replica cannot finish: it
			// releases them again moments later, and a resume that outlives the
			// write-back drain fails on the closed batcher and writes a terminal status
			// no other replica will revisit.
			if draining() {
				continue
			}

			claimed, err := repository.ClaimExpiredTasks(state.TaskReapBatchSize)
			if err != nil {
				slog.Error("Failed to look for deployments abandoned by other replicas", "error", err)
				continue
			}

			if len(claimed) > 0 {
				slog.Info("Took over deployments abandoned by another replica", "count", len(claimed))
			}

			for _, task := range claimed {
				go resumeSafely(task, draining, resume)
			}
		}
	}
}

// releaseTaskLeases gives up this replica's claims so the deployments it was
// monitoring are taken over by another replica on its next sweep, rather than
// waiting out their leases. It reports the outcome rather than returning it:
// shutdown proceeds either way, and a claim that could not be released still
// lapses on its own.
func (env *Env) releaseTaskLeases() {
	if env.argo == nil || env.argo.State == nil {
		return
	}

	released, err := env.argo.State.ReleaseOwnedLeases()
	if err != nil {
		slog.Error("failed to hand over in-flight deployments", "error", err)
		return
	}

	if released > 0 {
		slog.Info("handed in-flight deployments to the other replicas", "count", released)
	}
}

// resumeSafely runs resume and contains a panic to the one task that caused it.
// A task is only ever abandoned back to the other replicas, so a panic that took
// the process down would be re-claimed elsewhere and take that replica down too,
// walking one bad deployment through the whole fleet.
//
// Draining is re-checked here because it can begin between the sweep's own check
// and this goroutine starting. Monitoring a rollout this replica cannot finish is
// worse than not starting it: once shutdown closes the write-back batcher, the
// resumed task's write-back fails and records a terminal status for a deployment
// that is otherwise healthy. Giving up before starting leaves the claim to be
// released with the rest, so another replica takes it instead.
func resumeSafely(task models.Task, draining func() bool, resume func(models.Task)) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("Resuming an abandoned deployment panicked", "id", task.Id, "app", task.App, "panic", recovered)
		}
	}()

	if draining() {
		slog.Info("Not resuming a claimed deployment: this replica is shutting down", "id", task.Id, "app", task.App)
		return
	}

	resume(task)
}
