package main

import (
	"fmt"
	"github.com/codegangsta/cli"
	"github.com/fsouza/go-dockerclient"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/utils"
	"github.com/ryanuber/columnize"
	"strings"
	"time"
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

	for _, container := range containers {
		dockerContainer, err := client.InspectContainer(container.ID)
		if err != nil {
			fmt.Printf("ERROR: Unable to inspect container %s: %s. Skipping.\n", container.ID, err)
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

		existingConfig, err := serviceRegistry.GetServiceConfig(repository)
		if err != nil {
			fmt.Printf("ERROR: Unable to determine if app %s exists: %s. Skipping.\n", repository, err)
			continue
		}
		if existingConfig == nil {
			// container isn't a galaxy app. skip it.
			continue
		}

		registered, err := serviceRegistry.GetServiceRegistration(dockerContainer, serviceConfig)
		if err != nil {
			fmt.Printf("ERROR: Unable to determine status of %s: %s\n",
				serviceConfig.Name, err)
			return
		}

		if registered != nil {
			outputBuffer.Log(strings.Join([]string{
				container.ID[0:12],
				registered.Path,
				container.Image,
				registered.ExternalIP + ":" + registered.ExternalPort,
				registered.InternalIP + ":" + registered.InternalPort,
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

	result, _ := columnize.SimpleFormat(outputBuffer.Output)
	fmt.Println(result)
}
