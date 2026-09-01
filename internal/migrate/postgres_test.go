package migrate

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests exercise the compatibility floor against a real PostgreSQL. As with
// the other Postgres-backed tests in this repository they are skipped unless
// POSTGRES_DSN is set. Each test runs in a database of its own, so the repository's
// migrations are applied from scratch rather than onto a shared schema.
func newTestDatabase(t *testing.T) string {
	t.Helper()

	adminDSN := os.Getenv("POSTGRES_DSN")
	if adminDSN == "" {
		t.Skip("POSTGRES_DSN environment variable not set. Skipping integration test.")
	}

	parsed, err := url.Parse(adminDSN)
	require.NoError(t, err)

	name := fmt.Sprintf("migrate_test_%s", strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_")))
	if len(name) > 63 {
		name = name[:63]
	}

	admin, err := sql.Open("pgx", adminDSN)
	require.NoError(t, err)
	defer func() { _ = admin.Close() }()

	// Identifiers cannot be bound as parameters, and the name is derived from the
	// test's own name rather than from any external input.
	_, err = admin.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %q", name))
	require.NoError(t, err)
	_, err = admin.Exec(fmt.Sprintf("CREATE DATABASE %q", name))
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanup, err := sql.Open("pgx", adminDSN)
		if err != nil {
			return
		}
		defer func() { _ = cleanup.Close() }()
		_, _ = cleanup.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %q WITH (FORCE)", name))
	})

	parsed.Path = "/" + name
	return parsed.String()
}

// migrateDSN converts a postgres:// URL into the pgx5:// form the migration config
// builds, so the tests exercise the scheme production actually passes.
func migrateDSN(dsn string) string {
	return strings.Replace(dsn, "postgres://", "pgx5://", 1)
}

func applyRepositoryMigrations(t *testing.T, dsn string) *migrate.Migrate {
	t.Helper()

	m, err := migrate.New("file://../../db/migrations", migrateDSN(dsn))
	require.NoError(t, err)
	require.NoError(t, m.Up())
	return m
}

func TestSchemaCompatibilityMigration(t *testing.T) {
	dsn := newTestDatabase(t)
	m := applyRepositoryMigrations(t, dsn)
	defer func() { _, _ = m.Close() }()

	floor, err := postgresCompat{dsn: migrateDSN(dsn)}.MinBundledVersion()

	require.NoError(t, err)
	assert.Equal(t, uint(0), floor, "a schema with no destructive migration records no floor")
}

func TestMinBundledVersion_ReadsRecordedFloor(t *testing.T) {
	dsn := newTestDatabase(t)
	m := applyRepositoryMigrations(t, dsn)
	defer func() { _, _ = m.Close() }()

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	_, err = db.Exec("UPDATE schema_compatibility SET min_bundled_version = 11 WHERE id = 1")
	require.NoError(t, err)

	floor, err := postgresCompat{dsn: migrateDSN(dsn)}.MinBundledVersion()

	require.NoError(t, err)
	assert.Equal(t, uint(11), floor)
}

func TestMinBundledVersion_TableAbsent(t *testing.T) {
	dsn := newTestDatabase(t)

	floor, err := postgresCompat{dsn: migrateDSN(dsn)}.MinBundledVersion()

	require.NoError(t, err, "a database predating the table must not be an error")
	assert.Equal(t, uint(0), floor)
}

// A table with no row must not read as "no floor recorded": that would collapse
// the floor to the grandfathered minimum and admit a build the schema rejects.
func TestMinBundledVersion_TablePresentButEmpty(t *testing.T) {
	dsn := newTestDatabase(t)
	m := applyRepositoryMigrations(t, dsn)
	defer func() { _, _ = m.Close() }()

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	_, err = db.Exec("DELETE FROM schema_compatibility")
	require.NoError(t, err)

	floor, err := postgresCompat{dsn: migrateDSN(dsn)}.MinBundledVersion()

	require.Error(t, err)
	assert.ErrorContains(t, err, "holds no row")
	assert.Equal(t, uint(0), floor)
}

// The forward path against a real database, which the mocked tests and the
// in-memory sqlite construction cannot cover: a pgx5 driver or DSN-scheme
// regression would surface here, at the point migrations are actually applied.
func TestMigratorRun_AppliesMigrationsToAFreshDatabase(t *testing.T) {
	dsn := newTestDatabase(t)

	src, err := source.Open("file://../../db/migrations")
	require.NoError(t, err)
	head, err := highestVersion(src)
	require.NoError(t, err)
	require.NoError(t, src.Close())

	cfg := &MigrationConfig{DSN: migrateDSN(dsn), MigrationsPath: "../../db/migrations"}
	migrator, err := NewMigrator(cfg)
	require.NoError(t, err)

	require.NoError(t, migrator.Run())

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var version uint
	var dirty bool
	require.NoError(t, db.QueryRow("SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
	assert.Equal(t, head, version, "the schema must end at the repository head")
	assert.False(t, dirty)
}

func TestMigratorRun_RefusesADirtyDatabase(t *testing.T) {
	dsn := newTestDatabase(t)
	m := applyRepositoryMigrations(t, dsn)
	defer func() { _, _ = m.Close() }()

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	_, err = db.Exec("UPDATE schema_migrations SET dirty = true")
	require.NoError(t, err)

	err = NewMigratorWithDriver(m, postgresCompat{dsn: migrateDSN(dsn)}, 99).Run()

	require.Error(t, err)
	assert.ErrorContains(t, err, "marked dirty")
}

func TestSchemaCompatibilitySingleRow(t *testing.T) {
	dsn := newTestDatabase(t)
	m := applyRepositoryMigrations(t, dsn)
	defer func() { _, _ = m.Close() }()

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.Exec("INSERT INTO schema_compatibility (id, min_bundled_version) VALUES (2, 5)")

	require.Error(t, err, "the CHECK constraint must keep the table to a single row")
	assert.ErrorContains(t, err, "schema_compatibility_id_check")
}

// An older build meeting the recorded floor starts against the newer schema; one
// below it refuses. Both run against a database migrated to the repository's head.
func TestMigratorRun_AgainstRealSchema(t *testing.T) {
	tests := []struct {
		name        string
		floor       int
		bundled     uint
		errContains string
	}{
		{name: "no floor recorded", floor: 0, bundled: 4},
		{name: "build meets the floor", floor: 4, bundled: 4},
		{name: "build below the floor", floor: 9, bundled: 4, errContains: "up to at least 9"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn := newTestDatabase(t)
			m := applyRepositoryMigrations(t, dsn)
			defer func() { _, _ = m.Close() }()

			db, err := sql.Open("pgx", dsn)
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			_, err = db.Exec("UPDATE schema_compatibility SET min_bundled_version = $1 WHERE id = 1", tt.floor)
			require.NoError(t, err)

			migrator := NewMigratorWithDriver(m, postgresCompat{dsn: migrateDSN(dsn)}, tt.bundled)

			err = migrator.Run()

			if tt.errContains == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.errContains)
		})
	}
}
