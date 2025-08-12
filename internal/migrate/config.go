// internal/migrate/config.go
package migrate

import (
	"fmt"
	"net/url"

	envConfig "github.com/caarlos0/env/v11"
	"github.com/go-playground/validator/v10"
)

// dbConfig holds the database connection components required to build a migration-compatible DSN.
type dbConfig struct {
	User           string `env:"DB_USER" validate:"required"`
	Password       string `env:"DB_PASSWORD" validate:"required"`
	Host           string `env:"DB_HOST" validate:"required"`
	Port           string `env:"DB_PORT" validate:"required"`
	Name           string `env:"DB_NAME" validate:"required"`
	SslMode        string `env:"DB_SSL_MODE" envDefault:"disable"`
	MigrationsPath string `env:"MIGRATIONS_PATH" envDefault:"/db/migrations"`
}

// MigrationConfig holds the configuration required for running migrations.
type MigrationConfig struct {
	// DSN is the fully constructed, URI-formatted database source name for golang-migrate.
	DSN            string
	MigrationsPath string
}

// NewMigrationConfig creates a new configuration by parsing environment variables
// and constructing a URI-based DSN suitable for golang-migrate.
//
// Returns:
//
//	A pointer to the MigrationConfig struct or an error if parsing or validation fails.
func NewMigrationConfig() (*MigrationConfig, error) {
	var dbCfg dbConfig
	if err := envConfig.Parse(&dbCfg); err != nil {
		return nil, fmt.Errorf("could not parse database components: %w", err)
	}

	validate := validator.New()
	if err := validate.Struct(&dbCfg); err != nil {
		return nil, fmt.Errorf("database component validation failed: %w", err)
	}

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		url.QueryEscape(dbCfg.User),
		url.QueryEscape(dbCfg.Password),
		dbCfg.Host,
		dbCfg.Port,
		dbCfg.Name,
		dbCfg.SslMode,
	)

	return &MigrationConfig{DSN: dsn, MigrationsPath: dbCfg.MigrationsPath}, nil
}
