package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/runtime"
	"github.com/litl/galaxy/utils"
)

var (
	stopCutoff      int64
	app             string
	redisHost       string
	env             string
	pool            string
	loop            bool
	shuttleHost     string
	debug           bool
	loggedOnce      bool
	serviceConfigs  []*registry.ServiceConfig
	serviceRegistry *registry.ServiceRegistry
	serviceRuntime  *runtime.ServiceRuntime
)

func initOrDie() {

	serviceRegistry = registry.NewServiceRegistry(
		env,
		pool,
		"",
		600,
		"",
	)

	serviceRegistry.Connect(redisHost)
	serviceRuntime = runtime.NewServiceRuntime(shuttleHost, env, pool, redisHost)
}

func startContainersIfNecessary() error {
	serviceConfigs, err := serviceRegistry.ListApps("")
	if err != nil {
		log.Printf("ERROR: Could not retrieve service configs for /%s/%s: %s\n", env, pool, err)
		return err
	}

	if len(serviceConfigs) == 0 {
		log.Printf("No services configured for /%s/%s\n", env, pool)
		return err
	}

	for _, serviceConfig := range serviceConfigs {

		if app != "" && serviceConfig.Name != app {
			continue
		}

		if serviceConfig.Version() == "" {
			log.Printf("Skipping %s. No version configured.\n", serviceConfig.Name)
			continue
		}

		started, container, err := serviceRuntime.StartIfNotRunning(&serviceConfig)
		if err != nil {
			log.Printf("ERROR: Could not determine if %s is running: %s\n",
				serviceConfig.Version(), err)
			return err
		}

		if started {
			log.Printf("Started %s version %s as %s\n", serviceConfig.Name, serviceConfig.Version(), container.ID[0:12])
		}

		if !(debug || loggedOnce) {
			log.Printf("%s version %s running as %s\n", serviceConfig.Name, serviceConfig.Version(), container.ID[0:12])
		}

		log.Debugf("%s version %s running as %s\n", serviceConfig.Name, serviceConfig.Version(), container.ID[0:12])
	}

	loggedOnce = true
	return nil
}

func restartContainers(changedConfigs chan *registry.ConfigChange) {
	ticker := time.NewTicker(10 * time.Second)

	for {

		var changedConfig *registry.ConfigChange
		select {

		case changedConfig = <-changedConfigs:
			if changedConfig.Error != nil {
				log.Printf("ERROR: Error watching changes: %s\n", changedConfig.Error)
				continue
			}

			if changedConfig.ServiceConfig == nil {
				continue
			}

			if changedConfig.ServiceConfig.Version() == "" {
				continue
			}

			log.Printf("Restarting %s\n", changedConfig.ServiceConfig.Name)
			container, err := serviceRuntime.Start(changedConfig.ServiceConfig)
			if err != nil {
				log.Printf("ERROR: Could not start %s: %s\n",
					changedConfig.ServiceConfig.Version(), err)
				continue
			}
			log.Printf("Restarted %s as: %s\n", changedConfig.ServiceConfig.Version(), container.ID)

			err = serviceRuntime.StopAllButLatest(stopCutoff)
			if err != nil {
				log.Printf("ERROR: Could not stop containers: %s\n", err)
			}
		case <-ticker.C:
			err := startContainersIfNecessary()
			if err != nil {
				log.Printf("ERROR: Could not start containers: %s\n", err)
			}

			err = serviceRuntime.StopAllButLatest(stopCutoff)
			if err != nil {
				log.Printf("ERROR: Could not stop containers: %s\n", err)
			}
		}

	}
}

func main() {
	flag.Int64Var(&stopCutoff, "cutoff", 10, "Seconds to wait before stopping old containers")
	flag.StringVar(&app, "app", "", "App to start")
	flag.StringVar(&redisHost, "redis", utils.GetEnv("GALAXY_REDIS_HOST", "127.0.0.1:6379"), "redis host")
	flag.StringVar(&env, "env", utils.GetEnv("GALAXY_ENV", "dev"), "Environment namespace")
	flag.StringVar(&pool, "pool", utils.GetEnv("GALAXY_POOL", "web"), "Pool namespace")
	flag.BoolVar(&loop, "loop", false, "Run continously")
	flag.StringVar(&shuttleHost, "shuttleAddr", "", "IP where containers can reach shuttle proxy. Defaults to docker0 IP.")
	flag.BoolVar(&debug, "debug", false, "verbose logging")

	flag.Parse()

	if env == "" {
		fmt.Println("Need an env")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if pool == "" {
		fmt.Println("Need a pool")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if debug {
		log.DefaultLogger.Level = log.DEBUG
	}

	initOrDie()
	serviceRegistry.CreatePool(pool)

	err := startContainersIfNecessary()
	if err != nil && !loop {
		log.Printf("ERROR: Could not start containers: %s\n", err)
		return
	}

	err = serviceRuntime.StopAllButLatest(stopCutoff)
	if err != nil && !loop {
		log.Printf("ERROR: Could not start containers: %s\n", err)
		return
	}

	if !loop {
		return
	}

	restartChan := make(chan *registry.ConfigChange, 10)
	cancelChan := make(chan struct{})
	// do we need to cancel ever?

	serviceRegistry.Watch(restartChan, cancelChan)
	restartContainers(restartChan)
}
