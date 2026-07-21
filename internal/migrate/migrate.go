// Package migrate contains the logic for running database migrations.
package migrate

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	// Registers the "postgres" database driver with golang-migrate.
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	// Registers the "file" migration source driver with golang-migrate.
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// migrator is an interface that wraps the Up method for testing.
type migrator interface {
	Up() error
}

// Migrator is a struct that manages the database migration process.
type Migrator struct {
	migrator migrator
}

// NewMigrator initializes a new Migrator with a real migrate instance.
func NewMigrator(cfg *MigrationConfig) (*Migrator, error) {
	// migrate.New pings the database eagerly, so signal the attempt first; the
	// DSN's connect_timeout bounds a stalled connection.
	slog.Info("Connecting to database for migrations...")
	m, err := migrate.New(fmt.Sprintf("file://%s", cfg.MigrationsPath), cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("migration initialization failed: %w", err)
	}
	return NewMigratorWithDriver(m), nil
}

// NewMigratorWithDriver initializes a new Migrator with a provided driver for testing.
func NewMigratorWithDriver(driver migrator) *Migrator {
	return &Migrator{
		migrator: driver,
	}
}

// Run applies all available 'up' migrations and returns an error on failure.
func (m *Migrator) Run() error {
	slog.Info("Applying database migrations...")
	err := m.migrator.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("an error occurred while applying migrations: %w", err)
	}
	slog.Info("Migrations applied successfully.")
	return nil
}
