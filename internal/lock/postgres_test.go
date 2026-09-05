package lock

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Requires a running PostgreSQL database; gated on POSTGRES_DSN and skipped in short mode.
func TestPostgresLocker(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode.")
	}

	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN environment variable not set. Skipping integration test.")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	assert.NoError(t, err)

	locker := NewPostgresLocker(db)
	key := "integration-test-key"
	var wg sync.WaitGroup
	// The buffer must be large enough to hold all sent values before they are read.
	executionOrder := make(chan int, 4)

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := locker.WithLock(key, func() error {
			executionOrder <- 1
			// Hold the lock to ensure the second goroutine has to wait.
			time.Sleep(100 * time.Millisecond)
			executionOrder <- 1
			return nil
		})
		assert.NoError(t, err)
	}()

	// Give the first goroutine a moment to acquire the lock
	time.Sleep(10 * time.Millisecond)

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := locker.WithLock(key, func() error {
			executionOrder <- 2
			executionOrder <- 2
			return nil
		})
		assert.NoError(t, err)
	}()

	wg.Wait()
	close(executionOrder)

	var order []int
	for i := range executionOrder {
		order = append(order, i)
	}

	expectedOrder := []int{1, 1, 2, 2}
	assert.Equal(t, expectedOrder, order, "The second goroutine should not have started until the first one committed its transaction")
}

// newLockerTestDB opens the shared integration database, or skips the test.
func newLockerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in short mode.")
	}

	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN environment variable not set. Skipping integration test.")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	return db
}

// A COMMIT that fails once the callback has run must not be reported as a lock
// failure: the caller cannot tell the two apart from one error, and concluding
// the work never ran fails a write-back whose commit is already on the remote.
// The kill reproduces what a pooler's idle-in-transaction timeout does.
func TestPostgresLocker_CommitFailureAfterCallbackKeepsCallbackOutcome(t *testing.T) {
	db := newLockerTestDB(t)
	killer := newLockerTestDB(t)

	locker := NewPostgresLocker(db)

	var ran bool
	err := locker.WithLock("commit-failure-key", func() error {
		ran = true
		// The session holding the lock is the only one idle in a transaction
		// that took it.
		var pids []int
		require.NoError(t, killer.Raw(`
			SELECT pid FROM pg_stat_activity
			WHERE state = 'idle in transaction'
			  AND query ILIKE '%pg_advisory_xact_lock%'
			  AND pid <> pg_backend_pid()`).Scan(&pids).Error)
		require.Len(t, pids, 1, "expected exactly one session holding the advisory lock")
		require.NoError(t, killer.Exec("SELECT pg_terminate_backend(?)", pids[0]).Error)
		return nil
	})

	require.True(t, ran, "the callback must have run for this test to mean anything")
	assert.NoError(t, err, "a COMMIT failure after a successful callback must not be reported as a lock failure")
}

// The mirror of the case above: the callback's own error is what the caller gets.
func TestPostgresLocker_CallbackErrorIsReturned(t *testing.T) {
	locker := NewPostgresLocker(newLockerTestDB(t))

	sentinel := errors.New("write-back rejected")
	err := locker.WithLock("callback-error-key", func() error { return sentinel })

	assert.ErrorIs(t, err, sentinel)
}
