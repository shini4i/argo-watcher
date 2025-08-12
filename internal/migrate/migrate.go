package migrate

import (
	"fmt"
	"log"

	"errors"

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
// It uses the pre-constructed DSN to create a new instance of the migrate tool.
//
// Parameters:
//
//	cfg: The migration configuration containing the database DSN.
//
// Returns:
//
//	A pointer to a Migrator or an error if initialization fails.
func NewMigrator(cfg *MigrationConfig) (*Migrator, error) {
	m, err := migrate.New(fmt.Sprintf("file://%s", cfg.MigrationsPath), cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("migration initialization failed: %w", err)
	}

	return &Migrator{
		migrator: m,
	}, nil
}

// Run applies all available 'up' migrations.
func (m *Migrator) Run() {
	log.Println("Applying database migrations...")
	if err := m.migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		log.Fatalf("Fatal: An error occurred while applying migrations: %v", err)
	}
	log.Println("Migrations applied successfully.")
}
