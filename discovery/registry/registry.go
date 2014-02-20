package registry

import (
	"encoding/json"
	"github.com/coreos/go-etcd/etcd"
	"github.com/jwilder/go-dockerclient"
	"github.com/litl/galaxy/utils"
	"strings"
	"time"
)

const (
	ETCD_ENTRY_ALREADY_EXISTS = 105
)

type ServiceConfig struct {
	Name    string
	Version string
	Env     map[string]string
}

type ServiceRegistry struct {
	EctdClient   *etcd.Client
	Client       *docker.Client
	EtcdHosts    string
	Env          string
	Pool         string
	HostIp       string
	Hostname     string
	OutputBuffer *utils.OutputBuffer
}

type ServiceRegistration struct {
	ExternalIp   string `json:"EXTERNAL_IP"`
	ExternalPort string `json:"EXTERNAL_PORT"`
	InternalIp   string `json:"INTERNAL_IP"`
	InternalPort string `json:"INTERNAL_PORT"`
}

func (r *ServiceRegistry) setHostValue(service string, key string, value string) error {
	_, err := r.EctdClient.Set("/"+r.Env+"/"+r.Pool+"/hosts/"+r.Hostname+"/"+
		service+"/"+key, value, 0)
	return err
}

func (r *ServiceRegistry) RegisterService(container *docker.Container, serviceConfig *ServiceConfig) error {

	machines := strings.Split(r.EtcdHosts, ",")
	r.EctdClient = etcd.NewClient(machines)

	_, err := r.EctdClient.CreateDir("/"+r.Env+"/"+r.Pool+"/hosts", 0)
	if err != nil && err.(*etcd.EtcdError).ErrorCode != ETCD_ENTRY_ALREADY_EXISTS {
		return err
	}

	registrationPath := "/" + r.Env + "/" + r.Pool + "/hosts/" + r.Hostname + "/" + serviceConfig.Name
	registration, err := r.EctdClient.CreateDir(registrationPath, 60)
	if err != nil {

		if err.(*etcd.EtcdError).ErrorCode != ETCD_ENTRY_ALREADY_EXISTS {
			return err
		}

		registration, err = r.EctdClient.UpdateDir(registrationPath, 60)
		if err != nil {
			return err
		}
	}

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
