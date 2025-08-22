package main

import (
	"flag"
	"log"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/migrate"
	"github.com/shini4i/argo-watcher/internal/server"
)

func main() {
	migrateFlag := flag.Bool("migrate", false, "Run database migrations and exit.")
	flag.Parse()

	if *migrateFlag {
		cfg, err := migrate.NewMigrationConfig()
		if err != nil {
			log.Fatalf("failed to load migration config: %v", err)
		}

		migrator, err := migrate.NewMigrator(cfg)
		if err != nil {
			log.Fatalf("failed to create migrator: %v", err)
		}

		if err := migrator.Run(); err != nil {
			log.Fatalf("Migration failed: %v", err)
		}

		return
	}

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
