package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/litl/galaxy/log"
)

var (
	carbonAddr string
	debug      bool
	version    bool
	ironmqFlag sliceVar

	buildVersion = "version_missing"
)

type sliceVar []string

func (s *sliceVar) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func (s *sliceVar) String() string {
	return strings.Join(*s, ",")
}
func main() {
	flag.StringVar(&carbonAddr, "carbon", "", "host:port of graphite carbon cache line receiver")
	flag.BoolVar(&debug, "debug", false, "verbose logging")
	flag.BoolVar(&version, "v", false, "display version info")
	flag.Var(&ironmqFlag, "ironmq", "env:project_id:token")

	flag.Parse()

	if version {
		fmt.Println(buildVersion)
		return
	}

	if debug {
		log.DefaultLogger.Level = log.DEBUG
	}

	statChan := make(chan []Stat, 100)
	go loadCloudwatchStats(statChan)
	go loadIronMQStats(statChan)

	carbon, err := NewCarbon(carbonAddr)
	if err != nil {
		log.Fatalf("cannot connect to carbon cache: %s", err)
	}
	carbon.Collector(statChan)
}
