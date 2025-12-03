package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseTimestampOrDefault(t *testing.T) {
	t.Run("Success - Valid Timestamp", func(t *testing.T) {
		result, err := parseTimestampOrDefault("1234567890.5", 0)
		assert.NoError(t, err)
		assert.Equal(t, 1234567890.5, result)
	})

	t.Run("Success - Empty String Returns Default", func(t *testing.T) {
		result, err := parseTimestampOrDefault("", 999.0)
		assert.NoError(t, err)
		assert.Equal(t, 999.0, result)
	})

	t.Run("Success - Integer Timestamp", func(t *testing.T) {
		result, err := parseTimestampOrDefault("1234567890", 0)
		assert.NoError(t, err)
		assert.Equal(t, 1234567890.0, result)
	})

	t.Run("Error - Invalid Timestamp Format", func(t *testing.T) {
		_, err := parseTimestampOrDefault("not-a-number", 0)
		assert.Error(t, err)
	})

	t.Run("Error - Partially Invalid Timestamp", func(t *testing.T) {
		_, err := parseTimestampOrDefault("123abc", 0)
		assert.Error(t, err)
	})
}

func TestParseBoolOrDefault(t *testing.T) {
	t.Run("Success - True Values", func(t *testing.T) {
		testCases := []string{"true", "1", "t", "T", "TRUE"}
		for _, tc := range testCases {
			result, err := parseBoolOrDefault(tc, false)
			assert.NoError(t, err, "failed for input: %s", tc)
			assert.True(t, result, "failed for input: %s", tc)
		}
	})

	t.Run("Success - False Values", func(t *testing.T) {
		testCases := []string{"false", "0", "f", "F", "FALSE"}
		for _, tc := range testCases {
			result, err := parseBoolOrDefault(tc, true)
			assert.NoError(t, err, "failed for input: %s", tc)
			assert.False(t, result, "failed for input: %s", tc)
		}
	})

	t.Run("Success - Empty String Returns Default True", func(t *testing.T) {
		result, err := parseBoolOrDefault("", true)
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("Success - Empty String Returns Default False", func(t *testing.T) {
		result, err := parseBoolOrDefault("", false)
		assert.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("Error - Invalid Bool Format", func(t *testing.T) {
		_, err := parseBoolOrDefault("not-a-bool", false)
		assert.Error(t, err)
	})

	t.Run("Error - Partially Invalid Bool", func(t *testing.T) {
		_, err := parseBoolOrDefault("yes", false)
		assert.Error(t, err)
	})
}
