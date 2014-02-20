package main

import (
	"fmt"
	"github.com/jwilder/go-dockerclient"
	"github.com/litl/galaxy/commander/auth"
	"github.com/litl/galaxy/discovery/registry"
	"github.com/mitchellh/cli"
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
}

func main() {

	initOrDie()

	cli := &cli.CLI{
		Args:     os.Args[1:],
		Commands: Commands,
	}

	exitCode, err := cli.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error executing CLI: %s\n", err.Error())
		os.Exit(1)
		return
	}
	os.Exit(exitCode)
}
