package main

import (
	"os"
	"strings"

	"github.com/codegangsta/cli"
	"github.com/fsouza/go-dockerclient"
	"github.com/litl/galaxy/log"
	"github.com/ryanuber/columnize"
)

func unregister(c *cli.Context) {

	initOrDie(c)

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

			err = serviceRegistry.UnRegisterService(dockerContainer, &serviceConfig)
			if err != nil {
				log.Printf("ERROR: Could not unregister %s: %s\n",
					serviceConfig.Name, err)
				os.Exit(1)
			}
			log.Printf("Unregistered %s as %s", dockerContainer.ID[0:12], serviceConfig.Name)

		}
	}

	result, _ := columnize.SimpleFormat(outputBuffer.Output)
	log.Println(result)
}
