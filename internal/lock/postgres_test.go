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

// Acquiring is not the only way out of the poll loop: a failing probe must return its
// error. Narrowing that exit turns a dead database into a loop that never returns —
// reachable in normal operation, because shutdown closes this pool while waiters probe.
func TestPostgresLocker_ProbeFailureEndsTheWait(t *testing.T) {
	db := newLockerTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	locker := NewPostgresLocker(db)
	done := make(chan error, 1)
	go func() {
		done <- locker.WithLock("probe-error-key", func() error {
			return errors.New("the callback must not run on a closed pool")
		})
	}()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "the callback must not run")
	case <-time.After(10 * time.Second):
		t.Fatal("WithLock kept polling instead of returning the probe error")
	}
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
		// Matched on the lock id, not on query text: the locker probes with
		// pg_TRY_advisory_xact_lock, and internal/state takes real advisory locks on
		// this same database from a suite running alongside this one.
		var pids []int
		lockID := GenerateLockID("commit-failure-key")
		require.NoError(t, killer.Raw(`
			SELECT a.pid FROM pg_locks l
			JOIN pg_stat_activity a ON a.pid = l.pid
			WHERE l.locktype = 'advisory'
			  AND l.granted
			  AND l.objsubid = 1
			  AND ((l.classid::bigint << 32) | l.objid::bigint) = ?
			  AND a.pid <> pg_backend_pid()`, lockID).Scan(&pids).Error)
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

// The pool holds two connections: the holder takes one, the waiter the other, and the
// holder then needs one to get on with its work. Blocking in pg_advisory_xact_lock
// parks the waiter's connection until the holder commits, which it then cannot — the
// deadlock. Gated on POSTGRES_DSN, skipped in short mode.
func TestPostgresLocker_WaiterDoesNotPinAConnection(t *testing.T) {
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

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(2)

	locker := NewPostgresLocker(db)
	key := "contended-pool-key"

	held := make(chan struct{})
	queried := make(chan error, 1)
	done := make(chan error, 2)

	go func() {
		done <- locker.WithLock(key, func() error {
			close(held)
			// Stands in for the write-back's own reads: work the holder does while
			// holding the lock, needing a connection the waiter must not be sitting
			// on. Reachable only if the waiter releases between probes.
			<-time.After(5 * lockBasePollInterval)
			queried <- db.Exec("SELECT 1").Error
			return nil
		})
	}()

	<-held
	go func() {
		done <- locker.WithLock(key, func() error { return nil })
	}()

	select {
	case err := <-queried:
		require.NoError(t, err, "the holder must still be able to query while a waiter waits")
	case <-time.After(30 * time.Second):
		t.Fatal("a lock waiter pinned a pooled connection; the holder could not get one")
	}

	for range 2 {
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(30 * time.Second):
			t.Fatal("WithLock did not return")
		}
	}
}

// The advisory-lock pool is bounded so the locker cannot open a connection per
// concurrent write-back, and its closer must actually close the pool it hands back.
// Gated on POSTGRES_DSN, skipped in short mode.
func TestNewPostgresLockerPool_BoundsThePoolAndClosesIt(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode.")
	}

	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN environment variable not set. Skipping integration test.")
	}

	locker, closePool, err := NewPostgresLockerPool(dsn)
	require.NoError(t, err)

	pg, ok := locker.(*PostgresLocker)
	require.True(t, ok)
	sqlDB, err := pg.db.DB()
	require.NoError(t, err)
	assert.Equal(t, lockMaxOpenConns, sqlDB.Stats().MaxOpenConnections)

	require.NoError(t, closePool())
	assert.Error(t, pg.db.Exec("SELECT 1").Error, "the closer must close the pool it returned")
}
