package main

import (
	"flag"
	"sync"

	"github.com/litl/galaxy/log"
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

	// Listen address for the http server.
	adminListenAddr string

	// Debug logging
	debug bool

	// Redirect to SSL endpoint
	sslOnly bool

	// version flags
	version      bool
	buildVersion string
	wg           sync.WaitGroup
)

func init() {
	flag.StringVar(&listenAddr, "http", "127.0.0.1:8080", "http server address")
	flag.StringVar(&adminListenAddr, "admin", "127.0.0.1:9090", "admin http server address")
	flag.StringVar(&defaultConfig, "config", "", "default config file")
	flag.StringVar(&stateConfig, "state", "", "updated config which reflects the internal state")
	flag.BoolVar(&debug, "debug", false, "verbose logging")
	flag.BoolVar(&version, "v", false, "display version")
	flag.BoolVar(&sslOnly, "sslOnly", false, "require SSL")

	flag.Parse()
}

func main() {
	if debug {
		log.DefaultLogger.Level = log.DEBUG
	}

	if version {
		println(buildVersion)
		return
	}

	log.Printf("Starting shuttle %s", buildVersion)
	loadConfig()
	wg.Add(2)
	go startAdminHTTPServer()
	go startHTTPServer()
	wg.Wait()
}
