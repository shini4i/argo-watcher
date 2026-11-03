package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOwnerId_IsUniquePerProcess(t *testing.T) {
	first, err := newOwnerId()
	require.NoError(t, err)
	second, err := newOwnerId()
	require.NoError(t, err)

	assert.NotEmpty(t, first)
	assert.NotEqual(t, first, second,
		"a restarted pod reuses its name, so the id must not be the hostname alone")
}

// TestInMemoryState_LeasesAreNoOps documents the single-process contract: there
// is no second replica to lose a claim to, and a crash takes the tasks with it,
// so nothing is ever reclaimable.
func TestInMemoryState_LeasesAreNoOps(t *testing.T) {
	state := &InMemoryState{}
	task, err := state.AddTask(createTestTask("Leased"))
	require.NoError(t, err)

	held, err := state.RenewLease(task.Id)
	require.NoError(t, err)
	assert.True(t, held, "a single process can never lose its claim")

	released, err := state.ReleaseOwnedLeases()
	require.NoError(t, err)
	assert.Zero(t, released)

	reclaimed, err := state.ClaimExpiredTasks(10)
	require.NoError(t, err)
	assert.Empty(t, reclaimed)

	require.NoError(t, state.RecordConfirmedApp(task.Id, []byte{0x01}))
}

// TestInMemoryState_RecordConfirmedApp checks the task lookup sees the confirmation,
// which is what lets the unauthenticated_reads counter name the app.
func TestInMemoryState_RecordConfirmedApp(t *testing.T) {
	state := &InMemoryState{}
	task, err := state.AddTask(createTestTask("Confirmed"))
	require.NoError(t, err)
	other, err := state.AddTask(createTestTask("Other"))
	require.NoError(t, err)

	require.NoError(t, state.RecordConfirmedApp(task.Id, []byte{0x01}))

	stored, err := state.GetTask(task.Id)
	require.NoError(t, err)
	assert.True(t, stored.AppConfirmed)

	untouched, err := state.GetTask(other.Id)
	require.NoError(t, err)
	assert.False(t, untouched.AppConfirmed, "only the recorded task's reads may be labelled")

	require.NoError(t, state.RecordConfirmedApp("missing-id", nil))
}
