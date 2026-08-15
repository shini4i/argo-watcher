package lock

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newDeployLockTestDB connects to the migrated test database. Like the other
// Postgres-backed tests in this repository it is skipped unless POSTGRES_DSN is
// set, and it assumes migrations have already been applied (task ci-migrate).
func newDeployLockTestDB(t *testing.T) *gorm.DB {
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

// reseedDeployLock restores the table to exactly what migration 000006 leaves
// behind: one unlocked row. Tests both start and end here, so the contract runs
// against the production table shape and no test leaves the shared database
// locked for whatever runs next against the same DSN.
func reseedDeployLock(t *testing.T, db *gorm.DB) {
	t.Helper()

	require.NoError(t, db.Exec("DELETE FROM deploy_lock").Error)
	require.NoError(t, db.Exec("INSERT INTO deploy_lock (id) VALUES (1)").Error)
}

func TestPostgresDeployLockStore(t *testing.T) {
	runDeployLockStoreContract(t, func(t *testing.T) DeployLockStore {
		t.Helper()

		db := newDeployLockTestDB(t)
		reseedDeployLock(t, db)
		t.Cleanup(func() { reseedDeployLock(t, db) })

		return NewPostgresDeployLockStore(db)
	})
}

// TestPostgresDeployLockStore_MissingRowIsUnlocked covers the seeded row being
// absent — a database someone truncated. It must read as "never locked" rather
// than as an error, which would otherwise fail closed and freeze all deploys.
func TestPostgresDeployLockStore_MissingRowIsUnlocked(t *testing.T) {
	db := newDeployLockTestDB(t)
	require.NoError(t, db.Exec("DELETE FROM deploy_lock").Error)
	t.Cleanup(func() { reseedDeployLock(t, db) })

	state, err := NewPostgresDeployLockStore(db).State()
	require.NoError(t, err)
	assert.False(t, state.ManualLock)
	assert.True(t, state.OverrideUntil.IsZero())
}

// TestPostgresDeployLockStore_StateSurfacesReadErrors guards the foundation of
// the fail-closed guarantee: Lockdown only rejects deploys during an outage
// because State reports the failure. A read error must never be flattened into
// the zero state the way a missing row legitimately is.
func TestPostgresDeployLockStore_StateSurfacesReadErrors(t *testing.T) {
	db := newDeployLockTestDB(t)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	_, err = NewPostgresDeployLockStore(db).State()
	assert.Error(t, err, "an unreadable lock state must surface as an error, not as 'unlocked'")
}

// TestPostgresDeployLockStore_SharedAcrossInstances is the reason this store
// exists: a lock engaged by one replica must be visible to another replica that
// shares the database.
func TestPostgresDeployLockStore_SharedAcrossInstances(t *testing.T) {
	db := newDeployLockTestDB(t)
	reseedDeployLock(t, db)
	t.Cleanup(func() { reseedDeployLock(t, db) })

	replicaA := NewPostgresDeployLockStore(db)
	replicaB := NewPostgresDeployLockStore(newDeployLockTestDB(t))

	require.NoError(t, replicaA.Lock())

	state, err := replicaB.State()
	require.NoError(t, err)
	assert.True(t, state.ManualLock, "a lock set on one replica must be visible on the others")

	until := time.Now().Add(15 * time.Minute)
	require.NoError(t, replicaB.Release(until))

	state, err = replicaA.State()
	require.NoError(t, err)
	assert.False(t, state.ManualLock)
	assert.WithinDuration(t, until, state.OverrideUntil, time.Second)
}
