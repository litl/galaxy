package discovery

import (
	"fmt"

	"github.com/litl/galaxy/config"
	"github.com/litl/galaxy/log"

	shuttle "github.com/litl/shuttle/client"
)

var (
	client *shuttle.Client
)

func registerShuttle(serviceRegistry *config.ServiceRegistry, env, shuttleAddr string) {
	if client == nil {
		return
	}

	registrations, err := serviceRegistry.ListRegistrations(env)
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

	for _, service := range backends {
		err := client.UpdateService(service)
		if err != nil {
			log.Errorf("ERROR: Unable to register shuttle service: %s", err)
		}
	}

}

func unregisterShuttle(serviceRegistry *config.ServiceRegistry, env, hostIP, shuttleAddr string) {

	if client == nil {
		return
	}

	registrations, err := serviceRegistry.ListRegistrations(env)
	if err != nil {
		log.Errorf("ERROR: Unable to list registrations: %s", err)
		return
	}

	backends := make(map[string]*shuttle.ServiceConfig)

	for _, r := range registrations {

		// Registration for a container on a different host? Skip it.
		if r.ExternalIP != hostIP {
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

		err := client.RemoveService(service.Name)
		if err != nil {
			log.Errorf("ERROR: Unable to remove shuttle service: %s", err)
		}
	}

}

func pruneShuttleBackends(configStore *config.Store, serviceRegistry *config.ServiceRegistry, env, shuttleAddr string) {
	if client == nil {
		return
	}

	config, err := client.GetConfig()
	if err != nil {
		log.Errorf("ERROR: Unable to get shuttle config: %s", err)
		return
	}

	registrations, err := serviceRegistry.ListRegistrations(env)
	if err != nil {
		log.Errorf("ERROR: Unable to list registrations: %s", err)
		return
	}

	if len(registrations) == 0 {
		// If there are no registrations, skip pruning it because we might be in a bad state and
		// don't want to inadvertently unregister everything.  Shuttle will handle the down
		// nodes if they are really down.
		return
	}

	for _, service := range config.Services {

		app, err := configStore.GetApp(service.Name, env)
		if err != nil {
			log.Errorf("ERROR: Unable to load app %s: %s", app, err)
			continue
		}

		pools, err := configStore.ListAssignedPools(env, service.Name)
		if err != nil {
			log.Errorf("ERROR: Unable to list pool assignments for %s: %s", service.Name, err)
			continue
		}

		if app == nil || len(pools) == 0 {
			err := client.RemoveService(service.Name)
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
				err := client.RemoveBackend(service.Name, backend.Name)
				if err != nil {
					log.Errorf("ERROR: Unable to remove backend %s from shuttle: %s", backend.Name, err)
				}
				log.Printf("Unregisterred shuttle backend %s", backend.Name)
			}
		}
	}
}
