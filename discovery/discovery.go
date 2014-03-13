package main

import (
	"github.com/codegangsta/cli"
	"github.com/fsouza/go-dockerclient"
	"github.com/litl/galaxy/commander/auth"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/utils"
	"os"
	"os/user"
)

const (
	ETCD_ENTRY_ALREADY_EXISTS = 105
)

var (
	client          *docker.Client
	authConfig      *auth.ConfigFile
	hostname        string
	serviceRegistry *registry.ServiceRegistry
	outputBuffer    *utils.OutputBuffer
)

func initOrDie() {
	var err error
	endpoint := "unix:///var/run/docker.sock"
	client, err = docker.NewClient(endpoint)

	if err != nil {
		panic(err)
	}

	currentUser, err := user.Current()
	if err != nil {
		panic(err)
	}

	hostname, err = os.Hostname()
	if err != nil {
		panic(err)
	}

	// use ~/.dockercfg
	authConfig, err = auth.LoadConfig(currentUser.HomeDir)
	if err != nil {
		panic(err)
	}
	outputBuffer = &utils.OutputBuffer{}
}

func main() {

	initOrDie()

	app := cli.NewApp()
	app.Name = "discovery"
	app.Usage = "discovery service registration"
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "etcd", Value: "http://127.0.0.1:4001", Usage: "host:port[,host:port,..]"},
		cli.StringFlag{Name: "env", Value: "dev", Usage: "environment (dev, test, prod, etc.)"},
		cli.StringFlag{Name: "pool", Value: "web", Usage: "pool (web, worker, etc.)"},
		cli.StringFlag{Name: "hostIp", Value: "127.0.0.1", Usage: "hosts external IP"},
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
