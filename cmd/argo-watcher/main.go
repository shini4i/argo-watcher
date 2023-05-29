package main

import (
	"flag"
	"os"

	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/internal/client"
)

var (
	mode string
)

func init() {
	server := flag.Bool("server", false, "Server mode")
	client := flag.Bool("client", false, "Client mode")

	flag.Parse()

	if *server == *client {
		log.Error().Msgf("server=%v client=%v. Whether server or client should be", *server, *client)
		os.Exit(1)
	}

	if *server {
		mode = "server"
	}

	if *client {
		mode = "client"
	}
}

func main() {

	switch mode {
	case "server":
		serverWatcher()
	case "client":
		client.ClientWatcher()
	}

}
