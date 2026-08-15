package migrate

import (
	"net/url"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMigrationConfig_Success(t *testing.T) {
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "testuser")
	t.Setenv("DB_PASSWORD", "testpassword!@#")
	t.Setenv("DB_NAME", "testdb")
	t.Setenv("DB_SSL_MODE", "require")
	// Unset the custom path to ensure the default is used.
	t.Setenv("DB_MIGRATIONS_PATH", "")

	cfg, err := NewMigrationConfig()

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "/app/db/migrations", cfg.MigrationsPath)
	assert.Equal(t, "pgx5://testuser:testpassword%21%40%23@localhost:5432/testdb?sslmode=require&connect_timeout=10", cfg.DSN)
}

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
	assert.Equal(t, "pgx5://testuser:testpassword@localhost:5432/testdb?sslmode=require&connect_timeout=3", cfg.DSN)
}

// TestNewMigrationConfig_SchemeIsRegisteredDriver verifies the DSN scheme names a
// driver this package actually registers. golang-migrate resolves the driver from
// the scheme at runtime, so a scheme that no imported driver claims fails only when
// migrations run against a live database — after the deployment is already rolling.
func TestNewMigrationConfig_SchemeIsRegisteredDriver(t *testing.T) {
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "testuser")
	t.Setenv("DB_PASSWORD", "testpassword")
	t.Setenv("DB_NAME", "testdb")

	cfg, err := NewMigrationConfig()
	require.NoError(t, err)

	parsed, err := url.Parse(cfg.DSN)
	require.NoError(t, err)
	assert.Contains(t, database.List(), parsed.Scheme)
}

func TestNewMigrationConfig_CustomPath(t *testing.T) {
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "testuser")
	t.Setenv("DB_PASSWORD", "testpassword")
	t.Setenv("DB_NAME", "testdb")
	t.Setenv("DB_MIGRATIONS_PATH", "/my/custom/path")

	cfg, err := NewMigrationConfig()

	require.NoError(t, err)
	assert.Equal(t, "/my/custom/path", cfg.MigrationsPath)
}

// TestNewMigrationConfig_ConnectTimeoutRejectsNonPositive verifies that a
// non-positive DB_CONNECT_TIMEOUT is rejected: 0 disables the dial timeout, which
// defeats the fail-fast guard, and a negative value is rejected by pgx's own DSN
// parsing. Both are turned into one early configuration error naming the variable.
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

func TestNewMigrationConfig_ValidationError(t *testing.T) {
	os.Clearenv() // Ensure no conflicting variables are set.

	cfg, err := NewMigrationConfig()

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
