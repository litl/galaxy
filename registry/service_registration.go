package registry

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/garyburd/redigo/redis"
)

type ServiceRegistration struct {
	Name          string    `json:"NAME,omitempty"`
	ExternalIP    string    `json:"EXTERNAL_IP,omitempty"`
	ExternalPort  string    `json:"EXTERNAL_PORT,omitempty"`
	InternalIP    string    `json:"INTERNAL_IP,omitempty"`
	InternalPort  string    `json:"INTERNAL_PORT,omitempty"`
	ContainerID   string    `json:"CONTAINER_ID"`
	ContainerName string    `json:"CONTAINER_NAME"`
	Image         string    `json:"IMAGE,omitempty"`
	StartedAt     time.Time `json:"STARTED_AT"`
	Expires       time.Time `json:"-"`
	Path          string    `json:"-"`
	VirtualHosts  []string  `json:"VIRTUAL_HOSTS"`
	Port          string    `json:"PORT"`
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

func (r *ServiceRegistry) RegisterService(container *docker.Container, serviceConfig *ServiceConfig) (*ServiceRegistration, error) {
	registrationPath := path.Join(r.Env, r.Pool, "hosts", r.ensureHostname(), serviceConfig.Name)

	serviceRegistration := r.newServiceRegistration(container)
	serviceRegistration.Name = serviceConfig.Name

	vhosts := serviceConfig.Env()["VIRTUAL_HOST"]
	serviceRegistration.VirtualHosts = strings.Split(vhosts, ",")

	serviceRegistration.Port = serviceConfig.Env()["GALAXY_PORT"]

	jsonReg, err := json.Marshal(serviceRegistration)
	if err != nil {
		return nil, err
	}

	conn := r.redisPool.Get()
	defer conn.Close()

	// TODO: use a compare-and-swap SCRIPT
	_, err = conn.Do("HMSET", registrationPath, "location", jsonReg)
	if err != nil {
		return nil, err
	}

	_, err = conn.Do("EXPIRE", registrationPath, r.TTL)
	if err != nil {
		return nil, err
	}
	serviceRegistration.Expires = time.Now().UTC().Add(time.Duration(r.TTL) * time.Second)

	return serviceRegistration, nil
}

func (r *ServiceRegistry) UnRegisterService(container *docker.Container, serviceConfig *ServiceConfig) (*ServiceRegistration, error) {

	registrationPath := path.Join(r.Env, r.Pool, "hosts", r.ensureHostname(), serviceConfig.Name)

	conn := r.redisPool.Get()
	defer conn.Close()

	registration, err := r.GetServiceRegistration(container, serviceConfig)
	if err != nil {
		return registration, err
	}

	_, err = conn.Do("DEL", registrationPath)
	if err != nil {
		return registration, err
	}

	return registration, nil
}

func (r *ServiceRegistry) GetServiceRegistration(container *docker.Container, serviceConfig *ServiceConfig) (*ServiceRegistration, error) {

	regPath := path.Join(r.Env, r.Pool, "hosts", r.ensureHostname(), serviceConfig.Name)

	existingRegistration := ServiceRegistration{
		Path: regPath,
	}

	conn := r.redisPool.Get()
	defer conn.Close()

	val, err := conn.Do("HGET", regPath, "location")

	if err != nil {
		return nil, err
	}

	if val != nil {
		location, err := redis.Bytes(val, err)
		err = json.Unmarshal(location, &existingRegistration)
		if err != nil {
			return nil, err
		}

		expires, err := redis.Int(conn.Do("TTL", regPath))
		if err != nil {
			return nil, err
		}
		existingRegistration.Expires = time.Now().UTC().Add(time.Duration(expires) * time.Second)
		return &existingRegistration, nil
	}

	return nil, nil
}

func (r *ServiceRegistry) IsRegistered(container *docker.Container, serviceConfig *ServiceConfig) (bool, error) {

	reg, err := r.GetServiceRegistration(container, serviceConfig)
	return reg != nil, err
}

// TODO: get all ServiceRegistrations
func (r *ServiceRegistry) ListRegistrations() ([]ServiceRegistration, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	// TODO: convert to scan
	keys, err := redis.Strings(conn.Do("KEYS", path.Join(r.Env, "*", "hosts", "*", "*")))
	if err != nil {
		return nil, err
	}

	var regList []ServiceRegistration
	for _, key := range keys {

		val, err := redis.Bytes(conn.Do("HGET", key, "location"))
		if err != nil {
			return nil, err
		}

		svcReg := ServiceRegistration{
			Name: path.Base(key),
		}
		err = json.Unmarshal(val, &svcReg)
		if err != nil {
			return nil, err
		}

		svcReg.Path = key

		regList = append(regList, svcReg)
	}

	return regList, nil
}
