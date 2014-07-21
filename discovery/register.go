package main

import (
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/codegangsta/cli"
	docker "github.com/fsouza/go-dockerclient"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/utils"
	"github.com/ryanuber/columnize"
)

func unregisterAll(c *cli.Context, signals chan os.Signal) {
	// Block until a signal is received.
	<-signals
	unregisterShuttle(c)
	serviceRuntime.UnRegisterAll()
	os.Exit(0)
}

func register(c *cli.Context) {

	initOrDie(c)
	var lastLogged int64

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, os.Kill)
	go unregisterAll(c, signals)

	for {

		containers, err := client.ListContainers(docker.ListContainersOptions{
			All: false,
		})
		if err != nil {
			log.Errorf("ERROR: Could not list containers: %s", err)
		}

		outputBuffer.Log(strings.Join([]string{
			"CONTAINER ID", "IMAGE",
			"EXTERNAL", "INTERNAL", "CREATED", "EXPIRES",
		}, " | "))

		serviceConfigs, err := serviceRegistry.ListApps("")
		if err != nil {
			log.Errorf("ERROR: Could not retrieve service configs for /%s/%s: %s\n", utils.GalaxyEnv(c),
				utils.GalaxyPool(c), err)
		}

		registered := false
		for _, serviceConfig := range serviceConfigs {
			for _, container := range containers {
				dockerContainer, err := client.InspectContainer(container.ID)

				if err != nil {
					log.Printf("ERROR: Unable to inspect container %s: %s. Skipping.\n", container.ID, err)
					continue
				}

				if !serviceConfig.IsContainerVersion(strings.TrimPrefix(dockerContainer.Name, "/")) {
					continue
				}

				registration, err := serviceRegistry.RegisterService(dockerContainer, &serviceConfig)
				if err != nil {
					log.Printf("ERROR: Could not register %s: %s\n",
						serviceConfig.Name, err)
					continue
				}

				if lastLogged == 0 || time.Now().UnixNano()-lastLogged > (60*time.Second).Nanoseconds() {
					location := registration.ExternalAddr()
					if location != "" {
						location = " at " + location
					}
					log.Printf("Registered %s running as %s for %s%s", strings.TrimPrefix(dockerContainer.Name, "/"),
						dockerContainer.ID[0:12], serviceConfig.Name, location)
					registered = true

				}
			}
		}

		if registered {
			lastLogged = time.Now().UnixNano()
		}

		registerShuttle(c)

		if !c.Bool("loop") {
			break
		}
		time.Sleep(10 * time.Second)

	}

	result, _ := columnize.SimpleFormat(outputBuffer.Output)
	log.Println(result)

}
