package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/litl/galaxy/config"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/utils"
)

var (
	statsPrefix     string
	env             string
	pool            string
	registryURL     string
	debug           bool
	version         bool
	buildVersion    string
	stats           *TSCollection
	ironmqFlag      sliceVar
	influxDbAddr    string
	statsdAddr      string
	httpClient      *http.Client
	wg              sync.WaitGroup
	serviceRegistry *registry.ServiceRegistry
	Store           *config.Store
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
	flag.StringVar(&statsPrefix, "statsPrefix", "", "Global prefix for all stats")
	flag.StringVar(&env, "env", utils.GetEnv("GALAXY_ENV", ""), "Environment namespace")
	flag.StringVar(&pool, "pool", utils.GetEnv("GALAXY_POOL", ""), "Pool namespace")
	flag.StringVar(&registryURL, "registry", utils.GetEnv("GALAXY_REGISTRY_URL", ""), "registry URL")
	flag.BoolVar(&debug, "debug", false, "Enables debug build")
	flag.BoolVar(&version, "v", false, "display version info")

	flag.Var(&ironmqFlag, "ironmq", "env:project_id:token")
	flag.StringVar(&influxDbAddr, "influxdb-addr", "influxdb://admin:admin@localhost:8086/default", "Graphite host:port")
	flag.StringVar(&statsdAddr, "statsdAddr", utils.GetEnv("GALAXY_STATSD_HOST", ":8125"), "defaults to :8125.")

	flag.Parse()

	if version {
		fmt.Println(buildVersion)
		return
	}

	if strings.TrimSpace(env) == "" {
		fmt.Println("Need an env")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if debug {
		log.DefaultLogger.Level = log.DEBUG
	}

	serviceRegistry = registry.NewServiceRegistry(
		registry.DefaultTTL,
	)

	serviceRegistry.Connect(registryURL)

	Store = config.NewStore(
		registry.DefaultTTL,
	)

	Store.Connect(registryURL)

	stats = NewTSCollection()
	tscChan := make(chan *TSCollection, 100)
	wg.Add(4)

	go storeInfluxDB(tscChan)

	go loadCloudwatchStats(tscChan)
	go loadIronMQStats(tscChan)

	go StatsdListener(tscChan)
	wg.Wait()

}
