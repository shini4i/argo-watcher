package lock

import (
	"hash/fnv"
	"io"
	"log/slog"

	"gorm.io/gorm"
)

// PostgresLocker is a Locker backed by PostgreSQL advisory locks.
type PostgresLocker struct {
	db *gorm.DB
}

// NewPostgresLocker creates a new instance of PostgresLocker.
func NewPostgresLocker(db *gorm.DB) Locker {
	return &PostgresLocker{db: db}
}

// WithLock acquires a transaction-level advisory lock, executes the function,
// and releases the lock upon transaction commit or rollback. Once f has run its
// outcome is the answer: gorm returns the COMMIT error when the callback
// succeeded, and returning that would tell a caller its work never ran.
func (p *PostgresLocker) WithLock(key string, f func() error) error {
	var (
		acquired bool
		fnErr    error
	)

	txErr := p.db.Transaction(func(tx *gorm.DB) error {
		lockID := generateLockID(key)
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", lockID).Error; err != nil {
			return err
		}

		acquired = true
		fnErr = f()

		// Reported outside, so f's error is never mistaken for the lock's. There
		// is nothing here a rollback would undo.
		return nil
	})

	if !acquired {
		return txErr
	}

	// The transaction holds nothing but the lock, which ending it releases either way.
	if txErr != nil {
		slog.Warn("the advisory-lock transaction did not end cleanly; the lock is released regardless",
			"key", key, "error", txErr)
	}

	return fnErr
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
