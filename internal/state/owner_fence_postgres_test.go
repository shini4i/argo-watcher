package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/models"
)

// A replica checks its lease, then spends seconds fetching the resource tree
// before writing the outcome. If the lease lapsed in that window the new owner is
// already monitoring the same rollout, so an unfenced write lands on top of the
// outcome that owner is about to record — or has just recorded.
func TestPostgresState_SetTaskStatusRefusesATaskOwnedElsewhere(t *testing.T) {
	env := newPostgresTestEnv(t)

	task := env.addTask(t, sampleTask("app-fence"))
	// The real sequence: this replica held the claim, its lease lapsed, and a sweep
	// on another replica took the task over while the write was already under way.
	require.NoError(t, env.state.ClaimTask(task.Id))
	newOwner := env.secondReplica(t)
	require.NoError(t, newOwner.ClaimTask(task.Id))

	err := env.state.SetTaskStatus(task.Id, models.StatusFailedMessage, "written by the replica that lost it")

	require.ErrorIs(t, err, ErrTaskNotOwned)
	assertStatus(t, env.state, task.Id, models.StatusInProgressMessage)

	got, err := env.state.GetTask(task.Id)
	require.NoError(t, err)
	assert.Empty(t, got.StatusReason, "the refused write must leave no reason behind")
}

// The owner writes its own outcome, which is the whole point of holding a claim.
func TestPostgresState_SetTaskStatusAcceptsTheOwner(t *testing.T) {
	env := newPostgresTestEnv(t)

	task := env.addTask(t, sampleTask("app-fence"))
	require.NoError(t, env.state.ClaimTask(task.Id))

	require.NoError(t, env.state.SetTaskStatus(task.Id, models.StatusDeployedMessage, ""))
	assertStatus(t, env.state, task.Id, models.StatusDeployedMessage)
}

// A task nobody claimed is still writable: AddTask's claim is best-effort, so a
// database blip at submission must not leave the deployment unreportable.
func TestPostgresState_SetTaskStatusAcceptsAnUnclaimedTask(t *testing.T) {
	env := newPostgresTestEnv(t)

	task := env.addTask(t, sampleTask("app-fence"))

	require.NoError(t, env.state.SetTaskStatus(task.Id, models.StatusDeployedMessage, ""))
	assertStatus(t, env.state, task.Id, models.StatusDeployedMessage)
}

// An unknown id must stay distinguishable from a task held by someone else: one
// is a bug in the caller, the other is an ordinary handover.
func TestPostgresState_SetTaskStatusStillReportsAnUnknownTask(t *testing.T) {
	env := newPostgresTestEnv(t)

	// Rows must be present, and one of them owned elsewhere: against an empty table
	// an existence count with no id predicate would answer the same way, so the test
	// would pass while the classification had become "is the table non-empty".
	other := env.addTask(t, sampleTask("app-unrelated"))
	require.NoError(t, env.secondReplica(t).ClaimTask(other.Id))

	err := env.state.SetTaskStatus("00000000-0000-4000-8000-000000000000", models.StatusDeployedMessage, "")
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

// gorm drops a zero primary key from the WHERE clause. Before the fence that left
// no condition at all and gorm refused the statement; with the fence it would pass
// that check and rewrite every row this replica owns or nobody owns.
func TestPostgresState_SetTaskStatusRefusesTheNilUUID(t *testing.T) {
	env := newPostgresTestEnv(t)

	mine := env.addTask(t, sampleTask("app-nil-uuid"))
	require.NoError(t, env.state.ClaimTask(mine.Id))
	unclaimed := env.addTask(t, sampleTask("app-nil-uuid"))

	err := env.state.SetTaskStatus("00000000-0000-0000-0000-000000000000", models.StatusFailedMessage, "mass update")

	require.ErrorIs(t, err, ErrTaskNotFound)
	assertStatus(t, env.state, mine.Id, models.StatusInProgressMessage)
	assertStatus(t, env.state, unclaimed.Id, models.StatusInProgressMessage)
}
