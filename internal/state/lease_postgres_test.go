package state

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/models"
)

// secondReplica returns a state sharing the database but identifying as a
// different process, which is what makes cross-replica takeover observable.
func (env *postgresTestEnv) secondReplica(t *testing.T) *PostgresState {
	t.Helper()

	ownerId, err := newOwnerId()
	require.NoError(t, err)

	return &PostgresState{orm: env.state.orm, ownerId: ownerId}
}

// expireLease backdates a task's lease so a sweep treats it as abandoned,
// without making the test wait out the real TTL.
func (env *postgresTestEnv) expireLease(t *testing.T, id string) {
	t.Helper()
	require.NoError(t, env.state.orm.Exec(
		"UPDATE tasks SET lease_expires_at = now() - interval '1 second' WHERE id = ?", id).Error)
}

func TestPostgresState_ClaimTask(t *testing.T) {
	env := newPostgresTestEnv(t)

	inserted := env.addTask(t, sampleTask("Claimed"))
	require.NoError(t, env.state.ClaimTask(inserted.Id))

	stored := env.storedModel(t, inserted.Id)
	require.True(t, stored.OwnerId.Valid)
	assert.Equal(t, env.state.ownerId, stored.OwnerId.String)
	require.True(t, stored.LeaseExpiresAt.Valid)
	assert.True(t, stored.LeaseExpiresAt.Time.After(stored.Created),
		"the claim must be dated from the database clock, not left at the epoch")
}

func TestPostgresState_RenewLease(t *testing.T) {
	env := newPostgresTestEnv(t)

	t.Run("the owner keeps its claim", func(t *testing.T) {
		inserted := env.addTask(t, sampleTask("Renewed"))
		require.NoError(t, env.state.ClaimTask(inserted.Id))

		held, err := env.state.RenewLease(inserted.Id)
		require.NoError(t, err)
		assert.True(t, held)
	})

	t.Run("a replica that lost the task learns it on the next renewal", func(t *testing.T) {
		inserted := env.addTask(t, sampleTask("Stolen"))
		require.NoError(t, env.state.ClaimTask(inserted.Id))

		other := env.secondReplica(t)
		require.NoError(t, other.ClaimTask(inserted.Id))

		held, err := env.state.RenewLease(inserted.Id)
		require.NoError(t, err)
		assert.False(t, held,
			"the previous owner must stop rather than clobber the new owner's outcome")
	})

	t.Run("a cancelled task still belongs to its owner", func(t *testing.T) {
		inserted := env.addTask(t, sampleTask("Cancelled"))
		require.NoError(t, env.state.ClaimTask(inserted.Id))
		require.NoError(t, env.state.SetTaskStatus(inserted.Id, models.StatusCancelledMessage, "superseded"))

		held, err := env.state.RenewLease(inserted.Id)
		require.NoError(t, err)
		assert.True(t, held,
			"supersession stops the rollout through its own check; the lease must not double as one")
	})
}

func TestPostgresState_ClaimExpiredTasks(t *testing.T) {
	env := newPostgresTestEnv(t)

	t.Run("a live claim is left alone", func(t *testing.T) {
		inserted := env.addTask(t, sampleTask("Live"))
		require.NoError(t, env.state.ClaimTask(inserted.Id))

		claimed, err := env.secondReplica(t).ClaimExpiredTasks(TaskReapBatchSize)
		require.NoError(t, err)
		assert.NotContains(t, taskIds(claimed), inserted.Id)
	})

	t.Run("an abandoned task is taken over with everything the rollout needs", func(t *testing.T) {
		refresh := false
		task := sampleTask("Abandoned")
		task.Validated = true
		task.Timeout = 600
		task.Refresh = &refresh
		inserted := env.addTask(t, task)
		require.NoError(t, env.state.ClaimTask(inserted.Id))
		env.expireLease(t, inserted.Id)

		other := env.secondReplica(t)
		claimed, err := other.ClaimExpiredTasks(TaskReapBatchSize)
		require.NoError(t, err)

		resumed := findTask(claimed, inserted.Id)
		require.NotNil(t, resumed, "an expired lease must be reclaimable")
		assert.True(t, resumed.Validated, "a resumed task that loses authority skips its write-back")
		assert.Equal(t, 600, resumed.Timeout)
		require.NotNil(t, resumed.Refresh)
		assert.False(t, *resumed.Refresh)

		held, err := env.state.RenewLease(inserted.Id)
		require.NoError(t, err)
		assert.False(t, held, "the abandoning replica must not reclaim what was taken from it")
	})

	t.Run("a released task is taken over without waiting out the lease", func(t *testing.T) {
		require.NoError(t, env.state.orm.Exec("TRUNCATE TABLE tasks").Error)

		inserted := env.addTask(t, sampleTask("Released"))
		require.NoError(t, env.state.ClaimTask(inserted.Id))
		released, err := env.state.ReleaseOwnedLeases()
		require.NoError(t, err)
		assert.Equal(t, int64(1), released)

		claimed, err := env.secondReplica(t).ClaimExpiredTasks(TaskReapBatchSize)
		require.NoError(t, err)
		assert.NotNil(t, findTask(claimed, inserted.Id))
	})

	// A task accepted moments ago has not been abandoned — it is waiting for the
	// accepting replica's own claim, one statement behind. Claiming it here would
	// put two monitors on one rollout.
	t.Run("a just-accepted task is left for the replica that accepted it", func(t *testing.T) {
		require.NoError(t, env.state.orm.Exec("TRUNCATE TABLE tasks").Error)

		inserted := env.addTask(t, sampleTask("JustAccepted"))

		claimed, err := env.secondReplica(t).ClaimExpiredTasks(TaskReapBatchSize)
		require.NoError(t, err)
		assert.Nil(t, findTask(claimed, inserted.Id),
			"an unclaimed task is only abandoned once it has gone a whole lease unclaimed")
	})

	// The other half: a task whose claim never landed must not be stranded.
	t.Run("a long-unclaimed task is eventually taken over", func(t *testing.T) {
		require.NoError(t, env.state.orm.Exec("TRUNCATE TABLE tasks").Error)

		inserted := env.addTask(t, sampleTask("NeverClaimed"))
		require.NoError(t, env.state.orm.Exec(
			"UPDATE tasks SET created = now() - make_interval(secs => ?) WHERE id = ?",
			claimQueryTTLSeconds+1, inserted.Id).Error)

		claimed, err := env.secondReplica(t).ClaimExpiredTasks(TaskReapBatchSize)
		require.NoError(t, err)
		assert.NotNil(t, findTask(claimed, inserted.Id))
	})

	t.Run("a finished task is never reclaimed", func(t *testing.T) {
		inserted := env.addTask(t, sampleTask("Finished"))
		require.NoError(t, env.state.ClaimTask(inserted.Id))
		env.expireLease(t, inserted.Id)
		require.NoError(t, env.state.SetTaskStatus(inserted.Id, models.StatusDeployedMessage, ""))

		claimed, err := env.secondReplica(t).ClaimExpiredTasks(TaskReapBatchSize)
		require.NoError(t, err)
		assert.Nil(t, findTask(claimed, inserted.Id))
	})

	// The scenario this guards: a replica dies mid-rollout, a newer deployment for
	// the same app lands elsewhere and commits its own tag, and only then does a
	// sweep notice the abandoned task. Resuming it would re-run its write-back and
	// commit the superseded tag over the newer one. Superseding marks the old task
	// cancelled before the new one is even inserted, so it leaves the claimable set
	// entirely — a lapsed lease on it is irrelevant.
	t.Run("a superseded task is never reclaimed", func(t *testing.T) {
		require.NoError(t, env.state.orm.Exec("TRUNCATE TABLE tasks").Error)

		old := env.addTask(t, sampleTask("Superseded"))
		require.NoError(t, env.state.ClaimTask(old.Id))
		env.expireLease(t, old.Id)

		// What accepting a newer deployment for the same app does first.
		cancelled, err := env.state.CancelInProgressTasks(
			"Superseded", []models.Image{{Image: "test", Tag: "v0.0.2"}}, "superseded", true)
		require.NoError(t, err)
		require.Equal(t, int64(1), cancelled)

		claimed, err := env.secondReplica(t).ClaimExpiredTasks(TaskReapBatchSize)
		require.NoError(t, err)
		assert.Nil(t, findTask(claimed, old.Id),
			"resuming it would commit the tag the newer deployment just replaced")
	})

	t.Run("a task already claimed is not handed to a second owner", func(t *testing.T) {
		require.NoError(t, env.state.orm.Exec("TRUNCATE TABLE tasks").Error)

		var ids []string
		for range 6 {
			inserted := env.addTask(t, sampleTask("Contended"))
			require.NoError(t, env.state.ClaimTask(inserted.Id))
			env.expireLease(t, inserted.Id)
			ids = append(ids, inserted.Id)
		}

		first, err := env.secondReplica(t).ClaimExpiredTasks(TaskReapBatchSize)
		require.NoError(t, err)
		second, err := env.secondReplica(t).ClaimExpiredTasks(TaskReapBatchSize)
		require.NoError(t, err)

		assert.Len(t, first, len(ids), "the first sweep claims what is available")
		assert.Empty(t, second, "a task already claimed must not be handed to a second owner")
	})

	// The property every replica sweeping at once depends on: SKIP LOCKED must
	// make simultaneous sweeps partition the abandoned tasks rather than block on
	// each other or hand the same rollout to two owners. Sequential sweeps cannot
	// show this — they never contend for the same rows.
	t.Run("simultaneous sweeps partition the abandoned tasks", func(t *testing.T) {
		require.NoError(t, env.state.orm.Exec("TRUNCATE TABLE tasks").Error)

		const abandoned = 24
		for range abandoned {
			inserted := env.addTask(t, sampleTask("Raced"))
			require.NoError(t, env.state.ClaimTask(inserted.Id))
			env.expireLease(t, inserted.Id)
		}

		const sweepers = 4
		var (
			start   = make(chan struct{})
			wg      sync.WaitGroup
			mu      sync.Mutex
			claimed []models.Task
			errs    []error
		)

		for range sweepers {
			replica := env.secondReplica(t)
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				got, err := replica.ClaimExpiredTasks(abandoned)

				mu.Lock()
				defer mu.Unlock()
				claimed = append(claimed, got...)
				errs = append(errs, err)
			}()
		}

		close(start)
		wg.Wait()

		for _, err := range errs {
			require.NoError(t, err, "a sweep must not fail because another was running")
		}

		seen := map[string]int{}
		for _, task := range claimed {
			seen[task.Id]++
		}
		for id, times := range seen {
			assert.Equal(t, 1, times, "task %s was handed to more than one owner", id)
		}
		assert.Len(t, seen, abandoned, "every abandoned task must be claimed by exactly one sweep")
	})

	// A bounded sweep must take the oldest first, or a replica returning from an
	// outage keeps picking up fresh orphans while the ones nearest their deadline
	// starve and are eventually aborted as stale.
	t.Run("a bounded sweep takes the oldest abandoned tasks first", func(t *testing.T) {
		require.NoError(t, env.state.orm.Exec("TRUNCATE TABLE tasks").Error)

		var ids []string
		for age := range 3 {
			inserted := env.addTask(t, sampleTask("Batched"))
			// Spread creation times so "oldest" is unambiguous.
			require.NoError(t, env.state.orm.Exec(
				"UPDATE tasks SET created = now() - make_interval(secs => ?) WHERE id = ?",
				(3-age)*3600, inserted.Id).Error)
			env.expireLease(t, inserted.Id)
			ids = append(ids, inserted.Id)
		}

		claimed, err := env.secondReplica(t).ClaimExpiredTasks(2)
		require.NoError(t, err)
		require.Len(t, claimed, 2)
		assert.ElementsMatch(t, ids[:2], taskIds(claimed),
			"the two oldest must be claimed, not an arbitrary two")
	})
}

func taskIds(tasks []models.Task) []string {
	ids := make([]string, len(tasks))
	for i := range tasks {
		ids[i] = tasks[i].Id
	}
	return ids
}

func findTask(tasks []models.Task, id string) *models.Task {
	for i := range tasks {
		if tasks[i].Id == id {
			return &tasks[i]
		}
	}
	return nil
}

// TestPostgresState_ReleaseOwnedLeases pins the half of the handover that the
// takeover tests cannot see. Expiring the deadline alone is enough for another
// replica to claim the task, so only these assertions catch the loss of
// `owner_id = NULL` — and with it the straggler renewal that would silently take
// the claim back and undo the handover.
func TestPostgresState_ReleaseOwnedLeases(t *testing.T) {
	env := newPostgresTestEnv(t)

	t.Run("the owner is cleared, not just the deadline", func(t *testing.T) {
		require.NoError(t, env.state.orm.Exec("TRUNCATE TABLE tasks").Error)

		inserted := env.addTask(t, sampleTask("Handed"))
		require.NoError(t, env.state.ClaimTask(inserted.Id))

		released, err := env.state.ReleaseOwnedLeases()
		require.NoError(t, err)
		assert.Equal(t, int64(1), released)

		assert.False(t, env.storedModel(t, inserted.Id).OwnerId.Valid,
			"a released task must name no owner")

		held, err := env.state.RenewLease(inserted.Id)
		require.NoError(t, err)
		assert.False(t, held,
			"a goroutine still running in the departing replica must not take the claim back")
	})

	t.Run("only in-progress tasks are handed over", func(t *testing.T) {
		require.NoError(t, env.state.orm.Exec("TRUNCATE TABLE tasks").Error)

		live := env.addTask(t, sampleTask("Live"))
		require.NoError(t, env.state.ClaimTask(live.Id))

		finished := env.addTask(t, sampleTask("Finished"))
		require.NoError(t, env.state.ClaimTask(finished.Id))
		require.NoError(t, env.state.SetTaskStatus(finished.Id, models.StatusDeployedMessage, ""))

		released, err := env.state.ReleaseOwnedLeases()
		require.NoError(t, err)
		assert.Equal(t, int64(1), released, "a finished task is not in flight and needs no owner")

		assert.True(t, env.storedModel(t, finished.Id).OwnerId.Valid,
			"a finished task's row is left untouched")
	})
}
