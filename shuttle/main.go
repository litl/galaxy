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

	// Listen addressed for the http servers.
	httpAddr  string
	httpsAddr string

	// Listen address for the http server.
	adminListenAddr string

	// Debug logging
	debug bool

	// Redirect to SSL endpoint
	sslOnly bool

	// version flags
	version      bool
	buildVersion string

	// SSL Certificate directory
	certDir string
)

func init() {
	flag.StringVar(&httpAddr, "http", "", "http server address")
	flag.StringVar(&httpsAddr, "https", "", "https server address")
	flag.StringVar(&adminListenAddr, "admin", "127.0.0.1:9090", "admin http server address")
	flag.StringVar(&defaultConfig, "config", "", "default config file")
	flag.StringVar(&stateConfig, "state", "", "updated config which reflects the internal state")
	flag.StringVar(&certDir, "certs", "./", "directory containing SSL Certficates and Keys")
	flag.BoolVar(&debug, "debug", false, "verbose logging")
	flag.BoolVar(&version, "v", false, "display version")

	// FIXME: we may only want this for one HTTPRouter
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

	var wg sync.WaitGroup
	wg.Add(1)
	go startAdminHTTPServer(&wg)

	if httpAddr != "" {
		wg.Add(1)
		go startHTTPServer(&wg)
	}

	if httpsAddr != "" {
		wg.Add(1)
		go startHTTPSServer(&wg)
	}
	wg.Wait()
}
