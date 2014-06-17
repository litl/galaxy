package main

import (
	"strings"
	"time"

	"github.com/codegangsta/cli"
	"github.com/fsouza/go-dockerclient"
	"github.com/litl/galaxy/log"
	"github.com/ryanuber/columnize"
)

func register(c *cli.Context) {

	initOrDie(c)
	var lastLogged int64

	for {

		containers, err := client.ListContainers(docker.ListContainersOptions{
			All: false,
		})
		if err != nil {
			panic(err)
		}

		outputBuffer.Log(strings.Join([]string{
			"CONTAINER ID", "IMAGE",
			"EXTERNAL", "INTERNAL", "CREATED", "EXPIRES",
		}, " | "))

		serviceConfigs, err := serviceRegistry.ListApps("")
		if err != nil {
			log.Errorf("ERROR: Could not retrieve service configs for /%s/%s: %s\n", c.GlobalString("env"),
				c.GlobalString("pool"), err)
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
					log.Printf("Registered %s as %s at %s", dockerContainer.ID[0:12], serviceConfig.Name,
						registration.ExternalAddr())
					registered = true

				}
			}
		}

		if registered {
			lastLogged = time.Now().UnixNano()
		}
		if !c.Bool("loop") {
			break
		}
		time.Sleep(10 * time.Second)

	}

	result, _ := columnize.SimpleFormat(outputBuffer.Output)
	log.Println(result)

}
