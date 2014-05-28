package main

import (
	"strings"
	"time"

	"github.com/codegangsta/cli"
	"github.com/fsouza/go-dockerclient"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/utils"
	"github.com/ryanuber/columnize"
)

func status(c *cli.Context) {

	initOrDie(c)

	containers, err := client.ListContainers(docker.ListContainersOptions{
		All: false,
	})
	if err != nil {
		panic(err)
	}

	outputBuffer.Log(strings.Join([]string{
		"CONTAINER ID", "REGISTRATION", "IMAGE",
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

			registered, err := serviceRegistry.GetServiceRegistration(dockerContainer, &serviceConfig)
			if err != nil {
				log.Printf("ERROR: Unable to determine status of %s: %s\n",
					serviceConfig.Name, err)
				return
			}

			if registered != nil {
				outputBuffer.Log(strings.Join([]string{
					container.ID[0:12],
					registered.Path,
					container.Image,
					registered.ExternalAddr(),
					registered.InternalAddr(),
					utils.HumanDuration(time.Now().Sub(time.Unix(container.Created, 0))) + " ago",
					"In " + utils.HumanDuration(registered.Expires.Sub(time.Now())),
				}, " | "))
			} else {
				outputBuffer.Log(strings.Join([]string{
					container.ID[0:12],
					"",
					container.Image,
					"",
					"",
					utils.HumanDuration(time.Now().Sub(time.Unix(container.Created, 0))) + " ago",
					"",
				}, " | "))
			}

		}
	}

	result, _ := columnize.SimpleFormat(outputBuffer.Output)
	log.Println(result)
}
