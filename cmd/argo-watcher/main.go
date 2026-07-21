package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/logging"
	"github.com/shini4i/argo-watcher/internal/migrate"
	"github.com/shini4i/argo-watcher/internal/server"
)

// @title Argo-Watcher API
// @version 1.0
// @description A small tool that will help to improve deployment visibility
// @BasePath /
func main() {
	// Configure structured logging up front so both the --migrate and server
	// paths emit consistent JSON, including early startup failures. NewServer
	// re-applies this once the full config is loaded.
	logging.Init(os.Getenv("LOG_LEVEL"))

	migrateFlag := flag.Bool("migrate", false, "Run database migrations and exit.")
	flag.Parse()

	if *migrateFlag {
		cfg, err := migrate.NewMigrationConfig()
		if err != nil {
			slog.Error("failed to load migration config", "error", err)
			os.Exit(1)
		}

		migrator, err := migrate.NewMigrator(cfg)
		if err != nil {
			slog.Error("failed to create migrator", "error", err)
			os.Exit(1)
		}

		if err := migrator.Run(); err != nil {
			slog.Error("migration failed", "error", err)
			os.Exit(1)
		}

		return
	}

	serverConfig, err := config.NewServerConfig()
	if err != nil {
		slog.Error("failed to load server config", "error", err)
		os.Exit(1)
	}

	s, err := server.NewServer(serverConfig, prometheus.DefaultRegisterer)
	if err != nil {
		slog.Error("failed to create server", "error", err)
		os.Exit(1)
	}
	s.Run()
}
