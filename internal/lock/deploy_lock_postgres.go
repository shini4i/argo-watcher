package lock

import (
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"
)

// deployLockRowID is the primary key of the single row holding the deploy lock
// state. The table is constrained to that one row (see the 000006 migration),
// so every replica reads and writes the same record.
const deployLockRowID = 1

// PostgresDeployLockStore persists the deploy lock state in Postgres, making a
// lock set on one replica effective on all of them.
type PostgresDeployLockStore struct {
	db *gorm.DB
}

func NewPostgresDeployLockStore(db *gorm.DB) DeployLockStore {
	return &PostgresDeployLockStore{db: db}
}

// State reads the shared deploy lock state. A missing row is reported as the
// zero state (unlocked): the migration seeds the row, and Lock/Release recreate
// it, so its absence means "never locked" rather than a failure.
func (s *PostgresDeployLockStore) State() (DeployLockState, error) {
	var (
		manualLock    bool
		overrideUntil sql.NullTime
	)

	row := s.db.Raw("SELECT manual_lock, override_until FROM deploy_lock WHERE id = ?", deployLockRowID).Row()
	if err := row.Scan(&manualLock, &overrideUntil); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DeployLockState{}, nil
		}
		return DeployLockState{}, err
	}

	return DeployLockState{
		ManualLock:    manualLock,
		OverrideUntil: overrideUntil.Time,
	}, nil
}

func (s *PostgresDeployLockStore) Lock() error {
	return s.write(true, time.Time{})
}

// Release clears the manual lockdown, recording overrideUntil as the deadline of
// a temporary override of an active scheduled lockdown.
func (s *PostgresDeployLockStore) Release(overrideUntil time.Time) error {
	return s.write(false, overrideUntil)
}

// write upserts the single deploy lock row. The upsert (rather than a plain
// UPDATE) keeps the store working even if the seeded row was removed.
func (s *PostgresDeployLockStore) write(manualLock bool, overrideUntil time.Time) error {
	until := sql.NullTime{Time: overrideUntil, Valid: !overrideUntil.IsZero()}

	return s.db.Exec(`
		INSERT INTO deploy_lock (id, manual_lock, override_until)
		VALUES (?, ?, ?)
		ON CONFLICT (id) DO UPDATE
		SET manual_lock = EXCLUDED.manual_lock, override_until = EXCLUDED.override_until`,
		deployLockRowID, manualLock, until).Error
}
