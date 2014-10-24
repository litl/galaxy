package main

import (
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/codegangsta/cli"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/runtime"
	"github.com/litl/galaxy/utils"
	"github.com/ryanuber/columnize"
)

var (
	httpClient *http.Client
)

func unregisterAll(c *cli.Context, signals chan os.Signal) {
	// Block until a signal is received.
	<-signals
	unregisterShuttle(c)
	serviceRuntime.UnRegisterAll(utils.GalaxyEnv(c))
	os.Exit(0)
}

func registerAll(c *cli.Context, loggedOnce bool) {
	outputBuffer.Log(strings.Join([]string{
		"CONTAINER ID", "IMAGE",
		"EXTERNAL", "INTERNAL", "CREATED", "EXPIRES",
	}, " | "))

	registrations, err := serviceRuntime.RegisterAll(utils.GalaxyEnv(c))
	if err != nil {
		log.Errorf("ERROR: Unable to register containers: %s", err)
		return
	}

	fn := log.Debugf
	if !loggedOnce {
		fn = log.Printf
	}

	for _, registration := range registrations {
		if !loggedOnce || time.Now().Unix()%60 < 10 {
			fn("Registered %s running as %s for %s%s", strings.TrimPrefix(registration.ContainerName, "/"),
				registration.ContainerID[0:12], registration.Name, locationAt(registration))
		}

		statusLine := strings.Join([]string{
			registration.ContainerID[0:12],
			registration.Image,
			registration.ExternalAddr(),
			registration.InternalAddr(),
			utils.HumanDuration(time.Now().Sub(registration.StartedAt)) + " ago",
			"In " + utils.HumanDuration(registration.Expires.Sub(time.Now().UTC())),
		}, " | ")

		outputBuffer.Log(statusLine)
	}

	registerShuttle(c)
}

func register(c *cli.Context) {

	initOrDie(c)

	transport := &http.Transport{ResponseHeaderTimeout: 2 * time.Second}
	httpClient = &http.Client{Transport: transport}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, os.Kill, syscall.SIGTERM)
	go unregisterAll(c, signals)

	registerAll(c, false)

	if !c.Bool("loop") {
		result, _ := columnize.SimpleFormat(outputBuffer.Output)
		log.Println(result)
		return
	}

	containerEvents := make(chan runtime.ContainerEvent)
	err := serviceRuntime.RegisterEvents(utils.GalaxyEnv(c), containerEvents)
	if err != nil {
		log.Printf("ERROR: Unable to register docker event listener: %s", err)
	}

	for {

		select {
		case ce := <-containerEvents:
			switch ce.Status {
			case "start":
				reg, err := serviceRegistry.RegisterService(utils.GalaxyEnv(c), ce.Container, ce.ServiceConfig)
				if err != nil {
					log.Errorf("ERROR: Unable to register container: %s", err)
					continue
				}

				log.Printf("Registered %s running as %s for %s%s", strings.TrimPrefix(reg.ContainerName, "/"),
					reg.ContainerID[0:12], reg.Name, locationAt(reg))
				registerShuttle(c)
			case "die", "stop":
				reg, err := serviceRegistry.UnRegisterService(utils.GalaxyEnv(c), ce.Container, ce.ServiceConfig)
				if err != nil {
					log.Errorf("ERROR: Unable to unregister container: %s", err)
					continue
				}

				if reg != nil {
					log.Printf("Unregistered %s running as %s for %s%s", strings.TrimPrefix(reg.ContainerName, "/"),
						reg.ContainerID[0:12], reg.Name, locationAt(reg))
				}
				pruneShuttleBackends(c)
			}

		case <-time.After(10 * time.Second):
			registerAll(c, true)
			pruneShuttleBackends(c)
		}
	}
}

func locationAt(reg *registry.ServiceRegistration) string {
	location := reg.ExternalAddr()
	if location != "" {
		location = " at " + location
	}
	return location
}
