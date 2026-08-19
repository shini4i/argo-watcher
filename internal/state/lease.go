package state

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/state/state_models"
)

const (
	// TaskLeaseTTL is how long a replica's claim on a task stays valid without
	// being renewed. It bounds how long a deployment sits unattended after the
	// replica monitoring it dies, so it is short; it must still comfortably
	// outlast a garbage-collection pause or a slow ArgoCD call, or a live owner
	// would lose tasks it is actively monitoring.
	TaskLeaseTTL = 30 * time.Second

	// TaskLeaseRenewInterval is how often an owner extends its claim. An owner
	// gives the claim up one interval before it would lapse, so at a fifth of the
	// TTL three consecutive renewals can fail before a rollout changes hands — a
	// database blip costs nothing, while a real outage still hands the work over.
	TaskLeaseRenewInterval = TaskLeaseTTL / 5

	// TaskReapInterval is how often a replica looks for lapsed leases to take
	// over. Half the TTL keeps the worst-case unattended window at ~1.5x the TTL.
	TaskReapInterval = TaskLeaseTTL / 2

	// TaskReapBatchSize bounds how many tasks one replica takes over per tick, so
	// a replica returning after an outage picks up work gradually instead of
	// claiming every orphaned rollout at once.
	TaskReapBatchSize = 20
)

// newOwnerId returns an identifier for this process, used as the owner of task
// leases. The hostname alone would not do: a restarted pod keeps its name, and
// would then mistake the leases held by its predecessor for its own.
func newOwnerId() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("could not determine hostname for the task lease owner id: %w", err)
	}

	return fmt.Sprintf("%s/%s", hostname, uuid.NewString()), nil
}

// RenewLease reports whether this instance still holds the task. With in-memory
// state there is no second replica to lose it to, so it always does.
func (state *InMemoryState) RenewLease(_ string) (bool, error) {
	return true, nil
}

// ReleaseOwnedLeases has nothing to hand over: no other process can pick these
// tasks up.
func (state *InMemoryState) ReleaseOwnedLeases() (int64, error) {
	return 0, nil
}

// ClaimExpiredTasks never returns anything. In-memory tasks live in the process
// that accepted them, so a process that stopped monitoring them also lost them.
func (state *InMemoryState) ClaimExpiredTasks(_ int) ([]models.Task, error) {
	return nil, nil
}

// claimQueryTTLSeconds renders the lease TTL for make_interval. Every lease
// deadline is computed by Postgres rather than by the replica writing it, so
// clock skew between replicas cannot shorten or extend a claim.
const claimQueryTTLSeconds = float64(TaskLeaseTTL) / float64(time.Second)

// ClaimTask takes ownership of a task for this instance. It is called right
// after a task is accepted, so the replica that will monitor it holds the lease
// from the start rather than from its first renewal.
func (state *PostgresState) ClaimTask(id string) error {
	result := state.orm.Exec(`
		UPDATE tasks
		SET owner_id = ?, lease_expires_at = now() + make_interval(secs => ?)
		WHERE id = ?`,
		state.ownerId, claimQueryTTLSeconds, id)

	return result.Error
}

// RenewLease extends this instance's claim and reports whether it still holds
// it. A false return means another replica took the task over — the caller must
// stop monitoring it without writing a status, or it would clobber the outcome
// the new owner is about to record.
//
// Ownership is the only thing checked. A task cancelled by a newer deployment
// still belongs to this instance, and is stopped by the supersession check
// instead, so the two mechanisms never race to end the same rollout.
func (state *PostgresState) RenewLease(id string) (bool, error) {
	// Bounded because the DSN sets only connect_timeout, which does not cover a
	// statement issued on an already-established connection. Without this a stalled
	// renewal — a failover, a blackholed connection — would block the caller past
	// the lease it is meant to be extending, and the claim would lapse with nobody
	// noticing.
	ctx, cancel := context.WithTimeout(context.Background(), TaskLeaseRenewInterval)
	defer cancel()

	result := state.orm.WithContext(ctx).Exec(`
		UPDATE tasks
		SET lease_expires_at = now() + make_interval(secs => ?)
		WHERE id = ? AND owner_id = ?`,
		claimQueryTTLSeconds, id, state.ownerId)
	if result.Error != nil {
		return false, result.Error
	}

	return result.RowsAffected == 1, nil
}

// ReleaseOwnedLeases expires every claim this instance holds and reports how
// many it gave up, so the tasks it was monitoring are picked up by another
// replica on its next sweep instead of waiting out the full TTL. It is how a
// graceful shutdown hands its in-flight rollouts over.
//
// The owner is cleared as well as the deadline. Nothing waits for the monitoring
// goroutines, so one of them can outlive this call by a few instructions; while
// the row still named this instance, its next renewal would take the claim back
// and undo the handover. Unowned, that renewal reports the claim lost and the
// rollout stops without writing a status — which is what a replica on its way out
// should do.
func (state *PostgresState) ReleaseOwnedLeases() (int64, error) {
	result := state.orm.Exec(`
		UPDATE tasks
		SET owner_id = NULL, lease_expires_at = now()
		WHERE owner_id = ? AND status = ?`,
		state.ownerId, models.StatusInProgressMessage)

	return result.RowsAffected, result.Error
}

// ClaimExpiredTasks takes over up to limit in-progress tasks whose lease has
// lapsed — tasks whose previous owner died, was rolled, or released them on
// shutdown — and returns them ready to be monitored again.
//
// FOR UPDATE SKIP LOCKED is what makes this safe to run on every replica at
// once: concurrent sweeps neither block each other nor hand the same task to two
// owners.
//
// A task that was never claimed is picked up too, but only once it has gone a
// whole TTL unclaimed. Accepting a task and claiming it are two statements, and
// treating the gap between them as an abandoned task would let a sweep start a
// second monitor for a deployment the accepting replica is about to watch. The
// same grace covers tasks left in progress by a replica running a version from
// before leases existed: they carry no lease, and are taken over rather than
// stranded — during that upgrade only, both the old replica and the new owner
// watch the rollout, so it can be reported twice.
//
// That grace is the one deadline here not computed by Postgres: `created` is
// written by whichever replica accepted the task. Hosts kept in sync leave the
// difference far below a lease, but a replica whose clock runs badly behind will
// treat its own fresh rows as claimable.
func (state *PostgresState) ClaimExpiredTasks(limit int) ([]models.Task, error) {
	var claimed []state_models.TaskModel

	err := state.orm.Raw(`
		UPDATE tasks
		SET owner_id = ?, lease_expires_at = now() + make_interval(secs => ?)
		WHERE id IN (
			SELECT id FROM tasks
			WHERE status = ?
			  AND (
			        lease_expires_at < now()
			     OR (lease_expires_at IS NULL AND created < now() - make_interval(secs => ?))
			  )
			ORDER BY created
			FOR UPDATE SKIP LOCKED
			LIMIT ?
		)
		RETURNING *`,
		state.ownerId, claimQueryTTLSeconds, models.StatusInProgressMessage, claimQueryTTLSeconds, limit).
		Scan(&claimed).Error
	if err != nil {
		return nil, err
	}

	tasks := make([]models.Task, len(claimed))
	for i := range claimed {
		tasks[i] = *claimed[i].ConvertToResumedTask()
	}

	return tasks, nil
}

// ClaimTask is a no-op: the accepting process is the only one that could
// monitor the task.
func (state *InMemoryState) ClaimTask(_ string) error {
	return nil
}
