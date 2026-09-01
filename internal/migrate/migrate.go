// Package migrate contains the logic for running database migrations.
package migrate

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	// Registers the "pgx5" database driver with golang-migrate. It is preferred over
	// the "postgres" driver because it reuses the pgx stack the state layer already
	// links, instead of pulling a second PostgreSQL driver (lib/pq) into the binary.
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source"
	// Registers the "file" source driver, which backs the file:// URL both
	// source.Open and highestVersion read the bundled migrations through.
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// minCompatibleBundledVersion is the floor for databases predating the
// schema_compatibility table. 000003 rewrote column types and added NOT NULL
// constraints, so an older build cannot read the tasks table; 000004 onwards only
// add objects, or drop an index, which costs performance rather than correctness.
const minCompatibleBundledVersion = 3

type migrator interface {
	Up() error
	Version() (version uint, dirty bool, err error)
}

// compatChecker reports the oldest bundled migration version the live schema accepts.
type compatChecker interface {
	MinBundledVersion() (uint, error)
}

// Migrator is a struct that manages the database migration process.
type Migrator struct {
	migrator       migrator
	compat         compatChecker
	bundledVersion uint
}

// NewMigrator initializes a new Migrator with a real migrate instance. It fails when
// the migrations path holds no migrations, which means the build shipped without
// them rather than that there is nothing to apply.
func NewMigrator(cfg *MigrationConfig) (*Migrator, error) {
	src, err := source.Open(fmt.Sprintf("file://%s", cfg.MigrationsPath))
	if err != nil {
		return nil, fmt.Errorf("failed to open migrations path %q: %w", cfg.MigrationsPath, err)
	}

	bundledVersion, err := highestVersion(src)
	if err != nil {
		return nil, errors.Join(err, src.Close())
	}

	// NewWithSourceInstance pings the database eagerly, so signal the attempt first;
	// the DSN's connect_timeout bounds a stalled connection.
	slog.Info("Connecting to database for migrations...")
	m, err := migrate.NewWithSourceInstance("file", src, cfg.DSN)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("migration initialization failed: %w", err), src.Close())
	}

	return NewMigratorWithDriver(m, postgresCompat{dsn: cfg.DSN}, bundledVersion), nil
}

// NewMigratorWithDriver wraps an already-constructed migrate driver. compat reports
// the live schema's compatibility floor, and bundledVersion is the newest migration
// the driver's source offers.
func NewMigratorWithDriver(driver migrator, compat compatChecker, bundledVersion uint) *Migrator {
	return &Migrator{
		migrator:       driver,
		compat:         compat,
		bundledVersion: bundledVersion,
	}
}

// Run applies all available 'up' migrations and returns an error on failure.
// Migrations are forward-only: a database already newer than the bundled migrations
// is left untouched, so a rolled-back release starts against the schema its
// successor advanced instead of failing on a migration it does not ship.
func (m *Migrator) Run() error {
	current, dirty, err := m.migrator.Version()
	switch {
	case errors.Is(err, migrate.ErrNilVersion):
		// Fresh database, so Up starts from the first migration.
	case err != nil:
		return fmt.Errorf("failed to read the current schema version: %w", err)
	case dirty:
		return fmt.Errorf("database is at version %d and marked dirty: a previous migration "+
			"failed part-way and must be resolved by hand", current)
	case current > m.bundledVersion:
		return m.skipAhead(current)
	}

	slog.Info("Applying database migrations...")
	err = m.migrator.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("an error occurred while applying migrations: %w", err)
	}
	slog.Info("Migrations applied successfully.")
	return nil
}

// skipAhead decides whether this build may run against a schema newer than the
// migrations it bundles. It may, unless a destructive migration recorded a
// compatibility floor above this build's newest migration.
func (m *Migrator) skipAhead(current uint) error {
	floor, err := m.compat.MinBundledVersion()
	if err != nil {
		return err
	}
	floor = max(floor, minCompatibleBundledVersion)

	if m.bundledVersion < floor {
		return fmt.Errorf("database is at schema version %d and needs a build bundling migrations "+
			"up to at least %d, but this build bundles only %d: roll forward to a newer release",
			current, floor, m.bundledVersion)
	}

	slog.Warn("Database schema is newer than this build; leaving it untouched. Expected after a rollback.",
		"database_version", current,
		"bundled_version", m.bundledVersion,
		"min_bundled_version", floor,
	)
	return nil
}

// highestVersion reports the newest migration version the source offers. It returns
// an error when the source holds no migrations at all.
func highestVersion(src source.Driver) (uint, error) {
	version, err := src.First()
	if err != nil {
		return 0, fmt.Errorf("no migrations found in the migrations path: %w", err)
	}

	for {
		next, err := src.Next(version)
		if errors.Is(err, fs.ErrNotExist) {
			return version, nil
		}
		if err != nil {
			return 0, fmt.Errorf("failed to walk the bundled migrations: %w", err)
		}
		version = next
	}
}

// postgresCompat reads the compatibility floor from the live database. The migrate
// path otherwise reaches Postgres only through golang-migrate, which exposes no way
// to run a query, so this opens its own short-lived connection.
type postgresCompat struct {
	dsn string
}

// MinBundledVersion returns the floor recorded by the newest destructive migration.
// It is 0 when none has recorded one, and on a database predating the
// schema_compatibility table, leaving minCompatibleBundledVersion to apply.
func (p postgresCompat) MinBundledVersion() (uint, error) {
	// pgx parses a connection string as a URL only when it starts with postgres:// or
	// postgresql://. The DSN carries golang-migrate's pgx5 scheme, which that driver
	// rewrites the same way before connecting.
	dsn := strings.Replace(p.dsn, "pgx5://", "postgres://", 1)

	db, err := gorm.Open(postgres.Open(dsn))
	if err != nil {
		return 0, fmt.Errorf("failed to connect while reading the compatibility floor: %w", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()

	// to_regclass resolves through search_path and reports absence as NULL, so a
	// failed probe stays distinguishable from a table that is not there. gorm's
	// HasTable discards its query error, which would read as "no floor recorded".
	var exists bool
	err = db.Raw("SELECT to_regclass('schema_compatibility') IS NOT NULL").Scan(&exists).Error
	if err != nil {
		return 0, fmt.Errorf("failed to look for the compatibility table: %w", err)
	}
	if !exists {
		return 0, nil
	}

	var version uint
	read := db.Raw("SELECT min_bundled_version FROM schema_compatibility WHERE id = 1").Scan(&version)
	if read.Error != nil {
		return 0, fmt.Errorf("failed to read the compatibility floor: %w", read.Error)
	}
	// gorm reports a missing row as a zero-row Scan rather than an error, which
	// would read as "no floor recorded" and let an incompatible build through.
	if read.RowsAffected == 0 {
		return 0, errors.New("schema_compatibility holds no row: the compatibility floor cannot be established")
	}
	return version, nil
}
