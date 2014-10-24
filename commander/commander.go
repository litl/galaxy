package main

import (
	"errors"
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
		"",
		registry.DefaultTTL,
		"",
	)

	serviceRegistry.Connect(redisHost)
	serviceRuntime = runtime.NewServiceRuntime(serviceRegistry, shuttleHost, statsdHost)

	apps, err := serviceRegistry.ListAssignments(env, pool)
	if err != nil {
		log.Fatalf("ERROR: Could not retrieve service configs for /%s/%s: %s\n", env, pool, err)
	}

	workerChans = make(map[string]chan string)
	for _, app := range apps {
		serviceConfig, err := serviceRegistry.GetServiceConfig(app, env)
		if err != nil {
			log.Fatalf("ERROR: Could not retrieve service config for /%s/%s: %s\n", env, pool, err)
		}

		workerChans[serviceConfig.Name] = make(chan string)
	}
}

func pullImageAsync(serviceConfig registry.ServiceConfig, errChan chan error) {
	// err logged via pullImage
	_, err := pullImage(&serviceConfig)
	if err != nil {
		errChan <- err
		return
	}
	errChan <- nil
}

func pullImage(serviceConfig *registry.ServiceConfig) (*docker.Image, error) {

	image, err := serviceRuntime.InspectImage(serviceConfig.Version())
	if image != nil && image.ID == serviceConfig.VersionID() || serviceConfig.VersionID() == "" {
		return image, nil
	}

	log.Printf("Pulling %s version %s\n", serviceConfig.Name, serviceConfig.Version())
	image, err = serviceRuntime.PullImage(serviceConfig.Version(),
		serviceConfig.VersionID(), true)
	if image == nil || err != nil {
		log.Errorf("ERROR: Could not pull image %s: %s",
			serviceConfig.Version(), err)
		return nil, err
	}

	if image.ID != serviceConfig.VersionID() && len(serviceConfig.VersionID()) > 12 {
		log.Errorf("ERROR: Pulled image for %s does not match expected ID. Expected: %s: Got: %s",
			serviceConfig.Version(),
			image.ID[0:12], serviceConfig.VersionID()[0:12])
		return nil, errors.New(fmt.Sprintf("failed to pull image ID %s", serviceConfig.VersionID()[0:12]))
	}

	log.Printf("Pulled %s\n", serviceConfig.Version())
	return image, nil
}

func startService(serviceConfig *registry.ServiceConfig, logStatus bool) {
	started, container, err := serviceRuntime.StartIfNotRunning(env, serviceConfig)
	if err != nil {
		log.Errorf("ERROR: Could not start container for %s: %s", serviceConfig.Version(), err)
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
		log.Errorf("ERROR: Could not stop containers: %s", err)
	}
}

func appAssigned(app string) (bool, error) {
	assignments, err := serviceRegistry.ListAssignments(env, pool)
	if err != nil {
		return false, err
	}

	if !utils.StringInSlice(app, assignments) {
		return false, nil
	}
	return true, nil
}

func restartContainers(app string, cmdChan chan string) {
	defer wg.Done()
	logOnce := true

	ticker := time.NewTicker(10 * time.Second)

	for {

		select {

		case cmd := <-cmdChan:

			assigned, err := appAssigned(app)
			if err != nil {
				log.Errorf("ERROR: Error retrieving assignments for %s: %s", app, err)
				if !loop {
					return
				}
				continue
			}

			if !assigned {
				continue
			}

			serviceConfig, err := serviceRegistry.GetServiceConfig(app, env)
			if err != nil {
				log.Errorf("ERROR: Error retrieving service config for %s: %s", app, err)
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
					continue
				}
				startService(serviceConfig, logOnce)
			}

			if cmd == "restart" {
				err := serviceRuntime.Stop(serviceConfig)
				if err != nil {
					log.Errorf("ERROR: Could not stop %s: %s",
						serviceConfig.Version(), err)
					if !loop {
						return
					}

					startService(serviceConfig, logOnce)
					continue
				}
			}

			logOnce = false
		case <-ticker.C:

			serviceConfig, err := serviceRegistry.GetServiceConfig(app, env)
			if err != nil {
				log.Errorf("ERROR: Error retrieving service config for %s: %s", app, err)
				continue
			}

			assigned, err := appAssigned(app)
			if err != nil {
				log.Errorf("ERROR: Error retrieving service config for %s: %s", app, err)
				if !loop {
					return
				}

				continue
			}

			if serviceConfig == nil || !assigned {
				log.Errorf("%s no longer exists.  Stopping worker.", app)
				serviceRuntime.StopAllMatching(app)
				delete(workerChans, app)
				return
			}

			if serviceConfig.Version() == "" {
				continue
			}

			_, err = pullImage(serviceConfig)
			if err != nil {
				if !loop {
					return
				}
				log.Errorf("ERROR: Could not pull images: %s", err)
				continue
			}

			started, container, err := serviceRuntime.StartIfNotRunning(env, serviceConfig)
			if err != nil {
				log.Errorf("ERROR: Could not start containers: %s", err)
				continue
			}

			if started {
				log.Printf("Started %s version %s as %s\n", serviceConfig.Name, serviceConfig.Version(), container.ID[0:12])
			}

			log.Debugf("%s version %s running as %s\n", serviceConfig.Name, serviceConfig.Version(), container.ID[0:12])

			err = serviceRuntime.StopAllButCurrentVersion(serviceConfig)
			if err != nil {
				log.Errorf("ERROR: Could not stop containers: %s", err)
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
				log.Errorf("ERROR: Error watching changes: %s", changedConfig.Error)
				continue
			}

			if changedConfig.ServiceConfig == nil {
				continue
			}

			assigned, err := appAssigned(changedConfig.ServiceConfig.Name)
			if err != nil {
				log.Errorf("ERROR: Error retrieving service config for %s: %s", changedConfig.ServiceConfig.Name, err)
				if !loop {
					return
				}
				continue
			}

			if !assigned {
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

	if flag.NArg() < 1 {
		fmt.Println("Need a command")
		flag.PrintDefaults()
		os.Exit(1)
	}

	switch flag.Args()[0] {
	case "agent":
		loop = true
	}

	initOrDie()
	serviceRegistry.CreatePool(pool, env)

	for app, ch := range workerChans {
		wg.Add(1)
		go restartContainers(app, ch)
		ch <- "deploy"
	}

	if loop {
		log.Printf("Starting commander %s", buildVersion)
		log.Printf("Using env = %s, pool = %s",
			env, pool)

		cancelChan := make(chan struct{})
		// do we need to cancel ever?

		restartChan := serviceRegistry.Watch(env, cancelChan)
		monitorService(restartChan)
	}

	wg.Wait()
}
