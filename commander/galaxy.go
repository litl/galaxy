package main

import (
	"fmt"
	"github.com/fsouza/go-dockerclient"
	"net"
)

func main() {
	endpoint := "unix:///var/run/docker.sock"
	client, err := docker.NewClient(endpoint)
	if err != nil {
		panic(err)
	}
	imgs, err := client.ListContainers(docker.ListContainersOptions{
		All: false,
	})
	if err != nil {
		panic(err)
	}
	for _, img := range imgs {
		container, err := client.InspectContainer(img.ID)
		if err != nil {
			panic(err)
		}

		fmt.Println(img.ID, img.Image, img.Created,
			container.NetworkSettings.IPAddress,
			img.Ports[0].PublicPort, img.Ports[0].PrivatePort)
	}

	addrs, err := net.InterfaceAddrs()
	for _, addr := range addrs {
		fmt.Println(addr.String())
	}
}
