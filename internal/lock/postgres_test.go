package lock

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// getTestDB creates a new database connection for testing purposes.
// It reads connection details from environment variables.
// If the required environment variables are not set, the test will be skipped.
func getTestDB(t *testing.T) *gorm.DB {
	dsn := os.Getenv("ARGO_WATCHER_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("Skipping postgres lock tests: ARGO_WATCHER_TEST_DB_DSN is not set.")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})

	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	return db
}

// TestPostgresLocker_LockUnlock verifies a simple lock and unlock cycle.
func TestPostgresLocker_LockUnlock(t *testing.T) {
	db := getTestDB(t)
	locker := NewPostgresLocker(db)
	key := fmt.Sprintf("test-key-%d", time.Now().UnixNano())

	err := locker.Lock(key)
	assert.NoError(t, err, "Lock should not return an error")

	err = locker.Unlock(key)
	assert.NoError(t, err, "Unlock should not return an error")
}

// TestPostgresLocker_Blocking verifies that the Lock method is blocking.
func TestPostgresLocker_Blocking(t *testing.T) {
	// We need two separate connections to simulate two different processes/sessions.
	db1 := getTestDB(t)
	db2 := getTestDB(t)

	locker1 := NewPostgresLocker(db1)
	locker2 := NewPostgresLocker(db2)

	key := fmt.Sprintf("blocking-key-%d", time.Now().UnixNano())
	var wg sync.WaitGroup
	secondLockAcquired := make(chan bool, 1)

	// First locker acquires the lock.
	err := locker1.Lock(key)
	assert.NoError(t, err)

	wg.Add(1)
	go func() {
		defer wg.Done()
		// This call on the second connection should block.
		err := locker2.Lock(key)
		assert.NoError(t, err)
		// Signal that the lock was acquired.
		secondLockAcquired <- true
		err = locker2.Unlock(key)
		assert.NoError(t, err)
	}()

	// Give the goroutine a moment to start and block.
	time.Sleep(200 * time.Millisecond)

	// Check that the second goroutine is still blocked.
	select {
	case <-secondLockAcquired:
		t.Fatal("Second goroutine acquired lock prematurely, it should have been blocked.")
	default:
		// This is the expected path, the channel is empty because the goroutine is blocked.
	}

	// Release the lock from the first locker.
	err = locker1.Unlock(key)
	assert.NoError(t, err)

	// Now, wait for the second goroutine to acquire the lock and finish.
	// We use a timeout to prevent the test from hanging indefinitely if something is wrong.
	select {
	case <-secondLockAcquired:
		// Lock was acquired as expected.
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for second goroutine to acquire the lock.")
	}

	wg.Wait()
}

// TestPostgresLocker_MultipleKeys ensures locks on different keys do not interfere.
func TestPostgresLocker_MultipleKeys(t *testing.T) {
	db1 := getTestDB(t)
	db2 := getTestDB(t)

	locker1 := NewPostgresLocker(db1)
	locker2 := NewPostgresLocker(db2)

	key1 := fmt.Sprintf("multi-key-1-%d", time.Now().UnixNano())
	key2 := fmt.Sprintf("multi-key-2-%d", time.Now().UnixNano())

	// Acquire lock on the first key.
	err := locker1.Lock(key1)
	assert.NoError(t, err)

	// This should not block, because it's a different key.
	err = locker2.Lock(key2)
	assert.NoError(t, err)

	// Clean up
	err = locker1.Unlock(key1)
	assert.NoError(t, err)
	err = locker2.Unlock(key2)
	assert.NoError(t, err)
}
