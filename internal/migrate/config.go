// Package migrate handles database migrations. This file defines the
// configuration loading specific to the migration process.
package migrate

import (
	"fmt"
	"net/url"

	envConfig "github.com/caarlos0/env/v11"

	"github.com/shini4i/argo-watcher/internal/helpers"
)

// dbConfig holds the database connection components required to build a migration-compatible DSN.
type dbConfig struct {
	User           string `env:"DB_USER,required,notEmpty"`
	Password       string `env:"DB_PASSWORD,required,notEmpty"`
	Host           string `env:"DB_HOST,required,notEmpty"`
	Port           string `env:"DB_PORT,required,notEmpty"`
	Name           string `env:"DB_NAME,required,notEmpty"`
	SSLMode        string `env:"DB_SSL_MODE" envDefault:"disable"`
	MigrationsPath string `env:"DB_MIGRATIONS_PATH" envDefault:"/app/db/migrations"`
}

// MigrationConfig holds the configuration required for running migrations.
type MigrationConfig struct {
	DSN            string
	MigrationsPath string
}

// NewMigrationConfig creates a new configuration by parsing environment variables
// and constructing a URI-based DSN suitable for golang-migrate.
func NewMigrationConfig() (*MigrationConfig, error) {
	dbCfg, err := envConfig.ParseAs[dbConfig]()
	if err != nil {
		return nil, helpers.PrettifyEnvError(err, "invalid argo-watcher migration configuration:")
	}

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		url.QueryEscape(dbCfg.User),
		url.QueryEscape(dbCfg.Password),
		dbCfg.Host,
		dbCfg.Port,
		dbCfg.Name,
		dbCfg.SSLMode,
	)

	return &MigrationConfig{DSN: dsn, MigrationsPath: dbCfg.MigrationsPath}, nil
}
