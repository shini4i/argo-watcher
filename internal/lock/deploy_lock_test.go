package lock

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runDeployLockStoreContract exercises the behaviour every DeployLockStore
// implementation must provide, so the in-memory and Postgres stores cannot
// drift apart.
func runDeployLockStoreContract(t *testing.T, newStore func(t *testing.T) DeployLockStore) {
	t.Helper()

	t.Run("starts unlocked", func(t *testing.T) {
		state, err := newStore(t).State()
		require.NoError(t, err)
		assert.False(t, state.ManualLock)
		assert.True(t, state.OverrideUntil.IsZero())
	})

	t.Run("Lock sets the manual lock", func(t *testing.T) {
		store := newStore(t)
		require.NoError(t, store.Lock())

		state, err := store.State()
		require.NoError(t, err)
		assert.True(t, state.ManualLock)
	})

	t.Run("Release clears the manual lock", func(t *testing.T) {
		store := newStore(t)
		require.NoError(t, store.Lock())
		require.NoError(t, store.Release(time.Time{}))

		state, err := store.State()
		require.NoError(t, err)
		assert.False(t, state.ManualLock)
		assert.True(t, state.OverrideUntil.IsZero())
	})

	t.Run("Release stores the override deadline", func(t *testing.T) {
		store := newStore(t)
		until := time.Now().Add(15 * time.Minute)
		require.NoError(t, store.Release(until))

		state, err := store.State()
		require.NoError(t, err)
		assert.False(t, state.ManualLock)
		// Postgres rounds to microsecond precision, so compare with a tolerance
		// rather than requiring an exact round trip.
		assert.WithinDuration(t, until, state.OverrideUntil, time.Second)
	})

	t.Run("Lock clears a pending override", func(t *testing.T) {
		store := newStore(t)
		require.NoError(t, store.Release(time.Now().Add(15*time.Minute)))
		require.NoError(t, store.Lock())

		state, err := store.State()
		require.NoError(t, err)
		assert.True(t, state.ManualLock)
		assert.True(t, state.OverrideUntil.IsZero(), "setting the lock must drop the override")
	})
}

func TestInMemoryDeployLockStore(t *testing.T) {
	runDeployLockStoreContract(t, func(t *testing.T) DeployLockStore {
		t.Helper()
		return NewInMemoryDeployLockStore()
	})
}

// TestInMemoryDeployLockStore_ConcurrentAccess is a race-detector target: the
// store is read on every deploy request and written by API handlers.
func TestInMemoryDeployLockStore_ConcurrentAccess(t *testing.T) {
	store := NewInMemoryDeployLockStore()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// assert, not require: require calls t.FailNow, which is only
			// valid on the goroutine running the test.
			for j := 0; j < 100; j++ {
				assert.NoError(t, store.Lock())
				_, err := store.State()
				assert.NoError(t, err)
				assert.NoError(t, store.Release(time.Time{}))
			}
		}()
	}
	wg.Wait()

	state, err := store.State()
	require.NoError(t, err)
	assert.False(t, state.ManualLock)
}
