package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/shini4i/argo-watcher/pkg/client"
	"os"
)

var invalidModeError = errors.New("invalid mode")

func runWatcher(serverFlag, clientFlag bool) error {
	if serverFlag && clientFlag {
		return invalidModeError
	} else if serverFlag {
		serverWatcher()
	} else if clientFlag {
		client.ClientWatcher()
	} else {
		return invalidModeError
	}
	return nil
}

func usage() {
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

	flag.Usage = usage

	flag.Parse()

	if err := runWatcher(*serverFlag, *clientFlag); err != nil {
		flag.Usage()
		os.Exit(1)
	}
}
