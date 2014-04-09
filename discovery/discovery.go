package main

import (
	"github.com/codegangsta/cli"
	"github.com/fsouza/go-dockerclient"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/utils"
	"os"
)

const (
	ETCD_ENTRY_ALREADY_EXISTS = 105
)

var (
	client          *docker.Client
	serviceRegistry *registry.ServiceRegistry
	outputBuffer    *utils.OutputBuffer
)

func initOrDie(c *cli.Context) {
	var err error
	endpoint := "unix:///var/run/docker.sock"
	client, err = docker.NewClient(endpoint)

	if err != nil {
		panic(err)
	}

	serviceRegistry = &registry.ServiceRegistry{
		Env:          c.GlobalString("env"),
		Pool:         c.GlobalString("pool"),
		HostIP:       c.GlobalString("hostIp"),
		TTL:          uint64(c.Int("ttl")),
		HostSSHAddr:  c.GlobalString("sshAddr"),
		OutputBuffer: outputBuffer,
	}

	serviceRegistry.Connect(c.GlobalString("redis"))

	outputBuffer = &utils.OutputBuffer{}
}

func main() {

	app := cli.NewApp()
	app.Name = "discovery"
	app.Usage = "discovery service registration"
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "redis", Value: "http://127.0.0.1:6379", Usage: "host:port[,host:port,..]"},
		cli.StringFlag{Name: "env", Value: "dev", Usage: "environment (dev, test, prod, etc.)"},
		cli.StringFlag{Name: "pool", Value: "web", Usage: "pool (web, worker, etc.)"},
		cli.StringFlag{Name: "hostIp", Value: "127.0.0.1", Usage: "hosts external IP"},
		cli.StringFlag{Name: "sshAddr", Value: "127.0.0.1:22", Usage: "hosts external ssh IP:port"},
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
