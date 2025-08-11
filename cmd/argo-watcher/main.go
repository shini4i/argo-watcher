package main

import (
	"flag"
	"log"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/migrate"
	"github.com/shini4i/argo-watcher/internal/server"
)

func main() {
	// Define and parse the --migrate flag.
	migrateFlag := flag.Bool("migrate", false, "Run database migrations and exit.")
	flag.Parse()

	// If the flag is present, run migrations and exit.
	if *migrateFlag {
		// Load migration-specific configuration.
		cfg, err := migrate.NewMigrationConfig()
		if err != nil {
			log.Fatalf("failed to load migration config: %v", err)
		}

		// Create and run the migrator.
		migrator, err := migrate.NewMigrator(cfg)
		if err != nil {
			log.Fatalf("failed to create migrator: %v", err)
		}
		migrator.Run()

		return
	}

	// Default behavior: start the application server.
	serverConfig, err := config.NewServerConfig()
	if err != nil {
		log.Fatalf("failed to load server config: %v", err)
	}

	s, err := server.NewServer(serverConfig, prometheus.DefaultRegisterer)
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}
	s.Run()
}
