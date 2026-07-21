// internal/migrate/config_test.go
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
	assert.Equal(t, "/app/db/migrations", cfg.MigrationsPath)
	// Verify the DSN is assembled correctly: DB_SSL_MODE flows into the sslmode
	// segment, the password's special characters are URL-escaped, and the default
	// connect_timeout (10s) is appended so an unreachable database fails fast.
	assert.Equal(t, "postgres://testuser:testpassword%21%40%23@localhost:5432/testdb?sslmode=require&connect_timeout=10", cfg.DSN)
}

// TestNewMigrationConfig_ConnectTimeoutOverride verifies DB_CONNECT_TIMEOUT flows
// into the connect_timeout segment of the migration DSN.
func TestNewMigrationConfig_ConnectTimeoutOverride(t *testing.T) {
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "testuser")
	t.Setenv("DB_PASSWORD", "testpassword")
	t.Setenv("DB_NAME", "testdb")
	t.Setenv("DB_SSL_MODE", "require")
	t.Setenv("DB_CONNECT_TIMEOUT", "3")

	cfg, err := NewMigrationConfig()

	require.NoError(t, err)
	assert.Equal(t, "postgres://testuser:testpassword@localhost:5432/testdb?sslmode=require&connect_timeout=3", cfg.DSN)
}

// TestNewMigrationConfig_CustomPath tests that a custom migration path from env vars is used.
func TestNewMigrationConfig_CustomPath(t *testing.T) {
	// Arrange
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "testuser")
	t.Setenv("DB_PASSWORD", "testpassword")
	t.Setenv("DB_NAME", "testdb")
	t.Setenv("DB_MIGRATIONS_PATH", "/my/custom/path")

	// Act
	cfg, err := NewMigrationConfig()

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "/my/custom/path", cfg.MigrationsPath)
}

// TestNewMigrationConfig_ConnectTimeoutRejectsNonPositive verifies that a
// non-positive DB_CONNECT_TIMEOUT is rejected, since 0 (and negatives on libpq)
// mean "wait indefinitely" and would silently defeat the fail-fast guard.
func TestNewMigrationConfig_ConnectTimeoutRejectsNonPositive(t *testing.T) {
	for _, value := range []string{"0", "-1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("DB_HOST", "localhost")
			t.Setenv("DB_PORT", "5432")
			t.Setenv("DB_USER", "testuser")
			t.Setenv("DB_PASSWORD", "testpassword")
			t.Setenv("DB_NAME", "testdb")
			t.Setenv("DB_CONNECT_TIMEOUT", value)

			cfg, err := NewMigrationConfig()

			assert.Nil(t, cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "DB_CONNECT_TIMEOUT")
			assert.Contains(t, err.Error(), "must be at least 1 second")
		})
	}
}

// TestNewMigrationConfig_ValidationError tests the failure case where a required
// environment variable is missing. This test covers the validation error path.
func TestNewMigrationConfig_ValidationError(t *testing.T) {
	// Arrange
	os.Clearenv() // Ensure no conflicting variables are set.

	// Act
	cfg, err := NewMigrationConfig()

	// Assert
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "missing required environment variables")
	assert.Contains(t, err.Error(), "DB_USER")
}

// TestNewMigrationConfig_EmptyRequiredRejected verifies that a required DB
// variable set to an empty string is rejected (the `,notEmpty` tag), rather
// than producing a malformed DSN that fails obscurely at connect time.
func TestNewMigrationConfig_EmptyRequiredRejected(t *testing.T) {
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "") // set, but empty
	t.Setenv("DB_PASSWORD", "testpassword")
	t.Setenv("DB_NAME", "testdb")

	cfg, err := NewMigrationConfig()

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "DB_USER")
	assert.Contains(t, err.Error(), "should not be empty")
}
