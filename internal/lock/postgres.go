package lock

import (
	"hash/crc64"
	"io"

	"gorm.io/gorm"
)

// PostgresLocker implements the Locker interface using PostgreSQL advisory locks.
type PostgresLocker struct {
	db *gorm.DB
}

// NewPostgresLocker creates a new instance of PostgresLocker.
func NewPostgresLocker(db *gorm.DB) Locker {
	return &PostgresLocker{
		db: db,
	}
}

// Lock acquires a session-level advisory lock for the given key.
// It is a blocking call and will wait until the lock is available.
func (p *PostgresLocker) Lock(key string) error {
	lockID := generateLockID(key)
	// pg_advisory_lock is a blocking function.
	tx := p.db.Exec("SELECT pg_advisory_lock(?)", lockID)
	return tx.Error
}

// Unlock releases a session-level advisory lock for the given key.
func (p *PostgresLocker) Unlock(key string) error {
	lockID := generateLockID(key)
	tx := p.db.Exec("SELECT pg_advisory_unlock(?)", lockID)
	return tx.Error
}

// generateLockID creates a deterministic 64-bit integer from a string key.
func generateLockID(key string) int64 {
	// Using crc64 for a fast and uniform hash distribution.
	// We use the ISO table for a standard polynomial.
	table := crc64.MakeTable(crc64.ISO)
	hash := crc64.New(table)
	_, _ = io.WriteString(hash, key) // WriteString on crc64 never returns an error

	// Gosec flags this as a potential overflow. We are intentionally ignoring it because:
	// 1. PostgreSQL's pg_advisory_lock function accepts a `bigint` (int64), which can be negative.
	// 2. The wrapping behavior of the conversion is deterministic. The same input string will
	//    always produce the same int64 output, which is the only requirement for the lock to work.
	// Therefore, this is a false positive in the context of this specific function call.
	//gosec:ignore G115
	return int64(hash.Sum64())
}
