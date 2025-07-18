package lock

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestNewInMemoryLocker ensures that the constructor returns a valid, non-nil Locker.
func TestNewInMemoryLocker(t *testing.T) {
	locker := NewInMemoryLocker()
	assert.NotNil(t, locker, "NewInMemoryLocker() should not return nil")
}

// TestLockUnlock ensures that a single goroutine can acquire and release a lock.
func TestLockUnlock(t *testing.T) {
	locker := NewInMemoryLocker()
	key := "test-key"

	err := locker.Lock(key)
	assert.NoError(t, err, "Lock should not return an error")

	err = locker.Unlock(key)
	assert.NoError(t, err, "Unlock should not return an error")
}

// TestBlockingLock verifies that the Lock method is indeed blocking.
func TestBlockingLock(t *testing.T) {
	locker := NewInMemoryLocker()
	key := "blocking-key"
	var wg sync.WaitGroup
	var secondLockAcquired bool

	// First goroutine acquires the lock
	err := locker.Lock(key)
	assert.NoError(t, err)

	wg.Add(1)
	go func() {
		defer wg.Done()
		// This should block until the first lock is released.
		err := locker.Lock(key)
		assert.NoError(t, err)
		secondLockAcquired = true
		err = locker.Unlock(key)
		assert.NoError(t, err)
	}()

	// Give the second goroutine a moment to try and acquire the lock
	time.Sleep(100 * time.Millisecond)
	assert.False(t, secondLockAcquired, "Second goroutine should be blocked and not have acquired the lock yet")

	// Release the first lock
	err = locker.Unlock(key)
	assert.NoError(t, err)

	// Wait for the second goroutine to finish
	wg.Wait()
	assert.True(t, secondLockAcquired, "Second goroutine should have acquired the lock after it was released")
}

// TestConcurrentAccess tests that multiple goroutines trying to lock the same key
// do so sequentially, not concurrently.
func TestConcurrentAccess(t *testing.T) {
	locker := NewInMemoryLocker()
	key := "concurrent-key"
	var counter int
	var wg sync.WaitGroup
	numRoutines := 5

	wg.Add(numRoutines)
	for i := 0; i < numRoutines; i++ {
		go func() {
			defer wg.Done()
			err := locker.Lock(key)
			assert.NoError(t, err)

			// Simulate some work
			current := counter
			time.Sleep(10 * time.Millisecond)
			counter = current + 1

			err = locker.Unlock(key)
			assert.NoError(t, err)
		}()
	}

	wg.Wait()

	// If locking was sequential, the counter should be exactly the number of routines.
	// If it was concurrent, we would have a race condition and the final value would be unpredictable.
	assert.Equal(t, numRoutines, counter, "Counter should be incremented sequentially by each goroutine")
}

// TestMultipleKeys ensures that locks for different keys do not interfere with each other.
func TestMultipleKeys(t *testing.T) {
	locker := NewInMemoryLocker()
	key1 := "key1"
	key2 := "key2"
	var wg sync.WaitGroup

	// Lock the first key
	err := locker.Lock(key1)
	assert.NoError(t, err)

	wg.Add(1)
	go func() {
		defer wg.Done()
		// This should not block, as it's a different key.
		err := locker.Lock(key2)
		assert.NoError(t, err)
		err = locker.Unlock(key2)
		assert.NoError(t, err)
	}()

	// Wait for the second goroutine, it should finish almost immediately
	wg.Wait()

	// Release the first lock
	err = locker.Unlock(key1)
	assert.NoError(t, err)
}
