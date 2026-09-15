package lock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The locker exists on its own pool so a holder never competes with the queries its
// callback makes. A bad DSN must fail there, not hand back a half-built locker.
func TestNewPostgresLockerPool_RejectsAnUnusableDSN(t *testing.T) {
	locker, closePool, err := NewPostgresLockerPool("not-a-dsn")

	require.Error(t, err)
	assert.Nil(t, locker)
	assert.Nil(t, closePool)
	assert.Contains(t, err.Error(), "advisory-lock")
}

func TestLockPollDelay(t *testing.T) {
	t.Run("stays within the jittered ceiling for the attempt", func(t *testing.T) {
		for attempt := uint(0); attempt < 6; attempt++ {
			ceiling := lockBasePollInterval << attempt
			if ceiling > lockMaxPollInterval {
				ceiling = lockMaxPollInterval
			}
			for range 200 {
				delay := lockPollDelay(attempt)
				assert.GreaterOrEqual(t, delay, time.Duration(0))
				assert.LessOrEqual(t, delay, ceiling)
			}
		}
	})

	// The shift overflows to a non-positive ceiling long before this; without the
	// guard rand.Int63n would panic on a non-positive bound.
	t.Run("saturates instead of overflowing", func(t *testing.T) {
		for _, attempt := range []uint{60, 63, 64, 1000} {
			delay := lockPollDelay(attempt)
			assert.GreaterOrEqual(t, delay, time.Duration(0))
			assert.LessOrEqual(t, delay, lockMaxPollInterval)
		}
	})
}
