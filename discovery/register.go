package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/codegangsta/cli"
	"github.com/fsouza/go-dockerclient"
	"github.com/litl/galaxy/log"
	"github.com/ryanuber/columnize"
)

//FIXME: Make shuttle importable so we can re-use existing structs
type Backend struct {
	Name string `json:"name"`
	Addr string `json:"address"`
}

type Service struct {
	Name         string     `json:"name"`
	Addr         string     `json:"address"`
	VirtualHosts []string   `json:"virtual_hosts"`
	Backends     []*Backend `json:"backends"`
}

func registerShuttle() {

	registrations, err := serviceRegistry.ListRegistrations()
	if err != nil {
		log.Errorf("ERROR: Unable to list registrations: %s", err)
		return
	}

	backends := make(map[string]*Service)

	for _, r := range registrations {

		if r.Port == "" && len(r.VirtualHosts) == 0 {
			continue
		}
		service := backends[r.Name]
		if service == nil {
			service = &Service{
				Name:         r.Name,
				VirtualHosts: r.VirtualHosts,
			}
			if r.Port != "" {
				service.Addr = "127.0.0.1:" + r.Port
			}
			backends[r.Name] = service
		}
		b := &Backend{
			Name: r.ContainerID[0:12],
			Addr: r.ExternalAddr(),
		}
		service.Backends = append(service.Backends, b)
	}

	for k, service := range backends {

		js, err := json.Marshal(service)
		if err != nil {
			log.Printf("ERROR: Marshaling service to JSON: %s", err)
			continue
		}
		resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:9090/%s", k), "application/jsoN",
			bytes.NewBuffer(js))
		if err != nil {
			log.Errorf("ERROR: Registerring backend with shuttle: %s", err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			log.Errorf("ERROR: Failed to register service with shuttle: %s", resp.Status)
		}
		resp.Body.Close()
	}

}

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

		registerShuttle()

		if !c.Bool("loop") {
			break
		}
		time.Sleep(10 * time.Second)

	}

	result, _ := columnize.SimpleFormat(outputBuffer.Output)
	log.Println(result)

}
