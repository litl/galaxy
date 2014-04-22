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

func RunShuttle(shuttleAddr string) {
	shuttleAddr = "http://" + shuttleAddr
	log.Printf("Updating shuttle at %s", shuttleAddr)

	// give us some way to fail if shuttle gets locked up
	transport := &http.Transport{ResponseHeaderTimeout: 2 * time.Second}
	httpClient := &http.Client{Transport: transport}

	for {
		// These are declared up here due to the goto error handling.
		// We need a map to populate the services,
		// and a slice to serialize to a JSON array.
		svcMap := make(map[string]*ServiceConfig)
		var services []ServiceConfig
		var reqJSON []byte

		allRegs, err := serviceRegistry.ListRegistrations()
		if err != nil {
			log.Println("Error getting registrations:", err)
			goto SLEEP
		}

		for _, reg := range allRegs {
			pathParts := strings.Split(reg.Path, "/")
			if len(pathParts) < 5 {
				log.Println("Error, bad registration path: %s", pathParts)
				continue
			}

			hostName, svcName := pathParts[3], pathParts[4]

			service, ok := svcMap[svcName]
			if !ok {
				// TODO: make some of this configurable
				service = &ServiceConfig{
					Name:          svcName,
					Addr:          "127.0.0.1:" + reg.ExternalPort,
					Balance:       "RR",
					CheckInterval: 2,
					Fall:          2,
					Rise:          3,
					DialTimeout:   2,
				}
				svcMap[svcName] = service
				log.Debugf("updating shuttle service %+v", service)
			}

			backend := BackendConfig{
				Name: hostName,
				Addr: reg.ExternalIP + ":" + reg.ExternalPort,
			}
			backend.CheckAddr = backend.Addr
			log.Debugf("updating shuttle backend %+v", backend)

			service.Backends = append(service.Backends, backend)
		}

		// populate the slice for the JSON array
		for _, svc := range svcMap {
			services = append(services, *svc)
		}

		reqJSON, err = json.Marshal(services)
		if err != nil {
			log.Println("ERROR serializing services:", err)
			goto SLEEP
		}

		_, err = httpClient.Post(shuttleAddr, "application/json", bytes.NewReader(reqJSON))
		if err != nil {
			log.Println("ERROR updating shuttle config:", err)
		}

	SLEEP:
		// TODO: make update interval a flag or config option
		time.Sleep(5 * time.Second)
	}
}
