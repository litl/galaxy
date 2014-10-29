package main

import (
	"errors"
	"flag"
	"fmt"
	golog "log"
	"os"
	"strings"
	"sync"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/litl/galaxy/commander"
	"github.com/litl/galaxy/config"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/runtime"
	"github.com/litl/galaxy/utils"
)

var (
	stopCutoff      int64
	apps            []string
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
	serviceConfigs  []*config.ServiceConfig
	serviceRegistry *registry.ServiceRegistry
	configStore     *config.ConfigStore
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

	configStore = config.NewConfigStore(
		"",
		registry.DefaultTTL,
		"",
	)
	configStore.Connect(redisHost)

	serviceRuntime = runtime.NewServiceRuntime(serviceRegistry, shuttleHost, statsdHost)

	apps, err := configStore.ListAssignments(env, pool)
	if err != nil {
		log.Fatalf("ERROR: Could not retrieve service configs for /%s/%s: %s", env, pool, err)
	}

	workerChans = make(map[string]chan string)
	for _, app := range apps {
		serviceConfig, err := configStore.Get(app, env)
		if err != nil {
			log.Fatalf("ERROR: Could not retrieve service config for /%s/%s: %s", env, pool, err)
		}

		workerChans[serviceConfig.Name] = make(chan string)
	}
}

func pullImageAsync(serviceConfig config.ServiceConfig, errChan chan error) {
	// err logged via pullImage
	_, err := pullImage(&serviceConfig)
	if err != nil {
		errChan <- err
		return
	}
	errChan <- nil
}

func pullImage(serviceConfig *config.ServiceConfig) (*docker.Image, error) {

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

func startService(serviceConfig *config.ServiceConfig, logStatus bool) {
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

	err = serviceRuntime.StopAllButLatestService(serviceConfig.Name, stopCutoff)
	if err != nil {
		log.Errorf("ERROR: Could not stop containers: %s", err)
	}
}

func appAssigned(app string) (bool, error) {
	assignments, err := configStore.ListAssignments(env, pool)
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

			serviceConfig, err := configStore.Get(app, env)
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

			serviceConfig, err := configStore.Get(app, env)
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

func monitorService(changedConfigs chan *config.ConfigChange) {

	for {

		var changedConfig *config.ConfigChange
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
	flag.StringVar(&redisHost, "redis", utils.GetEnv("GALAXY_REDIS_HOST", utils.DefaultRedisHost), "redis host")
	flag.StringVar(&env, "env", utils.GetEnv("GALAXY_ENV", ""), "Environment namespace")
	flag.StringVar(&pool, "pool", utils.GetEnv("GALAXY_POOL", ""), "Pool namespace")
	flag.StringVar(&shuttleHost, "shuttleAddr", "", "IP where containers can reach shuttle proxy. Defaults to docker0 IP.")
	flag.StringVar(&statsdHost, "statsdAddr", utils.GetEnv("GALAXY_STATSD_HOST", ""), "IP where containers can reach a statsd service. Defaults to docker0 IP:8125.")
	flag.BoolVar(&debug, "debug", false, "verbose logging")
	flag.BoolVar(&version, "v", false, "display version info")

	flag.Usage = func() {
		println("Usage: commander [options] <command> [<args>]\n")
		println("Available commands are:")
		println("   agent           Runs commander agent")
		println("   app             List all apps")
		println("   app:create      Create an app")
		println("   app:deploy      Deploy an app")
		println("   app:delete      Delete an app")
		println("   app:restart     Restart an app")
		println("   app:run         Run a command within an app on this host")
		println("   app:shell       Run a bash shell within an app on this host")
		println("   start           Starts one or more apps")
		println("   stop            Stops one or more apps")
		println("\nOptions:\n")
		flag.PrintDefaults()

	}

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
		flag.Usage()
		os.Exit(1)
	}

	initOrDie()
	log.DefaultLogger.SetFlags(0)

	switch flag.Args()[0] {
	case "agent":
		log.DefaultLogger.SetFlags(golog.LstdFlags)
		loop = true
		agentFs := flag.NewFlagSet("agent", flag.ExitOnError)
		agentFs.Usage = func() {
			println("Usage: commander agent [options]\n")
			println("    Runs commander continuously\n\n")
			println("Options:\n\n")
			agentFs.PrintDefaults()
		}
		agentFs.Parse(flag.Args()[1:])
	case "app":
		appFs := flag.NewFlagSet("app", flag.ExitOnError)
		appFs.Usage = func() {
			println("Usage: commander app\n")
			println("    List all apps or apps in an environment\n")
			println("Options:\n")
			appFs.PrintDefaults()
		}
		appFs.Parse(flag.Args()[1:])
		err := commander.AppList(configStore, env)
		if err != nil {
			log.Fatalf("ERROR: %s", err)
		}
		return
	case "app:create":
		appFs := flag.NewFlagSet("app:create", flag.ExitOnError)
		appFs.Usage = func() {
			println("Usage: commander app:create <app>\n")
			println("    Create an app in an environment\n")
			println("Options:\n")
			appFs.PrintDefaults()
		}
		appFs.Parse(flag.Args()[1:])

		if appFs.NArg() == 0 {
			appFs.Usage()
			os.Exit(1)
		}
		err := commander.AppCreate(configStore, appFs.Args()[0], env)
		if err != nil {
			log.Fatalf("ERROR: %s", err)
		}
		return

	case "app:delete":
		appFs := flag.NewFlagSet("app:delete", flag.ExitOnError)
		appFs.Usage = func() {
			println("Usage: commander app:delete <app>\n")
			println("    Delete an app in an environment\n")
			println("Options:\n")
			appFs.PrintDefaults()
		}
		appFs.Parse(flag.Args()[1:])

		if appFs.NArg() == 0 {
			appFs.Usage()
			os.Exit(1)
		}
		err := commander.AppDelete(configStore, appFs.Args()[0], env)
		if err != nil {
			log.Fatalf("ERROR: %s", err)
		}
		return

	case "app:deploy":
		var force bool
		appFs := flag.NewFlagSet("app:delete", flag.ExitOnError)
		appFs.BoolVar(&force, "force", false, "Force pulling image")
		appFs.Usage = func() {
			println("Usage: commander app:deploy <app> <version>\n")
			println("    Deploy an app in an environment\n")
			println("Options:\n")
			appFs.PrintDefaults()
		}
		appFs.Parse(flag.Args()[1:])

		if appFs.NArg() != 2 {
			appFs.Usage()
			os.Exit(1)
		}
		err := commander.AppDeploy(configStore, serviceRuntime, appFs.Args()[0], env, appFs.Args()[1], force)
		if err != nil {
			log.Fatalf("ERROR: %s", err)
		}
		return

	case "app:restart":
		appFs := flag.NewFlagSet("app:restart", flag.ExitOnError)
		appFs.Usage = func() {
			println("Usage: commander app:restart <app>\n")
			println("    Restart an app in an environment\n")
			println("Options:\n")
			appFs.PrintDefaults()
		}
		appFs.Parse(flag.Args()[1:])

		if appFs.NArg() == 0 {
			appFs.Usage()
			os.Exit(1)
		}
		err := commander.AppRestart(configStore, appFs.Args()[0], env)
		if err != nil {
			log.Fatalf("ERROR: %s", err)
		}
		return

	case "app:run":
		appFs := flag.NewFlagSet("app:run", flag.ExitOnError)
		appFs.Usage = func() {
			println("Usage: commander app:run <app> <cmd>\n")
			println("    Restart an app in an environment\n")
			println("Options:\n")
			appFs.PrintDefaults()
		}
		appFs.Parse(flag.Args()[1:])

		if appFs.NArg() < 2 {
			appFs.Usage()
			os.Exit(1)
		}
		err := commander.AppRun(configStore, serviceRuntime, appFs.Args()[0], env, appFs.Args()[1:])
		if err != nil {
			log.Fatalf("ERROR: %s", err)
		}
		return

	case "app:shell":
		appFs := flag.NewFlagSet("app:shell", flag.ExitOnError)
		appFs.Usage = func() {
			println("Usage: commander app:shell <app>\n")
			println("    Run a bash shell for an app\n")
			println("Options:\n")
			appFs.PrintDefaults()
		}
		appFs.Parse(flag.Args()[1:])

		if appFs.NArg() != 1 {
			appFs.Usage()
			os.Exit(1)
		}
		err := commander.AppShell(configStore, serviceRuntime, appFs.Args()[0], env)
		if err != nil {
			log.Fatalf("ERROR: %s", err)
		}
		return

	case "start":

		startFs := flag.NewFlagSet("start", flag.ExitOnError)
		startFs.Usage = func() {
			println("Usage: commander start [options] [<app>]*\n")
			println("    Starts one or more apps. If no apps are specified, starts all apps.\n")
			println("Options:\n")
			startFs.PrintDefaults()
		}
		startFs.Parse(flag.Args()[1:])

		apps = startFs.Args()

		break
	case "stop":
		stopFs := flag.NewFlagSet("stop", flag.ExitOnError)
		stopFs.Usage = func() {
			println("Usage: commander stop [options] [<app>]*\n")
			println("    Stops one or more apps. If no apps are specified, stops all apps.\n")
			println("Options:\n")
			stopFs.PrintDefaults()
		}
		stopFs.Parse(flag.Args()[1:])

		apps = stopFs.Args()

		for _, app := range apps {
			err := serviceRuntime.StopAllMatching(app)
			if err != nil {
				log.Fatalf("ERROR: Unable able to stop all containers: %s", err)
			}
		}
		if len(apps) > 0 {
			return
		}

		err := serviceRuntime.StopAll(env)
		if err != nil {
			log.Fatalf("ERROR: Unable able to stop all containers: %s", err)
		}
		return
	}

	log.Printf("Starting commander %s", buildVersion)
	log.Printf("Using env = %s, pool = %s",
		env, pool)

	for app, ch := range workerChans {
		if len(apps) == 0 || utils.StringInSlice(app, apps) {
			wg.Add(1)
			go restartContainers(app, ch)
			ch <- "deploy"
		}
	}

	if loop {

		cancelChan := make(chan struct{})
		// do we need to cancel ever?

		restartChan := configStore.Watch(env, cancelChan)
		monitorService(restartChan)
	}

	wg.Wait()
}
