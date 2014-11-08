package main

import (
	"errors"
	"flag"
	"fmt"
	golog "log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"strconv"
	"syscall"

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
	hostIP          string
	dns             string
	debug           bool
	runOnce         bool
	version         bool
	buildVersion    string
	serviceRegistry *registry.ServiceRegistry
	configStore     *config.Store
	serviceRuntime  *runtime.ServiceRuntime
	workerChans     map[string]chan string
	wg              sync.WaitGroup
	signalsChan     chan os.Signal
)

func initOrDie() {

	serviceRegistry = registry.NewServiceRegistry(
		registry.DefaultTTL,
	)
	serviceRegistry.Connect(redisHost)

	configStore = config.NewStore(
		registry.DefaultTTL,
	)

	configStore.Connect(redisHost)

	serviceRuntime = runtime.NewServiceRuntime(serviceRegistry, dns, hostIP)

	apps, err := configStore.ListAssignments(env, pool)
	if err != nil {
		log.Fatalf("ERROR: Could not retrieve service configs for /%s/%s: %s", env, pool, err)
	}

	workerChans = make(map[string]chan string)
	for _, app := range apps {
		appCfg, err := configStore.GetApp(app, env)
		if err != nil {
			log.Fatalf("ERROR: Could not retrieve service config for /%s/%s: %s", env, pool, err)
		}

		workerChans[appCfg.Name] = make(chan string)
	}

	signalsChan = make(chan os.Signal, 1)
	signal.Notify(signalsChan, os.Interrupt, os.Kill, syscall.SIGTERM)
	go deregisterHost(signalsChan)
}

func pullImageAsync(appCfg config.AppConfig, errChan chan error) {
	// err logged via pullImage
	_, err := pullImage(&appCfg)
	if err != nil {
		errChan <- err
		return
	}
	errChan <- nil
}

func pullImage(appCfg *config.AppConfig) (*docker.Image, error) {

	image, err := serviceRuntime.InspectImage(appCfg.Version())
	if image != nil && image.ID == appCfg.VersionID() || appCfg.VersionID() == "" {
		return image, nil
	}

	log.Printf("Pulling %s version %s\n", appCfg.Name, appCfg.Version())
	image, err = serviceRuntime.PullImage(appCfg.Version(),
		appCfg.VersionID())
	if image == nil || err != nil {
		log.Errorf("ERROR: Could not pull image %s: %s",
			appCfg.Version(), err)
		return nil, err
	}

	if image.ID != appCfg.VersionID() && len(appCfg.VersionID()) > 12 {
		log.Errorf("ERROR: Pulled image for %s does not match expected ID. Expected: %s: Got: %s",
			appCfg.Version(),
			image.ID[0:12], appCfg.VersionID()[0:12])
		return nil, errors.New(fmt.Sprintf("failed to pull image ID %s", appCfg.VersionID()[0:12]))
	}

	log.Printf("Pulled %s\n", appCfg.Version())
	return image, nil
}

func startService(appCfg *config.AppConfig, logStatus bool) {

	desired, err := commander.Balanced(configStore, hostIP, appCfg.Name, env, pool)
	if err != nil {
		log.Errorf("ERROR: Could not determine instance count: %s", err)
		return
	}

	running, err := serviceRuntime.InstanceCount(appCfg.Name, strconv.FormatInt(appCfg.ID(), 10))
	if err != nil {
		log.Errorf("ERROR: Could not determine running instance count: %s", err)
		return
	}

	for i := 0; i < desired-running; i++ {
		container, err := serviceRuntime.Start(env, appCfg)
		if err != nil {
			log.Errorf("ERROR: Could not start containers: %s", err)
			return
		}

		log.Printf("Started %s version %s as %s\n", appCfg.Name, appCfg.Version(), container.ID[0:12])
	}

	err = serviceRuntime.StopAllButCurrentVersion(appCfg)
	if err != nil {
		log.Errorf("ERROR: Could not stop containers: %s", err)
	}

	running, err = serviceRuntime.InstanceCount(appCfg.Name, strconv.FormatInt(appCfg.ID(), 10))
	if err != nil {
		log.Errorf("ERROR: Could not determine running instance count: %s", err)
		return
	}

	for i := 0; i < running-desired; i++ {
		err := serviceRuntime.Stop(appCfg)
		if err != nil {
			log.Errorf("ERROR: Could not stop container: %s", err)
		}
	}

}

func heartbeatHost() {
	wg.Add(1)
	defer wg.Done()
	for {
		configStore.UpdateHost(env, pool, config.HostInfo{
			HostIP: hostIP,
		})

		if !loop {
			return
		}
		time.Sleep(45 * time.Second)
	}
}

func deregisterHost(signals chan os.Signal) {
	<-signals
	configStore.DeleteHost(env, pool, config.HostInfo{
		HostIP: hostIP,
	})
	os.Exit(0)
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

			appCfg, err := configStore.GetApp(app, env)
			if err != nil {
				log.Errorf("ERROR: Error retrieving service config for %s: %s", app, err)
				if !loop {
					return
				}
				continue
			}

			if appCfg.Version() == "" {
				if !loop {
					return
				}
				continue
			}

			if cmd == "deploy" {
				_, err = pullImage(appCfg)
				if err != nil {
					log.Errorf("ERROR: Error pulling image for %s: %s", app, err)
					if !loop {
						return
					}
					continue
				}
				startService(appCfg, logOnce)
			}

			if cmd == "restart" {
				err := serviceRuntime.Stop(appCfg)
				if err != nil {
					log.Errorf("ERROR: Could not stop %s: %s",
						appCfg.Version(), err)
					if !loop {
						return
					}

					startService(appCfg, logOnce)
					continue
				}
			}

			logOnce = false
		case <-ticker.C:

			appCfg, err := configStore.GetApp(app, env)
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

			if appCfg == nil || !assigned {
				log.Errorf("%s no longer exists.  Stopping worker.", app)
				serviceRuntime.StopAllMatching(app)
				delete(workerChans, app)
				return
			}

			if appCfg.Version() == "" {
				continue
			}

			_, err = pullImage(appCfg)
			if err != nil {
				if !loop {
					return
				}
				log.Errorf("ERROR: Could not pull images: %s", err)
				continue
			}
			startService(appCfg, logOnce)
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

			if changedConfig.AppConfig == nil {
				continue
			}

			assigned, err := appAssigned(changedConfig.AppConfig.Name)
			if err != nil {
				log.Errorf("ERROR: Error retrieving service config for %s: %s", changedConfig.AppConfig.Name, err)
				if !loop {
					return
				}
				continue
			}

			if !assigned {
				continue
			}

			ch, ok := workerChans[changedConfig.AppConfig.Name]
			if !ok {
				name := changedConfig.AppConfig.Name
				ch := make(chan string)
				workerChans[name] = ch
				wg.Add(1)
				go restartContainers(name, ch)
				ch <- "deploy"

				log.Printf("Started new worker for %s\n", name)
				continue
			}

			if changedConfig.Restart {
				log.Printf("Restarting %s", changedConfig.AppConfig.Name)
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
	flag.StringVar(&hostIP, "host-ip", "127.0.0.1", "Host IP")
	flag.StringVar(&dns, "dns", "", "DNS addr to use for containers")
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
		println("   app:start       Starts one or more apps")
		println("   app:stop        Stops one or more apps")
		println("   config          List config for an app")
		println("   config:get      Get config values for an app")
		println("   config:set      Set config values for an app")
		println("   config:unset    Unset config values for an app")
		println("   runtime         List container runtime policies")
		println("   runtime:set     Set container runtime policies")
		println("   hosts           List hosts in an env and pool")
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

		hosts, err := configStore.ListHosts(env, pool)
		if err != nil {
			log.Fatalf("ERROR: %s", err)
		}

		for _, hi := range hosts {
			if hi.HostIP == hostIP {
				log.Fatalf("ERROR: agent already running on this host")
			}
		}

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
		appFs := flag.NewFlagSet("app:delete", flag.ExitOnError)
		appFs.Usage = func() {
			println("Usage: commander app:deploy [-force] <app> <version>\n")
			println("    Deploy an app in an environment\n")
			println("Options:\n")
			appFs.PrintDefaults()
		}
		appFs.Parse(flag.Args()[1:])

		if appFs.NArg() != 2 {
			appFs.Usage()
			os.Exit(1)
		}
		err := commander.AppDeploy(configStore, serviceRuntime, appFs.Args()[0], env, appFs.Args()[1])
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

	case "app:start":

		startFs := flag.NewFlagSet("app:start", flag.ExitOnError)
		startFs.Usage = func() {
			println("Usage: commander start [options] [<app>]*\n")
			println("    Starts one or more apps. If no apps are specified, starts all apps.\n")
			println("Options:\n")
			startFs.PrintDefaults()
		}
		startFs.Parse(flag.Args()[1:])

		apps = startFs.Args()

		if len(apps) == 0 {
			acs, err := configStore.ListApps(env)
			if err != nil {
				log.Fatalf("ERROR: Unable to list apps: %s", err)
			}
			for _, ac := range acs {
				apps = append(apps, ac.Name)
			}
		}
		break
	case "app:stop":
		stopFs := flag.NewFlagSet("app:stop", flag.ExitOnError)
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
	case "hosts":
		hostFs := flag.NewFlagSet("hosts", flag.ExitOnError)
		hostFs.Usage = func() {
			println("Usage: commander hosts\n")
			println("    List hosts in an env and pool\n")
			println("Options:\n")
			hostFs.PrintDefaults()
		}
		err := hostFs.Parse(flag.Args()[1:])
		if err != nil {
			log.Fatalf("ERROR: Bad command line options: %s", err)
		}

		err = commander.HostsList(configStore, env, pool)
		if err != nil {
			log.Fatalf("ERROR: %s", err)
		}
		return
	case "config":
		configFs := flag.NewFlagSet("config", flag.ExitOnError)
		configFs.Usage = func() {
			println("Usage: commander config <app>\n")
			println("    List config values for an app\n")
			println("Options:\n")
			configFs.PrintDefaults()
		}
		err := configFs.Parse(flag.Args()[1:])
		if err != nil {
			log.Fatalf("ERROR: Bad command line options: %s", err)
		}

		if configFs.NArg() != 1 {
			log.Errorf("ERROR: Missing app name")
			configFs.Usage()
			os.Exit(1)
		}
		app := configFs.Args()[0]

		err = commander.ConfigList(configStore, app, env)
		if err != nil {
			log.Fatalf("ERROR: %s", err)
		}
		return
	case "config:get":
		configFs := flag.NewFlagSet("config:get", flag.ExitOnError)
		configFs.Usage = func() {
			println("Usage: commander config <app> KEY [KEY]*\n")
			println("    Get config values for an app\n")
			println("Options:\n")
			configFs.PrintDefaults()
		}
		err := configFs.Parse(flag.Args()[1:])
		if err != nil {
			log.Fatalf("ERROR: Bad command line options: %s", err)
		}

		if configFs.NArg() == 0 {
			log.Errorf("ERROR: Missing app name")
			configFs.Usage()
			os.Exit(1)
		}
		app := configFs.Args()[0]

		err = commander.ConfigGet(configStore, app, env, configFs.Args()[1:])
		if err != nil {
			log.Fatalf("ERROR: %s", err)
		}
		return
	case "config:set":
		configFs := flag.NewFlagSet("config:set", flag.ExitOnError)
		configFs.Usage = func() {
			println("Usage: commander config <app> KEY=VALUE [KEY=VALUE]*\n")
			println("    Set config values for an app\n")
			println("Options:\n")
			configFs.PrintDefaults()
		}
		err := configFs.Parse(flag.Args()[1:])
		if err != nil {
			log.Fatalf("ERROR: Bad command line options: %s", err)
		}

		if configFs.NArg() == 0 {
			log.Errorf("ERROR: Missing app name")
			configFs.Usage()
			os.Exit(1)
		}
		app := configFs.Args()[0]

		err = commander.ConfigSet(configStore, app, env, configFs.Args()[1:])
		if err != nil {
			log.Fatalf("ERROR: %s", err)
		}
		return
	case "config:unset":
		configFs := flag.NewFlagSet("config:unset", flag.ExitOnError)
		configFs.Usage = func() {
			println("Usage: commander config <app> KEY [KEY]*\n")
			println("    Unset config values for an app\n")
			println("Options:\n")
			configFs.PrintDefaults()
		}
		err := configFs.Parse(flag.Args()[1:])
		if err != nil {
			log.Fatalf("ERROR: Bad command line options: %s", err)
		}

		if configFs.NArg() == 0 {
			log.Errorf("ERROR: Missing app name")
			configFs.Usage()
			os.Exit(1)
		}
		app := configFs.Args()[0]

		err = commander.ConfigUnset(configStore, app, env, configFs.Args()[1:])
		if err != nil {
			log.Fatalf("ERROR: %s", err)
		}
		return

	case "runtime":
		runtimeFs := flag.NewFlagSet("runtime", flag.ExitOnError)
		runtimeFs.Usage = func() {
			println("Usage: commander runtime\n")
			println("    List container runtime policies\n")
			println("Options:\n")
			runtimeFs.PrintDefaults()
		}
		err := runtimeFs.Parse(flag.Args()[1:])
		if err != nil {
			log.Fatalf("ERROR: Bad command line options: %s", err)
		}

		app := ""
		if runtimeFs.NArg() > 0 {
			app = runtimeFs.Args()[0]
		}

		err = commander.RuntimeList(configStore, app, env, pool)
		if err != nil {
			log.Fatalf("ERROR: %s", err)
		}
		return

	case "runtime:set":
		var ps int
		runtimeFs := flag.NewFlagSet("runtime:set", flag.ExitOnError)
		runtimeFs.IntVar(&ps, "ps", 0, "Number of instances to run across all hosts")
		runtimeFs.Usage = func() {
			println("Usage: commander runtime:set [-ps 1] <app>\n")
			println("    Set container runtime policies\n")
			println("Options:\n")
			runtimeFs.PrintDefaults()
		}

		err := runtimeFs.Parse(flag.Args()[1:])
		if err != nil {
			log.Fatalf("ERROR: Bad command line options: %s", err)
		}

		if runtimeFs.NArg() != 1 {
			runtimeFs.Usage()
			os.Exit(1)
		}

		app := runtimeFs.Args()[0]

		updated, err := commander.RuntimeSet(configStore, app, env, pool, commander.RuntimeOptions{
			Ps: ps,
		})
		if err != nil {
			log.Fatalf("ERROR: %s", err)
		}

		if !updated {
			log.Fatalf("ERROR: Failed to set runtime options.")
		}
		log.Printf("Runtime options update for %s in %s running on %s", app, env, pool)
		return

	default:
		fmt.Println("Unknown command")
		flag.Usage()
		os.Exit(1)
	}

	log.Printf("Starting commander %s", buildVersion)
	log.Printf("Using env = %s, pool = %s",
		env, pool)

	go heartbeatHost()

	defer func() {
		configStore.DeleteHost(env, pool, config.HostInfo{
			HostIP: hostIP,
		})
	}()

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
