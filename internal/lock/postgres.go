package lock

import (
	"crypto/sha1"
	"encoding/binary"
	"io"

	"gorm.io/gorm"
)

// PostgresLocker is an implementation of the Locker interface that uses
// PostgreSQL advisory locks.
type PostgresLocker struct {
	db *gorm.DB
}

// NewPostgresLocker creates a new instance of PostgresLocker.
func NewPostgresLocker(db *gorm.DB) Locker {
	return &PostgresLocker{db: db}
}

// WithLock acquires a transaction-level advisory lock, executes the function,
// and releases the lock upon transaction commit or rollback.
func (p *PostgresLocker) WithLock(key string, f func() error) error {
	return p.db.Transaction(func(tx *gorm.DB) error {
		lockID := generateLockID(key)
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", lockID).Error; err != nil {
			return err
		}
		// The lock is released automatically when the transaction ends.
		return f()
	})
}

// generateLockID creates a deterministic 64-bit integer from a string key.
// Using a hash prevents collisions and allows arbitrary string keys.
func generateLockID(key string) int64 {
	hasher := sha1.New()
	_, _ = io.WriteString(hasher, key)
	// We only need the first 8 bytes for a 64-bit integer.
	return int64(binary.BigEndian.Uint64(hasher.Sum(nil)[:8]))
}
