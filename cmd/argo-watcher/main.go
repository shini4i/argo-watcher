package main

import (
	"log"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/server"
)

func main() {
	cfg, err := config.NewServerConfig()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// In production, we provide the config and the global prometheus registry.
	s, err := server.NewServer(cfg, prometheus.DefaultRegisterer)
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}
	s.Run()
}
