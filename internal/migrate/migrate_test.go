package migrate

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/mocks"
)

func TestMigrator_Run(t *testing.T) {
	upErr := errors.New("a serious migration error")
	versionErr := errors.New("connection refused")

	tests := []struct {
		name           string
		bundledVersion uint
		setup          func(m *mocks.Mockmigrator, c *mocks.MockcompatChecker)
		errContains    string
	}{
		{
			name:           "fresh database applies every migration",
			bundledVersion: 10,
			setup: func(m *mocks.Mockmigrator, _ *mocks.MockcompatChecker) {
				m.EXPECT().Version().Return(uint(0), false, migrate.ErrNilVersion)
				m.EXPECT().Up().Return(nil)
			},
		},
		{
			name:           "database behind the bundle is migrated up",
			bundledVersion: 10,
			setup: func(m *mocks.Mockmigrator, _ *mocks.MockcompatChecker) {
				m.EXPECT().Version().Return(uint(5), false, nil)
				m.EXPECT().Up().Return(nil)
			},
		},
		{
			name:           "database level with the bundle is a no-op",
			bundledVersion: 10,
			setup: func(m *mocks.Mockmigrator, _ *mocks.MockcompatChecker) {
				m.EXPECT().Version().Return(uint(10), false, nil)
				m.EXPECT().Up().Return(migrate.ErrNoChange)
			},
		},
		{
			name:           "an unreadable version is fatal",
			bundledVersion: 10,
			setup: func(m *mocks.Mockmigrator, _ *mocks.MockcompatChecker) {
				m.EXPECT().Version().Return(uint(0), false, versionErr)
			},
			errContains: "failed to read the current schema version",
		},
		{
			name:           "a dirty database is never touched",
			bundledVersion: 10,
			setup: func(m *mocks.Mockmigrator, _ *mocks.MockcompatChecker) {
				m.EXPECT().Version().Return(uint(7), true, nil)
			},
			errContains: "marked dirty",
		},
		{
			name:           "a failing Up is reported",
			bundledVersion: 10,
			setup: func(m *mocks.Mockmigrator, _ *mocks.MockcompatChecker) {
				m.EXPECT().Version().Return(uint(5), false, nil)
				m.EXPECT().Up().Return(upErr)
			},
			errContains: upErr.Error(),
		},
		{
			name:           "a newer database with no recorded floor is left alone",
			bundledVersion: 9,
			setup: func(m *mocks.Mockmigrator, c *mocks.MockcompatChecker) {
				m.EXPECT().Version().Return(uint(12), false, nil)
				c.EXPECT().MinBundledVersion().Return(uint(0), nil)
			},
		},
		{
			name:           "a build exactly at the floor is left alone",
			bundledVersion: 9,
			setup: func(m *mocks.Mockmigrator, c *mocks.MockcompatChecker) {
				m.EXPECT().Version().Return(uint(12), false, nil)
				c.EXPECT().MinBundledVersion().Return(uint(9), nil)
			},
		},
		{
			name:           "a database one migration ahead is skipped, not migrated",
			bundledVersion: 10,
			setup: func(m *mocks.Mockmigrator, c *mocks.MockcompatChecker) {
				m.EXPECT().Version().Return(uint(11), false, nil)
				c.EXPECT().MinBundledVersion().Return(uint(0), nil)
			},
		},
		{
			name:           "a build one migration below the floor refuses to start",
			bundledVersion: 10,
			setup: func(m *mocks.Mockmigrator, c *mocks.MockcompatChecker) {
				m.EXPECT().Version().Return(uint(12), false, nil)
				c.EXPECT().MinBundledVersion().Return(uint(11), nil)
			},
			errContains: "up to at least 11",
		},
		{
			name:           "a build below the recorded floor refuses to start",
			bundledVersion: 9,
			setup: func(m *mocks.Mockmigrator, c *mocks.MockcompatChecker) {
				m.EXPECT().Version().Return(uint(12), false, nil)
				c.EXPECT().MinBundledVersion().Return(uint(11), nil)
			},
			errContains: "needs a build bundling migrations up to at least 11",
		},
		{
			name:           "a build below the grandfathered floor refuses to start",
			bundledVersion: 2,
			setup: func(m *mocks.Mockmigrator, c *mocks.MockcompatChecker) {
				m.EXPECT().Version().Return(uint(9), false, nil)
				c.EXPECT().MinBundledVersion().Return(uint(0), nil)
			},
			errContains: "up to at least 3",
		},
		{
			name:           "an unreadable floor is fatal",
			bundledVersion: 9,
			setup: func(m *mocks.Mockmigrator, c *mocks.MockcompatChecker) {
				m.EXPECT().Version().Return(uint(12), false, nil)
				c.EXPECT().MinBundledVersion().Return(uint(0), versionErr)
			},
			errContains: versionErr.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			driver := mocks.NewMockmigrator(ctrl)
			compat := mocks.NewMockcompatChecker(ctrl)
			tt.setup(driver, compat)

			err := NewMigratorWithDriver(driver, compat, tt.bundledVersion).Run()

			if tt.errContains == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.errContains)
		})
	}
}

// A floor that cannot be read must not read as "no floor recorded": that would let
// a transient database error wave through a build the schema does not tolerate.
func TestMinBundledVersion_UnreachableDatabaseIsAnError(t *testing.T) {
	compat := postgresCompat{dsn: "pgx5://u:p@127.0.0.1:1/none?sslmode=disable&connect_timeout=1"}

	floor, err := compat.MinBundledVersion()

	require.Error(t, err)
	assert.Equal(t, uint(0), floor)
	assert.NotContains(t, err.Error(), "p@", "the DSN password must not reach the error text")
}

func TestNewMigrator_Success(t *testing.T) {
	migrationsDir := t.TempDir()
	writeMigration(t, migrationsDir, "1_init.up.sql", "CREATE TABLE users (id int);")
	writeMigration(t, migrationsDir, "7_later.up.sql", "CREATE TABLE more (id int);")

	cfg := &MigrationConfig{
		DSN:            "sqlite3://file::memory:?cache=shared",
		MigrationsPath: migrationsDir,
	}

	migrator, err := NewMigrator(cfg)

	require.NoError(t, err)
	require.NotNil(t, migrator)
	assert.Equal(t, uint(7), migrator.bundledVersion, "the newest migration file sets the bundled version")
	assert.NotNil(t, migrator.compat, "the compatibility floor must have a reader wired in")
}

// The guard turns on the bundled version, which in production comes from the
// db/migrations directory copied into the image. A source driver that stopped
// walking, or a filename convention change, would shift it silently.
func TestHighestVersion_RepositoryMigrations(t *testing.T) {
	const dir = "../../db/migrations"

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var want uint64
	for _, entry := range entries {
		prefix, _, found := strings.Cut(entry.Name(), "_")
		if !found {
			continue
		}
		version, err := strconv.ParseUint(prefix, 10, 64)
		require.NoError(t, err, "migration %q must start with a numeric version", entry.Name())
		want = max(want, version)
	}
	require.NotZero(t, want, "the repository must hold at least one migration")

	src, err := source.Open("file://" + dir)
	require.NoError(t, err)
	defer func() { _ = src.Close() }()

	got, err := highestVersion(src)

	require.NoError(t, err)
	assert.Equal(t, uint(want), got)
}

func TestNewMigrator_NoMigrations(t *testing.T) {
	cfg := &MigrationConfig{
		DSN:            "sqlite3://file::memory:?cache=shared",
		MigrationsPath: t.TempDir(),
	}

	migrator, err := NewMigrator(cfg)

	require.Error(t, err)
	assert.ErrorContains(t, err, "no migrations found")
	assert.Nil(t, migrator)
}

func TestNewMigrator_Failure(t *testing.T) {
	migrationsDir := t.TempDir()
	writeMigration(t, migrationsDir, "1_init.up.sql", "CREATE TABLE users (id int);")

	cfg := &MigrationConfig{
		DSN:            "this-is-not-a-valid-uri",
		MigrationsPath: migrationsDir,
	}

	migrator, err := NewMigrator(cfg)

	require.Error(t, err)
	assert.Nil(t, migrator)
}

func writeMigration(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0600))
}
