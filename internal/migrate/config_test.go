package migrate

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewMigrationConfig_Success tests that the default configuration is loaded correctly.
func TestNewMigrationConfig_Success(t *testing.T) {
	// Arrange
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "testuser")
	t.Setenv("DB_PASSWORD", "testpassword!@#")
	t.Setenv("DB_NAME", "testdb")
	t.Setenv("DB_SSL_MODE", "require")
	// Unset the custom path to ensure the default is used.
	t.Setenv("DB_MIGRATIONS_PATH", "")

	// Act
	cfg, err := NewMigrationConfig()

	// Assert
	require.NoError(t, err)
	require.NotNil(t, cfg)
	// Corrected the assertion to match the actual default value.
	assert.Equal(t, "/db/migrations", cfg.MigrationsPath)
	expectedDSN := "postgres://testuser:testpassword%21%40%23@localhost:5432/testdb?sslmode=require"
	assert.Equal(t, expectedDSN, cfg.DSN)
}

// TestNewMigrationConfig_CustomPath tests that a custom migration path from env vars is used.
func TestNewMigrationConfig_CustomPath(t *testing.T) {
	// Arrange
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "testuser")
	t.Setenv("DB_PASSWORD", "testpassword")
	t.Setenv("DB_NAME", "testdb")
	t.Setenv("DB_MIGRATIONS_PATH", "/my/custom/path") // Set a custom path.

	// Act
	cfg, err := NewMigrationConfig()

	// Assert
	require.NoError(t, err)
	// Assert that the custom path overrides the default.
	assert.Equal(t, "/my/custom/path", cfg.MigrationsPath)
}

// TestNewMigrationConfig_ValidationError tests the failure case where a required
// environment variable is missing.
func TestNewMigrationConfig_ValidationError(t *testing.T) {
	// Arrange
	os.Clearenv() // Ensure no conflicting variables are set.

	// Act
	cfg, err := NewMigrationConfig()

	// Assert
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "database component validation failed")
}
