package registry

import (
	"encoding/json"
	"fmt"
	"github.com/coreos/go-etcd/etcd"
	"github.com/fsouza/go-dockerclient"
	"github.com/litl/galaxy/utils"
	"os"
	"path"
	"strings"
	"time"
)

const (
	ETCD_ENTRY_ALREADY_EXISTS = 105
	ETCD_ENTRY_NOT_EXISTS     = 100
)

type ServiceConfig struct {
	Name    string
	Version string
	Env     map[string]string
}

type ServiceRegistry struct {
	ectdClient   *etcd.Client
	EtcdHosts    string
	Env          string
	Pool         string
	HostIp       string
	Hostname     string
	TTL          uint64
	HostSSHAddr  string
	OutputBuffer *utils.OutputBuffer
}

type ServiceRegistration struct {
	ExternalIp   string    `json:"EXTERNAL_IP"`
	ExternalPort string    `json:"EXTERNAL_PORT"`
	InternalIp   string    `json:"INTERNAL_IP"`
	InternalPort string    `json:"INTERNAL_PORT"`
	ContainerID  string    `json:"CONTAINER_ID"`
	StartedAt    time.Time `json:"STARTED_AT"`
	Expires      time.Time `json:"-"`
	Path         string    `json:"-"`
}

func (s *ServiceRegistration) Equals(other ServiceRegistration) bool {
	return s.ExternalIp == other.ExternalIp &&
		s.ExternalPort == other.ExternalPort &&
		s.InternalIp == other.InternalIp &&
		s.InternalPort == other.InternalPort
}

func (r *ServiceRegistry) setHostValue(service string, key string, value string) error {
	_, err := r.ensureEtcdClient().Set("/"+r.Env+"/"+r.Pool+"/hosts/"+r.ensureHostname()+"/"+
		service+"/"+key, value, 0)
	return err
}

func (r *ServiceRegistry) ensureHostname() string {
	if r.Hostname == "" {
		hostname, err := os.Hostname()
		if err != nil {
			panic(err)
		}
		r.Hostname = hostname

	}
	return r.Hostname
}

func (r *ServiceRegistry) ensureEtcdClient() *etcd.Client {
	if r.ectdClient == nil {
		if r.EtcdHosts == "" {
			panic("No etcd hosts configured")
		}
		machines := strings.Split(r.EtcdHosts, ",")
		r.ectdClient = etcd.NewClient(machines)
	}
	return r.ectdClient
}

func (r *ServiceRegistry) makeServiceRegistration(container *docker.Container) *ServiceRegistration {
	//FIXME: We're using the first found port and assuming it's tcp.
	//How should we handle a service that exposes multiple ports
	//as well as tcp vs udp ports.
	var externalPort, internalPort string
	for k, _ := range container.NetworkSettings.Ports {
		externalPort = k.Port()
		internalPort = externalPort
		break
	}

	serviceRegistration := ServiceRegistration{
		ExternalIp:   r.HostIp,
		ExternalPort: externalPort,
		InternalIp:   container.NetworkSettings.IPAddress,
		InternalPort: internalPort,
		ContainerID:  container.ID,
		StartedAt:    container.Created,
	}
	return &serviceRegistration
}

func (r *ServiceRegistry) GetServiceConfigs() []*ServiceConfig {
	var serviceConfigs []*ServiceConfig

	resp, err := r.ensureEtcdClient().Get("/"+r.Env+"/"+r.Pool, false, true)
	if err != nil {
		fmt.Printf("ERROR: Could not retrieve service config: %s\n", err)
		return serviceConfigs
	}
	for _, node := range resp.Node.Nodes {
		service := path.Base(node.Key)

		if service == "hosts" {
			continue
		}

		serviceConfig := &ServiceConfig{
			Name: service,
			Env:  make(map[string]string),
		}
		fmt.Printf("Detected service %s\n", service)

		for _, configKey := range node.Nodes {
			if strings.HasSuffix(configKey.Key, "/version") {
				serviceConfig.Version = configKey.Value
			} else if strings.HasSuffix(configKey.Key, "/environment") {
				err := json.Unmarshal([]byte(configKey.Value), &serviceConfig.Env)
				if err != nil {
					fmt.Printf("ERROR: Could not unmarshall config: %s\n", err)
					return serviceConfigs
				}
			} else {
				fmt.Printf("WARN: Unknown entry %s. Ignoring\n", configKey.Key)
			}
		}
		serviceConfigs = append(serviceConfigs, serviceConfig)
	}
	return serviceConfigs
}

func (r *ServiceRegistry) RegisterService(container *docker.Container, serviceConfig *ServiceConfig) error {

	_, err := r.ensureEtcdClient().CreateDir("/"+r.Env+"/"+r.Pool+"/hosts", 0)
	if err != nil && err.(*etcd.EtcdError).ErrorCode != ETCD_ENTRY_ALREADY_EXISTS {
		return err
	}

	hostPath := "/" + r.Env + "/" + r.Pool + "/hosts/" + r.ensureHostname() + "/ssh"
	_, err = r.ensureEtcdClient().Set(hostPath, r.HostSSHAddr, r.TTL)
	if err != nil {
		return err
	}

	registrationPath := "/" + r.Env + "/" + r.Pool + "/hosts/" + r.ensureHostname() + "/" + serviceConfig.Name
	registration, err := r.ensureEtcdClient().CreateDir(registrationPath, r.TTL)
	if err != nil {

		if err.(*etcd.EtcdError).ErrorCode != ETCD_ENTRY_ALREADY_EXISTS {
			return err
		}

		registration, err = r.ensureEtcdClient().UpdateDir(registrationPath, r.TTL)
		if err != nil {
			return err
		}
	}

	var existingRegistration ServiceRegistration
	existingJson, err := r.ensureEtcdClient().Get(registrationPath+"/location", false, false)
	if err != nil {
		if err.(*etcd.EtcdError).ErrorCode != ETCD_ENTRY_NOT_EXISTS {
			return err
		}
	} else {
		err = json.Unmarshal([]byte(existingJson.Node.Value), &existingRegistration)
		if err != nil {
			return err
		}

		if existingRegistration.StartedAt.After(container.Created) {
			return nil
		}
	}

	serviceRegistration := r.makeServiceRegistration(container)
	if serviceRegistration.Equals(existingRegistration) {
		statusLine := strings.Join([]string{
			container.ID[0:12],
			registrationPath,
			container.Config.Image,
			serviceRegistration.ExternalIp + ":" + serviceRegistration.ExternalPort,
			serviceRegistration.InternalIp + ":" + serviceRegistration.InternalPort,
			utils.HumanDuration(time.Now().Sub(container.Created)) + " ago",
			"In " + utils.HumanDuration(registration.Node.Expiration.Sub(time.Now())),
		}, " | ")

		r.OutputBuffer.Log(statusLine)
		return nil
	}

	jsonReg, err := json.Marshal(serviceRegistration)
	if err != nil {
		return err
	}

	err = r.setHostValue(serviceConfig.Name, "location", string(jsonReg))
	if err != nil {
		return err
	}

	jsonReg, err = json.Marshal(serviceConfig.Env)
	if err != nil {
		return err
	}

	err = r.setHostValue(serviceConfig.Name, "environment", string(jsonReg))
	if err != nil {
		return err
	}

	statusLine := strings.Join([]string{
		container.ID[0:12],
		registrationPath,
		container.Config.Image,
		serviceRegistration.ExternalIp + ":" + serviceRegistration.ExternalPort,
		serviceRegistration.InternalIp + ":" + serviceRegistration.InternalPort,
		utils.HumanDuration(time.Now().Sub(container.Created)) + " ago",
		"In " + utils.HumanDuration(registration.Node.Expiration.Sub(time.Now())),
	}, " | ")

	r.OutputBuffer.Log(statusLine)

	return nil
}

func (r *ServiceRegistry) UnRegisterService(container *docker.Container, serviceConfig *ServiceConfig) error {

	registrationPath := "/" + r.Env + "/" + r.Pool + "/hosts/" + r.ensureHostname() + "/" + serviceConfig.Name

	_, err := r.ensureEtcdClient().Delete(registrationPath, true)
	if err != nil && err.(*etcd.EtcdError).ErrorCode != ETCD_ENTRY_NOT_EXISTS {
		return err
	}

	statusLine := strings.Join([]string{
		container.ID[0:12],
		"",
		container.Config.Image,
		"",
		"",
		utils.HumanDuration(time.Now().Sub(container.Created)) + " ago",
		"",
	}, " | ")

	r.OutputBuffer.Log(statusLine)

	return nil
}

func (r *ServiceRegistry) findRegistration(node *etcd.Node, criteria *ServiceRegistration) (*ServiceRegistration, error) {

	var serviceRegistration ServiceRegistration

	if strings.HasSuffix(node.Key, "location") {
		err := json.Unmarshal([]byte(node.Value), &serviceRegistration)
		if err != nil {
			return nil, err
		}

		if serviceRegistration.Equals(*criteria) {
			serviceRegistration.Path = path.Dir(node.Key)
			return &serviceRegistration, nil
		}
	}

	for _, child := range node.Nodes {
		serviceRegistration, err := r.findRegistration(&child, criteria)
		if err != nil {
			return nil, err
		}

		if serviceRegistration != nil {
			// This is ugly.  We don't have the TTL on the "location" entry since it is
			// set on the parent node so after the first match, set the parents expiration
			// (based on TTL) for the registration if it's not alreayd set.
			if serviceRegistration.Expires.IsZero() {
				serviceRegistration.Expires = time.Now().Add(time.Duration(node.TTL) * time.Second)
			}
			return serviceRegistration, err
		}
	}

	return nil, nil

}

func (r *ServiceRegistry) IsRegistered(container *docker.Container, serviceConfig *ServiceConfig) (*ServiceRegistration, error) {
	registrations, err := r.ensureEtcdClient().Get("/", true, true)
	if err != nil {
		return nil, err
	}

	desiredServiceRegistration := r.makeServiceRegistration(container)
	return r.findRegistration(registrations.Node, desiredServiceRegistration)

}
