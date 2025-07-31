package lock

import (
	"hash/fnv"
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
// Using FNV-1a for a fast, non-cryptographic hash suitable for this use case.
func generateLockID(key string) int64 {
	hasher := fnv.New64a()
	// The Write method on hash.Hash never returns an error.
	_, _ = io.WriteString(hasher, key)
	// gosec flags this as a potential overflow, but PostgreSQL's advisory lock
	// function accepts a signed 64-bit integer (bigint), so negative lock IDs
	// are perfectly valid. The conversion is deterministic and safe in this context.
	return int64(hasher.Sum64()) // #nosec G115
}
