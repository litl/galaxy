package main

import (
	"os"
	"strings"
	"time"

	"github.com/codegangsta/cli"
	"github.com/fsouza/go-dockerclient"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/utils"
	"github.com/ryanuber/columnize"
)

func register(c *cli.Context) {

	initOrDie(c)

	for {

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
				log.Printf("ERROR: Unable to inspect container %s: %s. Skipping.\n", container.ID, err)
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

			serviceConfig := registry.NewServiceConfigWithEnv(repository, tag, env)

			if !serviceConfig.IsContainerVersion(strings.TrimPrefix(dockerContainer.Name, "/")) {
				continue
			}
			app := strings.TrimPrefix(strings.Split(dockerContainer.Name, "_")[0], "/")
			serviceConfig.Name = app

			existingConfig, err := serviceRegistry.GetServiceConfig(serviceConfig.Name)
			if err != nil {
				log.Printf("ERROR: Unable to determine if app %s exists: %s. Skipping.\n", serviceConfig.Name, err)
				continue
			}
			if existingConfig == nil {
				// container isn't a galaxy app. skip it.
				continue
			}

			err = serviceRegistry.RegisterService(dockerContainer, serviceConfig)
			if err != nil {
				log.Printf("ERROR: Could not register %s: %s\n",
					serviceConfig.Name, err)
				os.Exit(1)
			}
			log.Printf("Registered %s as %s", dockerContainer.ID[0:12], serviceConfig.Name)

		}

		if !c.Bool("loop") {
			break
		}
		time.Sleep(10 * time.Second)

	}

	result, _ := columnize.SimpleFormat(outputBuffer.Output)
	log.Println(result)

}
