package migrate

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewMigrationConfig_Success tests the happy path for loading migration configuration.
func TestNewMigrationConfig_Success(t *testing.T) {
	// Arrange: Set valid environment variables required by the config struct.
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "testuser")
	t.Setenv("DB_PASSWORD", "testpassword")
	t.Setenv("DB_NAME", "testdb")
	t.Setenv("DB_SSL_MODE", "require")

	// Act: Create a new migration configuration.
	cfg, err := NewMigrationConfig()

	// Assert: Check for errors and validate the constructed DSN.
	require.NoError(t, err, "NewMigrationConfig should not return an error with valid environment variables")
	require.NotNil(t, cfg, "Configuration object should not be nil")

	expectedDSN := "postgres://testuser:testpassword@localhost:5432/testdb?sslmode=require"
	assert.Equal(t, expectedDSN, cfg.DSN)
}

// TestNewMigrationConfig_ValidationError tests the failure case where a required
// environment variable is missing, triggering a validation error.
func TestNewMigrationConfig_ValidationError(t *testing.T) {
	// Arrange: Clear environment variables to force a validation failure.
	os.Clearenv()
	t.Setenv("DB_HOST", "localhost")
	// DB_USER is intentionally not set.

	// Act: Attempt to create a new migration configuration.
	cfg, err := NewMigrationConfig()

	// Assert: Ensure that an error is returned and the config object is nil.
	require.Error(t, err, "NewMigrationConfig should return an error when a required variable is missing")
	assert.Nil(t, cfg, "Configuration object should be nil on validation failure")
	assert.Contains(t, err.Error(), "database component validation failed", "Error message should indicate a validation failure")
}
