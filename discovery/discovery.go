package main

import (
	"os"

	"github.com/codegangsta/cli"
	docker "github.com/fsouza/go-dockerclient"
	"github.com/litl/galaxy/config"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/runtime"
	"github.com/litl/galaxy/utils"
)

var (
	client          *docker.Client
	serviceRegistry *registry.ServiceRegistry
	configStore     *config.Store
	serviceRuntime  *runtime.ServiceRuntime
	outputBuffer    *utils.OutputBuffer
	buildVersion    string
)

func initOrDie(c *cli.Context) {
	var err error
	endpoint := runtime.GetEndpoint()
	client, err = docker.NewClient(endpoint)

	// Don't log timestamps, etc. if running interactively
	if !c.Bool("loop") {
		log.DefaultLogger.SetFlags(0)
	}

	if err != nil {
		log.Fatalf("ERROR: %s", err)
	}

	if utils.GalaxyEnv(c) == "" {
		log.Fatalln("ERROR: env not set.  Pass -env or set GALAXY_ENV.")
	}

	if utils.GalaxyPool(c) == "" {
		log.Fatalln("ERROR: pool not set.  Pass -pool or set GALAXY_POOL.")
	}

	serviceRegistry = registry.NewServiceRegistry(
		c.GlobalString("hostIp"),
		uint64(c.Int("ttl")),
		c.GlobalString("sshAddr"),
	)

	serviceRegistry.Connect(utils.GalaxyRedisHost(c))

	configStore = config.NewStore(
		c.GlobalString("hostIp"),
		uint64(c.Int("ttl")),
		c.GlobalString("sshAddr"),
	)

	configStore.Connect(utils.GalaxyRedisHost(c))

	serviceRuntime = runtime.NewServiceRuntime(
		serviceRegistry,
		c.GlobalString("shuttleAddr"),
		"",
	)

	outputBuffer = &utils.OutputBuffer{}
	serviceRegistry.OutputBuffer = outputBuffer

	if c.Bool("loop") {
		log.Printf("Starting discovery %s", buildVersion)
		log.Printf("Using env = %s, pool = %s, HostIp = %s",
			utils.GalaxyEnv(c), utils.GalaxyPool(c),
			c.GlobalString("hostIp"))
	}

	if c.GlobalBool("debug") {
		log.DefaultLogger.Level = log.DEBUG
	}
}

func main() {

	app := cli.NewApp()
	app.Name = "discovery"
	app.Usage = "discovery service registration"
	app.Version = buildVersion
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "redis", Value: utils.DefaultRedisHost, Usage: "host:port[,host:port,..]"},
		cli.StringFlag{Name: "env", Value: utils.DefaultEnv, Usage: "environment (dev, test, prod, etc.)"},
		cli.StringFlag{Name: "pool", Value: utils.DefaultPool, Usage: "pool (web, worker, etc.)"},
		cli.StringFlag{Name: "hostIp", Value: "127.0.0.1", Usage: "hosts external IP"},
		cli.StringFlag{Name: "sshAddr", Value: "127.0.0.1:22", Usage: "hosts external ssh IP:port"},
		cli.StringFlag{Name: "shuttleAddr", Value: "127.0.0.1:9090", Usage: "shuttle http address"},
		cli.BoolFlag{Name: "debug", Usage: "enbable debug logging"},
	}

	app.Commands = []cli.Command{
		{
			Name:        "register",
			Usage:       "discovers and registers running containers",
			Action:      register,
			Description: "register [options]",
			Flags: []cli.Flag{
				cli.IntFlag{Name: "ttl", Value: registry.DefaultTTL, Usage: "TTL (s) for service registrations"},
				cli.BoolFlag{Name: "loop", Usage: "Continuously register containers"},
			},
		},
		{
			Name:        "unregister",
			Usage:       "discovers and unregisters running containers",
			Action:      unregister,
			Description: "unregister [options]",
		},
		{
			Name:        "status",
			Usage:       "Lists the registration status of running containers",
			Action:      status,
			Description: "status",
		},
	}

	app.Run(os.Args)
}
