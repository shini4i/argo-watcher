package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/server"

	"github.com/shini4i/argo-watcher/pkg/client"
)

var errorInvalidMode = errors.New("invalid mode")

func runWatcher(serverFlag, clientFlag, migrationFlag, migrationDryRunFlag bool) error {
	// start server if requested
	if serverFlag && !clientFlag && !migrationFlag {
		server.RunServer()
		return nil
	}

	// start migrations
	if migrationFlag && !clientFlag && !serverFlag {
		server.RunMigrations(migrationDryRunFlag)
		return nil
	}

	// start client if requested
	if clientFlag && !serverFlag && !migrationFlag {
		client.Run()
		return nil
	}

	// return error. we must start client or server
	return errorInvalidMode
}

func printUsage() {
	if _, err := fmt.Fprintf(os.Stderr, "Usage: argo-watcher [options]\n"); err != nil {
		return
	}

	if _, err := fmt.Fprintf(os.Stderr, "Invalid mode specified. Please specify either -server, -client or -migration. \nMigration also supports -dry-run\n"); err != nil {
		return
	}

	flag.PrintDefaults()
}

func main() {
	serverFlag := flag.Bool("server", false, "Run in server mode.")
	clientFlag := flag.Bool("client", false, "Run in client mode.")
	migrationFlag := flag.Bool("migration", false, "Run in migration mode.")
	migrationDryRunFlag := flag.Bool("dry-run", false, "Run migration in dry-run mode. Requires -migration to have effect.")

	flag.Usage = printUsage
	flag.Parse()

	if err := runWatcher(*serverFlag, *clientFlag, *migrationFlag, *migrationDryRunFlag); err != nil {
		flag.Usage()
		os.Exit(1)
	}
}
