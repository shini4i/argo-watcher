package migrate

import (
	"net/url"
	"os"
	"strings"
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

// restoreEnvAfter puts the process environment back once the test finishes.
// os.Clearenv wipes it for the whole test binary, which would otherwise strip
// POSTGRES_DSN from every later test in this package and silently skip them.
func restoreEnvAfter(t *testing.T) {
	t.Helper()
	saved := os.Environ()
	t.Cleanup(func() {
		os.Clearenv()
		for _, entry := range saved {
			if key, value, found := strings.Cut(entry, "="); found {
				_ = os.Setenv(key, value)
			}
		}
	})
}

func TestNewMigrationConfig_ValidationError(t *testing.T) {
	restoreEnvAfter(t)
	os.Clearenv() // Ensure no conflicting variables are set.

	cfg, err := NewMigrationConfig()

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "missing required environment variables")
	assert.Contains(t, err.Error(), "DB_USER")
}

// TestNewMigrationConfig_EmptyRequiredRejected covers the `,notEmpty` tag on every
// connection setting, not just one. pgx drops an empty username outright and falls back
// to PGUSER or the OS user, so a blank DB_USER would connect as somebody else instead of
// failing — a malformed DSN that goes wrong silently rather than at connect time.
func TestNewMigrationConfig_EmptyRequiredRejected(t *testing.T) {
	required := []string{"DB_USER", "DB_PASSWORD", "DB_HOST", "DB_PORT", "DB_NAME"}

	for _, blanked := range required {
		t.Run(blanked, func(t *testing.T) {
			for _, name := range required {
				t.Setenv(name, "value")
			}
			t.Setenv(blanked, "") // set, but empty

			cfg, err := NewMigrationConfig()

			require.Error(t, err)
			assert.Nil(t, cfg)
			assert.Contains(t, err.Error(), blanked)
			assert.Contains(t, err.Error(), "should not be empty")
		})
	}
}

// TestNewMigrationConfig_CredentialsSurviveTheDSN pins that the credentials the
// database receives are the ones configured. The userinfo component is not a query
// string: a space encoded as "+" there stays a literal plus, so the password sent is
// not the password set and the migration fails authentication naming nothing.
func TestNewMigrationConfig_CredentialsSurviveTheDSN(t *testing.T) {
	const (
		user = "test user"
		// Go emits the sub-delims "&" and "=" literally in userinfo, so a credential
		// carrying them must not be able to open a query string of its own. "%" is the
		// escape introducer, the one character an asymmetric escaper corrupts.
		password = `p@ss word+x/y?z#w&k=v%s\`
	)

	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", user)
	t.Setenv("DB_PASSWORD", password)
	t.Setenv("DB_NAME", "testdb")

	cfg, err := NewMigrationConfig()
	require.NoError(t, err)

	parsed, err := url.Parse(cfg.DSN)
	require.NoError(t, err)

	gotPassword, set := parsed.User.Password()
	require.True(t, set)
	assert.Equal(t, user, parsed.User.Username())
	assert.Equal(t, password, gotPassword)
	// The host, database and connection options must all still parse out intact.
	assert.Equal(t, "localhost:5432", parsed.Host)
	assert.Equal(t, "/testdb", parsed.Path)
	assert.Equal(t, "disable", parsed.Query().Get("sslmode"))
	assert.Equal(t, "10", parsed.Query().Get("connect_timeout"))
}
