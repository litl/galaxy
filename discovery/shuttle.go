package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/litl/galaxy/log"
)

type BackendConfig struct {
	Name      string `json:"name"`
	Addr      string `json:"address"`
	CheckAddr string `json:"check_address"`
	Weight    int    `json:"weight"`
}
type ServiceConfig struct {
	Name          string          `json:"name"`
	Addr          string          `json:"address"`
	Backends      []BackendConfig `json:"backends"`
	Balance       string          `json:"balance"`
	CheckInterval int             `json:"check_interval"`
	Fall          int             `json:"fall"`
	Rise          int             `json:"rise"`
	ClientTimeout int             `json:"client_timeout"`
	ServerTimeout int             `json:"server_timeout"`
	DialTimeout   int             `json:"connect_timeout"`
}

// build our shuttle config from the current state of registered services.
func buildConfig() (map[string]*ServiceConfig, error) {
	svcMap := make(map[string]*ServiceConfig)

	allRegs, err := serviceRegistry.ListRegistrations()
	if err != nil {
		log.Println("Error getting registrations:", err)
		return nil, err
	}

	// get all registered applications so we know what ports to listen on
	allApps, err := serviceRegistry.ListApps("*")
	if err != nil {
		log.Println("Error listing apps:", err)
		return nil, err
	}

	// Make a Service config to listen for every service in our env
	for _, cfg := range allApps {
		port := cfg.EnvGet("PORT")
		if port == "" {
			// assign a random port when missing
			// TODO: can we skip everything that has no port?
			port = "0"
		}

		service, ok := svcMap[cfg.Name]
		if !ok {
			// TODO: make some of this configurable
			service = &ServiceConfig{
				Name:          cfg.Name,
				Addr:          "127.0.0.1:" + port,
				CheckInterval: 2000,
				Fall:          2,
				Rise:          3,
				DialTimeout:   2000,
			}
			svcMap[cfg.Name] = service
			log.Debugf("updating shuttle service %+v", service)
		}
	}

	// Add the all the backends we can find to the services
	for _, reg := range allRegs {
		pathParts := strings.Split(reg.Path, "/")
		if len(pathParts) < 5 {
			log.Printf("Error, bad registration path: %s", pathParts)
			continue
		}

		hostName, svcName := pathParts[3], pathParts[4]

		service, ok := svcMap[svcName]
		if !ok {
			log.Printf("Found registration for unknown service %s:%s", service, reg)
			continue
		}

		backend := BackendConfig{
			Name: hostName,
			Addr: reg.ExternalIP + ":" + reg.ExternalPort,
		}
		backend.CheckAddr = backend.Addr
		log.Debugf("updating shuttle backend %+v", backend)

		service.Backends = append(service.Backends, backend)
	}

	return svcMap, nil
}

func RunShuttle(shuttleAddr string) {
	shuttleAddr = "http://" + shuttleAddr
	log.Printf("Updating shuttle at %s", shuttleAddr)

	// give us some way to fail if shuttle gets locked up
	transport := &http.Transport{ResponseHeaderTimeout: 2 * time.Second}
	httpClient := &http.Client{Transport: transport}

	for {
		svcMap, err := buildConfig()
		if err != nil {
			goto SLEEP
		}

		for name, service := range svcMap {
			reqJSON, err := json.Marshal(service)
			if err != nil {
				log.Println("ERROR serializing services:", err)
				continue
			}

			resp, err := httpClient.Post(shuttleAddr+"/"+name, "application/json", bytes.NewReader(reqJSON))
			if err != nil {
				log.Println("ERROR updating shuttle config:", err)
				continue
			}

			if resp.StatusCode != http.StatusOK {
				log.Printf("ERROR: recieved %s from shuttle", resp.Status)
			}
		}

	SLEEP:
		// TODO: make update interval a flag or config option
		time.Sleep(5 * time.Second)
	}
}
