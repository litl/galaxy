package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/codegangsta/cli"
	"github.com/litl/galaxy/log"
	shuttle "github.com/litl/galaxy/shuttle/client"
	"github.com/litl/galaxy/utils"
)

func getShuttleConfig(c *cli.Context) (*shuttle.Config, error) {

	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/_config", c.GlobalString("shuttleAddr")), nil)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	config := &shuttle.Config{}
	err = json.Unmarshal(body, config)
	if err != nil {
		return nil, err
	}

	return config, nil

}
func pruneShuttleBackends(c *cli.Context) {
	config, err := getShuttleConfig(c)
	if err != nil {
		log.Errorf("ERROR: Unable to get shuttle config: %s", err)
		return
	}

	registrations, err := serviceRegistry.ListRegistrations(utils.GalaxyEnv(c))
	if err != nil {
		log.Errorf("ERROR: Unable to list registrations: %s", err)
		return
	}

	for _, service := range config.Services {

		// Remove services that no longer exist
		svcCfg, err := serviceRegistry.GetServiceConfig(service.Name, utils.GalaxyEnv(c))
		if err != nil {
			log.Errorf("ERROR: Unable to get service config for %s: %s", service.Name, err)
			return
		}

		if svcCfg == nil {
			err := unregisterShuttleService(c, &service)
			if err != nil {
				log.Errorf("ERROR: Unable to remove service %s from shuttle: %s", service.Name, err)
			}
			log.Printf("Unregisterred shuttle service %s", service.Name)
			continue
		}

		for _, backend := range service.Backends {
			backendExists := false
			for _, r := range registrations {
				if backend.Name == r.ContainerID[0:12] {
					backendExists = true
					break
				}
			}

			if !backendExists {
				err := unregisterShuttleBackend(c, service.Name, backend.Name)
				if err != nil {
					log.Errorf("ERROR: Unable to remove backend %s from shuttle: %s", backend.Name, err)
				}
				log.Printf("Unregisterred shuttle backend %s", backend.Name)
			}

		}

	}
}

func registerShuttle(c *cli.Context) {

	registrations, err := serviceRegistry.ListRegistrations(utils.GalaxyEnv(c))
	if err != nil {
		log.Errorf("ERROR: Unable to list registrations: %s", err)
		return
	}

	backends := make(map[string]*shuttle.ServiceConfig)

	for _, r := range registrations {

		// No service ports exposed on the host, skip it.
		if r.ExternalAddr() == "" || r.Port == "" {
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
			Name:      r.ContainerID[0:12],
			Addr:      r.ExternalAddr(),
			CheckAddr: r.ExternalAddr(),
		}
		service.Backends = append(service.Backends, b)

		// lookup the VIRTUAL_HOST_%d environment variables and load them into the ServiceConfig
		errorPages := make(map[string][]int)
		for vhostCode, url := range r.ErrorPages {
			code := 0
			n, err := fmt.Sscanf(vhostCode, "VIRTUAL_HOST_%d", &code)
			if err != nil || n == 0 {
				continue
			}

			errorPages[url] = append(errorPages[url], code)
		}

		if len(errorPages) > 0 {
			service.ErrorPages = errorPages
		}
	}

	for k, service := range backends {

		js, err := json.Marshal(service)
		if err != nil {
			log.Printf("ERROR: Marshaling service to JSON: %s", err)
			continue
		}

		resp, err := httpClient.Post(fmt.Sprintf("http://%s/%s", c.GlobalString("shuttleAddr"), k), "application/json",
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

func unregisterShuttle(c *cli.Context) {

	registrations, err := serviceRegistry.ListRegistrations(utils.GalaxyEnv(c))
	if err != nil {
		log.Errorf("ERROR: Unable to list registrations: %s", err)
		return
	}

	backends := make(map[string]*shuttle.ServiceConfig)

	for _, r := range registrations {

		// Registration for a container on a different host? Skip it.
		if r.ExternalIP != serviceRegistry.HostIP {
			continue
		}

		// No service ports exposed on the host, skip it.
		if r.ExternalAddr() == "" || r.Port == "" {
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

	for _, service := range backends {

		err := unregisterShuttleService(c, service)
		if err != nil {
			log.Errorf("ERROR: Unable to remove shuttle service: %s", err)
		}
	}

}

func unregisterShuttleService(c *cli.Context, service *shuttle.ServiceConfig) error {
	js, err := json.Marshal(service)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("DELETE", fmt.Sprintf("http://%s/%s", c.GlobalString("shuttleAddr"), service.Name), bytes.NewBuffer(js))
	if err != nil {
		return err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New(fmt.Sprintf("failed to unregister service: %s", resp.Status))
	}
	return nil
}

func unregisterShuttleBackend(c *cli.Context, service, backend string) error {
	req, err := http.NewRequest("DELETE", fmt.Sprintf("http://%s/%s/%s", c.GlobalString("shuttleAddr"), service, backend), nil)
	if err != nil {
		return err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New(fmt.Sprintf("failed to unregister backend: %s", resp.Status))
	}
	return nil
}
