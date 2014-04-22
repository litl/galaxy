package main

import (
	"flag"
)

var (
	// Location of the default config.
	// This will not be overwritten by shuttle.
	defaultConfig string

	// Location of the live config which is updated on every state change.
	// The default config is loaded if this file does not exist.
	stateConfig string

	// Listen address for the http server.
	listenAddr string
)

func init() {
	flag.StringVar(&listenAddr, "http", "127.0.0.1:9090", "http server address")
	flag.StringVar(&defaultConfig, "config", "", "default config file")
	flag.StringVar(&stateConfig, "state", "", "updated config which reflects the internal state")

	flag.Parse()
}

func main() {
	loadConfig()
	startHTTPServer()
}
