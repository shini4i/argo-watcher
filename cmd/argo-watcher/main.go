package main

import (
	"log"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/server"
)

func main() {
	cfg, err := config.NewServerConfig()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	s, err := server.NewServer(cfg)
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}
	s.Run()
}
