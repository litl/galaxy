package config

import (
	"fmt"
	"sort"
	"time"

	"github.com/fsouza/go-dockerclient"
)

func newServiceRegistration(container *docker.Container, hostIP, galaxyPort string) *ServiceRegistration {
	//FIXME: We're using the first found port and assuming it's tcp.
	//How should we handle a service that exposes multiple ports
	//as well as tcp vs udp ports.
	var externalPort, internalPort string

	// sort the port bindings by internal port number so multiple ports are assigned deterministically
	// (docker.Port is a string with a Port method)
	cPorts := container.NetworkSettings.Ports
	allPorts := []string{}
	for p, _ := range cPorts {
		allPorts = append(allPorts, string(p))
	}
	sort.Strings(allPorts)

	for _, k := range allPorts {
		v := cPorts[docker.Port(k)]
		if len(v) > 0 {
			externalPort = v[0].HostPort
			internalPort = docker.Port(k).Port()
			// Look for a match to GALAXY_PORT if we have multiple ports to
			// choose from. (don't require this, or we may break existing services)
			if len(allPorts) > 1 && internalPort == galaxyPort {
				break
			}
		}
	}

	serviceRegistration := ServiceRegistration{
		ContainerName: container.Name,
		ContainerID:   container.ID,
		StartedAt:     container.Created,
		Image:         container.Config.Image,
		Port:          galaxyPort,
	}

	if externalPort != "" && internalPort != "" {
		serviceRegistration.ExternalIP = hostIP
		serviceRegistration.InternalIP = container.NetworkSettings.IPAddress
		serviceRegistration.ExternalPort = externalPort
		serviceRegistration.InternalPort = internalPort
	}
	return &serviceRegistration
}

type ServiceRegistration struct {
	Name          string            `json:"NAME,omitempty"`
	ExternalIP    string            `json:"EXTERNAL_IP,omitempty"`
	ExternalPort  string            `json:"EXTERNAL_PORT,omitempty"`
	InternalIP    string            `json:"INTERNAL_IP,omitempty"`
	InternalPort  string            `json:"INTERNAL_PORT,omitempty"`
	ContainerID   string            `json:"CONTAINER_ID"`
	ContainerName string            `json:"CONTAINER_NAME"`
	Image         string            `json:"IMAGE,omitempty"`
	ImageId       string            `json:"IMAGE_ID,omitempty"`
	StartedAt     time.Time         `json:"STARTED_AT"`
	Expires       time.Time         `json:"-"`
	Path          string            `json:"-"`
	VirtualHosts  []string          `json:"VIRTUAL_HOSTS"`
	Port          string            `json:"PORT"`
	ErrorPages    map[string]string `json:"ERROR_PAGES,omitempty"`
	// pool is inserted only for commander dump and restore
	Pool string
}

func (s *ServiceRegistration) Equals(other ServiceRegistration) bool {
	return s.ExternalIP == other.ExternalIP &&
		s.ExternalPort == other.ExternalPort &&
		s.InternalIP == other.InternalIP &&
		s.InternalPort == other.InternalPort
}

func (s *ServiceRegistration) addr(ip, port string) string {
	if ip != "" && port != "" {
		return fmt.Sprint(ip, ":", port)
	}
	return ""

}
func (s *ServiceRegistration) ExternalAddr() string {
	return s.addr(s.ExternalIP, s.ExternalPort)
}

func (s *ServiceRegistration) InternalAddr() string {
	return s.addr(s.InternalIP, s.InternalPort)
}
