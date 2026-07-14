package helpers

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMulDurationSaturating(t *testing.T) {
	testCases := []struct {
		name     string
		count    uint
		unit     time.Duration
		expected time.Duration
	}{
		{name: "normal", count: 3, unit: 15 * time.Second, expected: 45 * time.Second},
		{name: "zeroCount", count: 0, unit: time.Second, expected: 0},
		{name: "zeroUnit", count: 5, unit: 0, expected: 0},
		{name: "negativeUnit", count: 5, unit: -time.Second, expected: 0},
		{name: "overflowClampsToMaxInt64", count: math.MaxUint32, unit: time.Hour, expected: time.Duration(math.MaxInt64)},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := MulDurationSaturating(tc.count, tc.unit)
			assert.Equal(t, tc.expected, got)
			assert.GreaterOrEqual(t, got, time.Duration(0), "deadline must never be negative")
		})
	}
}

func TestCeilDivDuration(t *testing.T) {
	testCases := []struct {
		name     string
		d        time.Duration
		unit     time.Duration
		expected int64
	}{
		{
			name:     "exact multiple",
			d:        30 * time.Second,
			unit:     15 * time.Second,
			expected: 2,
		},
		{
			name:     "rounds up on remainder",
			d:        31 * time.Second,
			unit:     15 * time.Second,
			expected: 3,
		},
		{
			name:     "single unit",
			d:        15 * time.Second,
			unit:     15 * time.Second,
			expected: 1,
		},
		{
			name:     "duration smaller than unit returns 1",
			d:        5 * time.Second,
			unit:     15 * time.Second,
			expected: 1,
		},
		{
			name:     "zero duration returns minimum of 1",
			d:        0,
			unit:     15 * time.Second,
			expected: 1,
		},
		{
			name:     "negative duration returns minimum of 1",
			d:        -10 * time.Second,
			unit:     15 * time.Second,
			expected: 1,
		},
		{
			name:     "large duration",
			d:        225 * time.Second,
			unit:     15 * time.Second,
			expected: 15,
		},
		{
			name:     "millisecond precision",
			d:        1500 * time.Millisecond,
			unit:     time.Second,
			expected: 2,
		},
		{
			name:     "zero unit returns 1",
			d:        30 * time.Second,
			unit:     0,
			expected: 1,
		},
		{
			name:     "negative unit returns 1",
			d:        30 * time.Second,
			unit:     -5 * time.Second,
			expected: 1,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			result := CeilDivDuration(tc.d, tc.unit)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestSafeIntToUint(t *testing.T) {
	testCases := []struct {
		name     string
		input    int64
		expected uint
	}{
		{name: "positive value", input: 42, expected: 42},
		{name: "zero returns 1", input: 0, expected: 1},
		{name: "negative returns 1", input: -5, expected: 1},
		{name: "one stays one", input: 1, expected: 1},
		{name: "large value fits", input: 1000000, expected: 1000000},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			result := SafeIntToUint(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}
