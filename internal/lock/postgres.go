package lock

import (
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"math/rand"
	"time"

	"gorm.io/driver/postgres"
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

// Poll bounds for a held lock. Capped exponential with full jitter: a short holder
// is picked up almost at once, while many waiters on one key do not re-probe in
// lockstep.
const (
	lockBasePollInterval = 50 * time.Millisecond
	lockMaxPollInterval  = 2 * time.Second
)

// WithLock takes a transaction-level advisory lock, runs f, and releases the lock
// when that transaction ends. A held lock is re-probed rather than waited on inside
// Postgres, which would pin a pooled connection for as long as the holder runs.
// Waiting is therefore unordered and unbounded; callers bound their own work.
func (p *PostgresLocker) WithLock(key string, f func() error) error {
	lockID := GenerateLockID(key)

	for attempt := uint(0); ; attempt++ {
		ran, err := p.tryWithLock(lockID, key, f)
		if ran || err != nil {
			return err
		}
		time.Sleep(lockPollDelay(attempt))
	}
}

// tryWithLock takes the advisory lock without waiting and, when it gets it, runs f
// inside the holding transaction. It reports whether f ran. Once f has run its
// outcome is the answer: gorm returns the COMMIT error when the callback succeeded,
// and returning that would tell a caller its work never ran.
func (p *PostgresLocker) tryWithLock(lockID int64, key string, f func() error) (bool, error) {
	var (
		acquired bool
		fnErr    error
	)

	txErr := p.db.Transaction(func(tx *gorm.DB) error {
		var locked bool
		if err := tx.Raw("SELECT pg_try_advisory_xact_lock(?)", lockID).Scan(&locked).Error; err != nil {
			return err
		}
		if !locked {
			return nil
		}

		acquired = true
		fnErr = f()

		// Reported outside, so f's error is never mistaken for the lock's. There
		// is nothing here a rollback would undo.
		return nil
	})

	if !acquired {
		return false, txErr
	}

	// The transaction holds nothing but the lock, which ending it releases either way.
	if txErr != nil {
		slog.Warn("the advisory-lock transaction did not end cleanly; the lock is released regardless",
			"key", key, "error", txErr)
	}

	return true, fnErr
}

// lockPollDelay returns how long to wait before re-probing a held lock, given the
// 0-based number of probes already refused.
func lockPollDelay(attempt uint) time.Duration {
	ceiling := lockBasePollInterval << attempt
	// Guard the shift: a large attempt count overflows to <= 0; saturate at the cap.
	if ceiling <= 0 || ceiling > lockMaxPollInterval {
		ceiling = lockMaxPollInterval
	}
	// math/rand is deliberate: this jitter only de-synchronises waiters. It guards
	// no secret and gates no security decision.
	// #nosec G404
	return time.Duration(rand.Int63n(int64(ceiling) + 1)) // NOSONAR: pseudorandom is safe here (poll jitter, not a security context)
}

// GenerateLockID creates a deterministic 64-bit integer from a string key, using
// FNV-1a. Every caller must derive its key through this: advisory locks share one
// flat namespace, so two hashings of the same resource would not exclude each other.
func GenerateLockID(key string) int64 {
	hasher := fnv.New64a()
	// The Write method on hash.Hash never returns an error.
	_, _ = io.WriteString(hasher, key)
	// gosec flags this as a potential overflow, but PostgreSQL's advisory lock
	// function accepts a signed 64-bit integer (bigint), so negative lock IDs
	// are perfectly valid. The conversion is deterministic and safe in this context.
	return int64(hasher.Sum64()) // #nosec G115
}

// Connection bounds for the dedicated advisory-lock pool. Small, because a holder
// needs one connection for the whole critical section and the number of gitops
// repositories written back at once is what sizes this.
const (
	lockMaxOpenConns    = 10
	lockConnMaxLifetime = 30 * time.Minute
)

// NewPostgresLockerPool opens a pool used only for advisory locks and returns the
// locker over it plus a function that closes it. The pool must be its own: a holder
// keeps its connection for the whole callback, and that callback queries the main
// pool, so one shared bounded pool would let holders starve their own queries.
func NewPostgresLockerPool(dsn string) (Locker, func() error, error) {
	db, err := gorm.Open(postgres.Open(dsn))
	if err != nil {
		return nil, nil, fmt.Errorf("could not open the advisory-lock connection pool: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("could not reach the advisory-lock connection pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(lockMaxOpenConns)
	sqlDB.SetMaxIdleConns(lockMaxOpenConns)
	sqlDB.SetConnMaxLifetime(lockConnMaxLifetime)

	return NewPostgresLocker(db), sqlDB.Close, nil
}
