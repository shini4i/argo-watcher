package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/shini4i/argo-watcher/pkg/client"
)

var errorInvalidMode = errors.New("invalid mode")

func runWatcher(serverFlag, clientFlag bool) error {
	// start server if requested
	if serverFlag && !clientFlag {
		runServer()
		return nil
	}

	// start client if requested
	if clientFlag && !serverFlag {
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

	if _, err := fmt.Fprintf(os.Stderr, "Invalid mode specified. Please specify either -server or -client.\n"); err != nil {
		return
	}

	flag.PrintDefaults()
}

func main() {
	serverFlag := flag.Bool("server", false, "Run in server mode")
	clientFlag := flag.Bool("client", false, "Run in client mode")

	flag.Usage = printUsage
	flag.Parse()

	if err := runWatcher(*serverFlag, *clientFlag); err != nil {
		flag.Usage()
		os.Exit(1)
	}
}
