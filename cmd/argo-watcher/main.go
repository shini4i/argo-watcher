package main

import (
	"flag"
	"github.com/shini4i/argo-watcher/pkg/client"
	"os"

	"github.com/rs/zerolog/log"
)

func main() {
	serverFlag := flag.Bool("server", false, "Run in server mode")
	clientFlag := flag.Bool("client", false, "Run in client mode")

	flag.Parse()

	if *serverFlag && *clientFlag {
		log.Error().Msg("Both server and client modes cannot be specified simultaneously")
		os.Exit(1)
	} else if *serverFlag {
		serverWatcher()
	} else if *clientFlag {
		client.ClientWatcher()
	} else {
		log.Error().Msg("Either server or client mode should be specified")
		os.Exit(1)
	}
}
