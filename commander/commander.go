package main

import (
	"flag"
	"fmt"
	"github.com/jwilder/go-dockerclient"
	"github.com/litl/galaxy/commander/auth"
	"os"
	"os/user"
	"strings"
	"time"
)

var (
	client     *docker.Client
	stopCutoff = flag.Int64("cutoff", 5*60, "Seconds to wait before stopping old containers")
	image      = flag.String("t", "", "Image to start")
	authConfig *auth.ConfigFile
)

func splitDockerImage(img string) (string, string, string) {
	if !strings.Contains(img, "/") {
		return "", img, ""
	}
	parts := strings.Split(img, "/")

	if !strings.Contains(parts[1], ":") {
		return parts[0], parts[0] + "/" + parts[1], ""
	}

	imageParts := strings.Split(parts[1], ":")
	// registry, repository, tag
	return parts[0], parts[0] + "/" + imageParts[0], imageParts[1]
}

func isRunning(img string) (string, error) {

	image, err := client.InspectImage(img)
	if err != nil {
		return "", err
	}

	containers, err := client.ListContainers(docker.ListContainersOptions{
		All: false,
	})
	if err != nil {
		return "", err
	}

	for _, container := range containers {
		dockerContainer, err := client.InspectContainer(container.ID)
		if err != nil {
			return "", err
		}

		if image.ID == dockerContainer.Image {
			return container.ID, nil
		}
	}
	return "", nil
}

func startIfNotRunning(img string) (*docker.Container, error) {
	containerId, err := isRunning(img)
	if err != nil && err != docker.ErrNoSuchImage {
		return nil, err
	}

	// already running, grab the container details
	if containerId != "" {
		return client.InspectContainer(containerId)
	}

	registry, repository, _ := splitDockerImage(img)

	// see if we have the image locally
	_, err = client.InspectImage(img)

	if err == docker.ErrNoSuchImage {
		// No, pull it down locally
		pullOpts := docker.PullImageOptions{
			Repository:   repository,
			Registry:     registry,
			OutputStream: os.Stdout}

		// use .dockercfg if available
		auth := docker.AuthConfiguration{}
		if registry != "" {
			pullOpts.Registry = registry
			authCreds := authConfig.ResolveAuthConfig(registry)

			auth.Username = authCreds.Username
			auth.Password = authCreds.Password
			auth.Email = authCreds.Email

		}

		err = client.PullImage(pullOpts, auth)

		if err != nil {
			return nil, err
		}
	}

	container, err := client.CreateContainer(docker.CreateContainerOptions{
		Config: &docker.Config{
			Image: img,
		},
	})
	if err != nil {
		return nil, err
	}
	err = client.StartContainer(container.ID,
		&docker.HostConfig{
			PublishAllPorts: true,
		})
	return container, err

}

func getImageByName(img string) (*docker.APIImages, error) {
	imgs, err := client.ListImages(true)
	if err != nil {
		panic(err)
	}

	for _, image := range imgs {
		if stringInSlice(img, image.RepoTags) {
			return &image, nil
		}
	}
	return nil, nil

}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func stopAllButLatest(img string, latest *docker.Container) error {
	imageParts := strings.Split(img, ":")
	repository := imageParts[0]

	containers, err := client.ListContainers(docker.ListContainersOptions{
		All:    false,
		Before: latest.ID,
	})
	if err != nil {
		return err
	}
	for _, container := range containers {

		if strings.HasPrefix(container.Image, repository) && container.ID != latest.ID &&
			container.Created < (time.Now().Unix()-*stopCutoff) {
			err := client.StopContainer(container.ID, 10)
			if err != nil {
				fmt.Printf("ERROR: Unable to stop container: %s\n", container.ID)
			}
			client.RemoveContainer(docker.RemoveContainerOptions{
				ID:            container.ID,
				RemoveVolumes: true,
			})
		}
	}
	return nil

}

func main() {
	flag.Parse()

	if *image == "" {
		fmt.Println("Need an image to start")
		flag.PrintDefaults()
		os.Exit(1)
	}

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

	// use ~/.dockercfg
	authConfig, err = auth.LoadConfig(currentUser.HomeDir)
	if err != nil {
		panic(err)
	}

	container, err := startIfNotRunning(*image)
	if err != nil {
		fmt.Printf("ERROR: Could not determine if %s is running: %s\n", *image, err)
		os.Exit(1)
	}

	fmt.Printf("%s running as %s\n", *image, container.ID)

	stopAllButLatest(*image, container)
}
