package migrate

import (
	"errors"
	"fmt"
	"log"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// migrator is an interface that wraps the methods of the migrate library
// that we use. This allows us to mock the migration process for testing.
type migrator interface {
	Up() error
}

// Migrator is a struct that manages the database migration process.
type Migrator struct {
	migrator migrator
}

// NewMigrator initializes a new Migrator with a given configuration.
// It creates a real migrate instance and is used by the application's main entry point.
//
// Parameters:
//
//	cfg: The migration configuration containing the database DSN and path.
//
// Returns:
//
//	A pointer to a Migrator or an error if initialization fails.
func NewMigrator(cfg *MigrationConfig) (*Migrator, error) {
	m, err := migrate.New(fmt.Sprintf("file://%s", cfg.MigrationsPath), cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("migration initialization failed: %w", err)
	}
	return NewMigratorWithDriver(m), nil
}

// NewMigratorWithDriver initializes a new Migrator with a provided driver instance.
// This constructor is used for testing to inject a mock migrator.
//
// Parameters:
//
//	driver: An instance that satisfies the migrator interface.
//
// Returns:
//
//	A pointer to a Migrator.
func NewMigratorWithDriver(driver migrator) *Migrator {
	return &Migrator{
		migrator: driver,
	}
}

// Run applies all available 'up' migrations.
// It logs the outcome and will panic on failure, as a failed migration
// should halt the deployment process.
func (m *Migrator) Run() {
	log.Println("Applying database migrations...")
	if err := m.migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		log.Fatalf("Fatal: An error occurred while applying migrations: %v", err)
	}
	log.Println("Migrations applied successfully.")
}
