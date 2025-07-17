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
	return int64(hash.Sum64())
}
