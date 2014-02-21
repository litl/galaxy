package command

import (
	"flag"
	"fmt"
	"github.com/coreos/go-etcd/etcd"
	"github.com/jwilder/go-dockerclient"
	"github.com/litl/galaxy/discovery/registry"
	"github.com/litl/galaxy/utils"
	"github.com/mitchellh/cli"
	"github.com/ryanuber/columnize"
	"os"
	"strings"
)

type UnregisterCommand struct {
	Ui              cli.Ui
	Client          *docker.Client
	EctdClient      *etcd.Client
	Hostname        string
	ServiceRegistry *registry.ServiceRegistry
	OutputBuffer    *utils.OutputBuffer
}

func (c *UnregisterCommand) Help() string {
	helpText := `
Usage: discovery register [options]

  Registers running containers.

Options:

  -etcd=http://127.0.0.1:4001[,..]   Etcd addresss
  -env=dev                           Host's environment
  -pool=web                          Host's deployment pool
  -hostIp=127.0.0.1                  Host's external IP
  -ttl=60                            TTL (s) for service registrations
  -continuous=false                  Continously registers containers
`
	return strings.TrimSpace(helpText)
}

func (c *UnregisterCommand) DiscoverContainers() {

	containers, err := c.Client.ListContainers(docker.ListContainersOptions{
		All: false,
	})
	if err != nil {
		panic(err)
	}

	c.OutputBuffer.Log(strings.Join([]string{
		"CONTAINER ID", "REGISTRATION", "IMAGE",
		"EXTERNAL", "INTERNAL", "CREATED", "EXPIRES",
	}, " | "))

	for _, container := range containers {
		dockerContainer, err := c.Client.InspectContainer(container.ID)
		if err != nil {
			c.Ui.Error(fmt.Sprintf("ERROR: Unable to inspect container %s: %s. Skipping.\n", container.ID, err))
			continue
		}

		_, repository, tag := utils.SplitDockerImage(dockerContainer.Config.Image)

		env := make(map[string]string)
		for _, entry := range dockerContainer.Config.Env {
			firstSeparator := strings.Index(entry, "=")
			key := entry[0:firstSeparator]
			value := entry[firstSeparator+1:]
			env[key] = value
		}

		serviceConfig := &registry.ServiceConfig{
			Name:    repository,
			Env:     env,
			Version: tag,
		}

		err = c.ServiceRegistry.UnRegisterService(dockerContainer, serviceConfig)
		if err != nil {
			c.Ui.Error(fmt.Sprintf("ERROR: Could not register service %s is running: %s\n",
				serviceConfig.Version, err))
			os.Exit(1)
		}

	}
}

func (c *UnregisterCommand) Run(args []string) int {

	var (
		etcdHosts string
		env       string
		pool      string
		hostIp    string
		ttl       int
	)

	cmdFlags := flag.NewFlagSet("discovery", flag.ContinueOnError)
	cmdFlags.Usage = func() { c.Ui.Output(c.Help()) }
	cmdFlags.StringVar(&etcdHosts, "etcd", "http://127.0.0.1:4001", "Comma-separated list of etcd hosts")
	cmdFlags.StringVar(&env, "env", "dev", "Environment namespace")
	cmdFlags.StringVar(&pool, "pool", "web", "Pool namespace")
	cmdFlags.StringVar(&hostIp, "hostIp", "127.0.0.1", "Hosts external IP")

	if err := cmdFlags.Parse(args); err != nil {
		return 1
	}

	args = cmdFlags.Args()
	if len(args) > 0 {
		c.Ui.Error("Too many command line arguments.")
		c.Ui.Error("")
		c.Ui.Error(c.Help())
		return 1
	}

	c.ServiceRegistry = &registry.ServiceRegistry{
		Client:       c.Client,
		EtcdHosts:    etcdHosts,
		Env:          env,
		Pool:         pool,
		HostIp:       hostIp,
		TTL:          uint64(ttl),
		Hostname:     c.Hostname,
		OutputBuffer: c.OutputBuffer,
	}

	c.DiscoverContainers()

	result, _ := columnize.SimpleFormat(c.OutputBuffer.Output)
	c.Ui.Output(result)
	return 0
}

func (c *UnregisterCommand) Synopsis() string {
	return "Discovers and unregisters running containers"
}
