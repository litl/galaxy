package main

import (
	"os"

	"github.com/codegangsta/cli"
	"github.com/fsouza/go-dockerclient"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/utils"
)

const (
	ETCD_ENTRY_ALREADY_EXISTS = 105
)

var (
	client          *docker.Client
	serviceRegistry *registry.ServiceRegistry
	outputBuffer    *utils.OutputBuffer
	buildVersion    string
)

func initOrDie(c *cli.Context) {
	var err error
	endpoint := "unix:///var/run/docker.sock"
	client, err = docker.NewClient(endpoint)

	if err != nil {
		panic(err)
	}

	serviceRegistry = registry.NewServiceRegistry(
		c.GlobalString("env"),
		c.GlobalString("pool"),
		c.GlobalString("hostIp"),
		uint64(c.Int("ttl")),
		c.GlobalString("sshAddr"),
	)

	serviceRegistry.Connect(c.GlobalString("redis"))

	outputBuffer = &utils.OutputBuffer{}
	serviceRegistry.OutputBuffer = outputBuffer

	// Don't log timestamps, etc. if running interactively
	if !c.Bool("loop") {
		log.DefaultLogger.SetFlags(0)
	}
}

func main() {

	app := cli.NewApp()
	app.Name = "discovery"
	app.Usage = "discovery service registration"
	app.Version = buildVersion
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "redis", Value: utils.GetEnv("GALAXY_REDIS_HOST", "127.0.0.1:6379"), Usage: "host:port[,host:port,..]"},
		cli.StringFlag{Name: "env", Value: utils.GetEnv("GALAXY_ENV", "dev"), Usage: "environment (dev, test, prod, etc.)"},
		cli.StringFlag{Name: "pool", Value: utils.GetEnv("GALAXY_POOL", "web"), Usage: "pool (web, worker, etc.)"},
		cli.StringFlag{Name: "hostIp", Value: "127.0.0.1", Usage: "hosts external IP"},
		cli.StringFlag{Name: "sshAddr", Value: "127.0.0.1:22", Usage: "hosts external ssh IP:port"},
		cli.StringFlag{Name: "shuttleAddr", Value: "127.0.0.1:9090", Usage: "shuttle http address"},
	}

	app.Commands = []cli.Command{
		{
			Name:        "register",
			Usage:       "discovers and registers running containers",
			Action:      register,
			Description: "register [options]",
			Flags: []cli.Flag{
				cli.IntFlag{Name: "ttl", Value: 60, Usage: "TTL (s) for service registrations"},
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
