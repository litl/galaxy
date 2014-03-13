package main

import (
	"flag"
	"fmt"
	"github.com/fsouza/go-dockerclient"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/runtime"
	"os"
)

var (
	client          *docker.Client
	stopCutoff      = flag.Int64("cutoff", 5*60, "Seconds to wait before stopping old containers")
	app             = flag.String("app", "", "App to start")
	etcdHosts       = flag.String("etcd", "http://127.0.0.1:4001", "Comma-separated list of etcd hosts")
	env             = flag.String("env", "dev", "Environment namespace")
	pool            = flag.String("pool", "web", "Pool namespace")
	serviceConfigs  []*registry.ServiceConfig
	hostname        string
	serviceRegistry *registry.ServiceRegistry
	serviceRuntime  *runtime.ServiceRuntime
)

func initOrDie() {
	var err error
	endpoint := "unix:///var/run/docker.sock"
	client, err = docker.NewClient(endpoint)

	if err != nil {
		panic(err)
	}

	hostname, err = os.Hostname()
	if err != nil {
		panic(err)
	}

	serviceRegistry = &registry.ServiceRegistry{
		Client:    client,
		EtcdHosts: *etcdHosts,
		Env:       *env,
		Pool:      *pool,
		//FIXME: Move these closer to functions that use them
		//HostIp:       "FIXME"
		//TTL:          uint64(c.Int("ttl")),
		//Hostname:     hostname,
		//HostSSHAddr:  c.GlobalString("sshAddr"),
		//OutputBuffer: outputBuffer,
	}

	serviceRuntime = &runtime.ServiceRuntime{}

}

func main() {
	flag.Parse()

	if *env == "" {
		fmt.Println("Need an env")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if *pool == "" {
		fmt.Println("Need a pool")
		flag.PrintDefaults()
		os.Exit(1)
	}

	initOrDie()

	serviceConfigs = serviceRegistry.GetServiceConfigs()

	if len(serviceConfigs) == 0 {
		fmt.Printf("No services configured for /%s/%s\n", *env, *pool)
		os.Exit(0)
	}

	for _, serviceConfig := range serviceConfigs {

		if *app != "" && serviceConfig.Name != *app {
			continue
		}

		if serviceConfig.Version == "" {
			fmt.Printf("Skipping %s. No version configured.\n", serviceConfig.Name)
			continue
		}

		container, err := serviceRuntime.StartIfNotRunning(serviceConfig)
		if err != nil {
			fmt.Printf("ERROR: Could not determine if %s is running: %s\n",
				serviceConfig.Version, err)
			os.Exit(1)
		}

		fmt.Printf("%s running as %s\n", serviceConfig.Version, container.ID)

		serviceRuntime.StopAllButLatest(serviceConfig.Version, container, *stopCutoff)

	}

}
