package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	docker "github.com/fsouza/go-dockerclient"
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
	statsdHost      string
	debug           bool
	runOnce         bool
	version         bool
	buildVersion    string
	serviceConfigs  []*registry.ServiceConfig
	serviceRegistry *registry.ServiceRegistry
	serviceRuntime  *runtime.ServiceRuntime
	workerChans     map[string]chan string
	wg              sync.WaitGroup
)

func initOrDie() {

	serviceRegistry = registry.NewServiceRegistry(
		env,
		pool,
		"",
		registry.DefaultTTL,
		"",
	)

	serviceRegistry.Connect(redisHost)
	serviceRuntime = runtime.NewServiceRuntime(serviceRegistry, shuttleHost, statsdHost)

	serviceConfigs, err := serviceRegistry.ListApps()
	if err != nil {
		log.Fatalf("ERROR: Could not retrieve service configs for /%s/%s: %s\n", env, pool, err)
	}

	workerChans = make(map[string]chan string)
	for _, serviceConfig := range serviceConfigs {
		workerChans[serviceConfig.Name] = make(chan string)
	}
}

func pullAllImages() error {
	serviceConfigs, err := serviceRegistry.ListApps()
	if err != nil {
		log.Errorf("ERROR: Could not retrieve service configs for /%s/%s: %s\n", env, pool, err)
		return err
	}

	if len(serviceConfigs) == 0 {
		log.Printf("No services configured for /%s/%s\n", env, pool)
		return err
	}

	errChan := make(chan error)
	for _, serviceConfig := range serviceConfigs {
		go func(serviceConfig registry.ServiceConfig, errChan chan error) {
			// err logged via pullImage
			_, err := pullImage(&serviceConfig)
			if err != nil {
				errChan <- err
				return
			}
			errChan <- nil

		}(serviceConfig, errChan)
	}

	for i := 0; i < len(serviceConfigs); i++ {
		err := <-errChan
		if err != nil {
			// return the first error we got to signal that one of the pulls failed
			return err
		}
	}
	return nil
}

func pullImage(serviceConfig *registry.ServiceConfig) (*docker.Image, error) {
	log.Printf("Pulling %s version %s\n", serviceConfig.Name, serviceConfig.Version())
	image, err := serviceRuntime.PullImage(serviceConfig.Version(), true)
	if image == nil || err != nil {
		log.Errorf("ERROR: Could not pull image %s: %s\n",
			serviceConfig.Version(), err)
		return nil, err
	}
	log.Printf("Pulled %s\n", serviceConfig.Version())
	return image, nil
}

func startService(serviceConfig *registry.ServiceConfig, logStatus bool) {
	started, container, err := serviceRuntime.StartIfNotRunning(serviceConfig)
	if err != nil {
		log.Errorf("ERROR: Could not start containers: %s\n", err)
		return
	}

	if started {
		log.Printf("Started %s version %s as %s\n", serviceConfig.Name, serviceConfig.Version(), container.ID[0:12])
	}

	if logStatus && !debug {
		log.Printf("%s version %s running as %s\n", serviceConfig.Name, serviceConfig.Version(), container.ID[0:12])
	}

	log.Debugf("%s version %s running as %s\n", serviceConfig.Name, serviceConfig.Version(), container.ID[0:12])

	err = serviceRuntime.StopAllButLatestService(serviceConfig, stopCutoff)
	if err != nil {
		log.Errorf("ERROR: Could not stop containers: %s\n", err)
	}
}

func restartContainers(app string, cmdChan chan string) {
	defer wg.Done()
	logOnce := true

	ticker := time.NewTicker(10 * time.Second)

	for {

		select {

		case cmd := <-cmdChan:
			serviceConfig, err := serviceRegistry.GetServiceConfig(app)
			if err != nil {
				log.Errorf("ERROR: Error retrieving service config for %s: %s\n", app, err)
				if !loop {
					return
				}

				continue
			}

			if serviceConfig.Version() == "" {
				if !loop {
					return
				}
				continue
			}

			if cmd == "deploy" {
				_, err = pullImage(serviceConfig)
				if err != nil {
					if !loop {
						return
					}

					// if we can't pull the image, leave whatever is running alone
					continue
				}

			}

			if cmd == "restart" {
				err := serviceRuntime.Stop(serviceConfig)
				if err != nil {
					log.Errorf("ERROR: Could not stop %s: %s\n",
						serviceConfig.Version(), err)
					if !loop {
						return
					}

					continue
				}
			}

			startService(serviceConfig, logOnce)
			logOnce = false
		case <-ticker.C:

			serviceConfig, err := serviceRegistry.GetServiceConfig(app)
			if err != nil {
				log.Errorf("ERROR: Error retrieving service config for %s: %s\n", app, err)
				continue
			}

			if serviceConfig == nil {
				log.Errorf("%s no longer exists.  Stopping worker.", app)
				serviceRuntime.StopAllMatching(app)
				return
			}

			if serviceConfig.Version() == "" {
				continue
			}

			started, container, err := serviceRuntime.StartIfNotRunning(serviceConfig)
			if err != nil {
				log.Errorf("ERROR: Could not start containers: %s\n", err)
				continue
			}

			if started {
				log.Printf("Started %s version %s as %s\n", serviceConfig.Name, serviceConfig.Version(), container.ID[0:12])
			}

			log.Debugf("%s version %s running as %s\n", serviceConfig.Name, serviceConfig.Version(), container.ID[0:12])

			err = serviceRuntime.StopAllButLatestService(serviceConfig, stopCutoff)
			if err != nil {
				log.Errorf("ERROR: Could not stop containers: %s\n", err)
			}
		}

		if !loop {
			return
		}

	}
}

func monitorService(changedConfigs chan *registry.ConfigChange) {

	for {

		var changedConfig *registry.ConfigChange
		select {

		case changedConfig = <-changedConfigs:
			if changedConfig.Error != nil {
				log.Errorf("ERROR: Error watching changes: %s\n", changedConfig.Error)
				continue
			}

			if changedConfig.ServiceConfig == nil {
				continue
			}

			ch, ok := workerChans[changedConfig.ServiceConfig.Name]
			if !ok {
				name := changedConfig.ServiceConfig.Name
				ch := make(chan string)
				workerChans[name] = ch
				wg.Add(1)
				go restartContainers(name, ch)
				ch <- "deploy"

				log.Printf("Started new worker for %s\n", name)
				continue
			}

			if changedConfig.Restart {
				log.Printf("Restarting %s", changedConfig.ServiceConfig.Name)
				ch <- "restart"
			} else {
				ch <- "deploy"
			}
		}
	}

}

func main() {
	flag.Int64Var(&stopCutoff, "cutoff", 10, "Seconds to wait before stopping old containers")
	flag.StringVar(&app, "app", "", "App to start")
	flag.StringVar(&redisHost, "redis", utils.GetEnv("GALAXY_REDIS_HOST", utils.DefaultRedisHost), "redis host")
	flag.StringVar(&env, "env", utils.GetEnv("GALAXY_ENV", ""), "Environment namespace")
	flag.StringVar(&pool, "pool", utils.GetEnv("GALAXY_POOL", ""), "Pool namespace")
	flag.BoolVar(&loop, "loop", false, "Run continously")
	flag.StringVar(&shuttleHost, "shuttleAddr", "", "IP where containers can reach shuttle proxy. Defaults to docker0 IP.")
	flag.StringVar(&statsdHost, "statsdAddr", utils.GetEnv("GALAXY_STATSD_HOST", ""), "IP where containers can reach a statsd service. Defaults to docker0 IP:8125.")
	flag.BoolVar(&debug, "debug", false, "verbose logging")
	flag.BoolVar(&version, "v", false, "display version info")

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

	if strings.TrimSpace(pool) == "" {
		fmt.Println("Need a pool")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if debug {
		log.DefaultLogger.Level = log.DEBUG
	}

	initOrDie()
	serviceRegistry.CreatePool(pool)

	for app, ch := range workerChans {
		wg.Add(1)
		go restartContainers(app, ch)
		ch <- "deploy"
	}

	if loop {
		cancelChan := make(chan struct{})
		// do we need to cancel ever?

		restartChan := serviceRegistry.Watch(cancelChan)
		monitorService(restartChan)
	}

	wg.Wait()
}
