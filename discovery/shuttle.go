package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/codegangsta/cli"
	"github.com/litl/galaxy/log"
	shuttle "github.com/litl/galaxy/shuttle/client"
)

func registerShuttle(c *cli.Context) {

	registrations, err := serviceRegistry.ListRegistrations()
	if err != nil {
		log.Errorf("ERROR: Unable to list registrations: %s", err)
		return
	}

	backends := make(map[string]*shuttle.ServiceConfig)

	for _, r := range registrations {

		// No service ports exposed on the host, skip it.
		if r.ExternalAddr() == "" {
			continue
		}

		// No listening port or virtual hosts configured, skip it.
		if r.Port == "" && len(r.VirtualHosts) == 0 {
			continue
		}
		service := backends[r.Name]
		if service == nil {
			service = &shuttle.ServiceConfig{
				Name:         r.Name,
				VirtualHosts: r.VirtualHosts,
			}
			if r.Port != "" {
				service.Addr = "0.0.0.0:" + r.Port
			}
			backends[r.Name] = service
		}
		b := shuttle.BackendConfig{
			Name: r.ContainerID[0:12],
			Addr: r.ExternalAddr(),
		}
		service.Backends = append(service.Backends, b)
	}

	transport := &http.Transport{ResponseHeaderTimeout: 2 * time.Second}
	httpClient := &http.Client{Transport: transport}

	for k, service := range backends {

		js, err := json.Marshal(service)
		if err != nil {
			log.Printf("ERROR: Marshaling service to JSON: %s", err)
			continue
		}

		resp, err := httpClient.Post(fmt.Sprintf("http://%s/%s", c.GlobalString("shuttleAddr"), k), "application/jsoN",
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
