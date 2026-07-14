package helpers

import (
	"math"
	"time"
)

// MulDurationSaturating returns count*unit, clamped to math.MaxInt64 to avoid the uint->int64
// overflow that a very large (client-supplied) attempt count could otherwise wrap into a negative
// duration. A non-positive unit or zero count yields 0.
func MulDurationSaturating(count uint, unit time.Duration) time.Duration {
	if unit <= 0 || count == 0 {
		return 0
	}
	// unit > 0 is guaranteed above, so the quotient is a non-negative int64 that fits in uint64.
	maxCount := uint64(math.MaxInt64 / int64(unit)) // #nosec G115 -- non-negative quotient
	if uint64(count) > maxCount {
		return time.Duration(math.MaxInt64)
	}
	// The guard above proves count fits in a positive int64, so this conversion cannot overflow.
	return time.Duration(count) * unit // #nosec G115 -- count bounded by check above
}

// CeilDivDuration returns the ceiling of d/unit as an int64, with a minimum of 1.
// A non-positive unit is treated as invalid and returns 1 to avoid division by zero.
func CeilDivDuration(d, unit time.Duration) int64 {
	if unit <= 0 {
		return 1
	}
	result := int64((d + unit - 1) / unit)
	if result <= 0 {
		return 1
	}
	return result
}

// SafeIntToUint converts an int64 to uint with overflow protection, enforcing a minimum of 1.
// On 32-bit platforms where uint is 32 bits, values exceeding math.MaxUint32 are clamped to max uint.
func SafeIntToUint(v int64) uint {
	if v <= 0 {
		return 1
	}
	maxUint := ^uint(0)
	if uint64(v) > uint64(maxUint) {
		return maxUint
	}
	return uint(v) // #nosec G115 -- overflow checked above
}
