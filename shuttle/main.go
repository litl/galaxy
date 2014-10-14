package main

import (
	"flag"
	"sync"

	"github.com/litl/galaxy/log"
	gs "github.com/litl/galaxy/stats"
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

	// Listen address for the http server.
	influxDbAddr string

	// Prefix for stats send to influxdb
	statsPrefix string

	// Debug logging
	debug bool

	// Redirect to SSL endpoint
	sslOnly bool

	// version flags
	version      bool
	buildVersion string
	wg           sync.WaitGroup
	tscChan      = make(chan *gs.TSCollection, 1000)
)

func init() {
	flag.StringVar(&listenAddr, "http", "127.0.0.1:8080", "http server address")
	flag.StringVar(&adminListenAddr, "admin", "127.0.0.1:9090", "admin http server address")
	flag.StringVar(&defaultConfig, "config", "", "default config file")
	flag.StringVar(&stateConfig, "state", "", "updated config which reflects the internal state")
	flag.StringVar(&influxDbAddr, "influxdb", "", "influxdb addr [influxdb://user:pw@host:port/db]")
	flag.StringVar(&statsPrefix, "statsPrefix", "", "prefix for stats sent to influxdb")
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

	if influxDbAddr != "" {
		wg.Add(1)
		iw := gs.InfluxDBWriter{
			Addr:    influxDbAddr,
			Prefix:  statsPrefix,
			Wg:      &wg,
			TscChan: tscChan,
		}
		go iw.StoreInfluxDB()
		log.Printf("Sending stats to %s", influxDbAddr)
	}

	go startAdminHTTPServer()
	go startHTTPServer()
	wg.Wait()
}
