package registry

import (
	"encoding/json"
	"fmt"
	"github.com/fsouza/go-dockerclient"
	"github.com/garyburd/redigo/redis"
	"github.com/litl/galaxy/utils"
	"os"
	"path"
	"strings"
	"time"
)

type ServiceConfig struct {
	Name string
	ID   int64
	Env  map[string]string
}

type ServiceRegistration struct {
	// ID is used for ordering and conflict resolution.
	// Usualy set to time.Now().UnixNano()
	ID           int64     `json:"-"`
	ExternalIP   string    `json:"EXTERNAL_IP"`
	ExternalPort string    `json:"EXTERNAL_PORT"`
	InternalIP   string    `json:"INTERNAL_IP"`
	InternalPort string    `json:"INTERNAL_PORT"`
	ContainerID  string    `json:"CONTAINER_ID"`
	StartedAt    time.Time `json:"STARTED_AT"`
	Expires      time.Time `json:"-"`
	Path         string    `json:"-"`
}

type ServiceRegistry struct {
	redisPool    redis.Pool
	Env          string
	Pool         string
	HostIP       string
	Hostname     string
	TTL          uint64
	HostSSHAddr  string
	OutputBuffer *utils.OutputBuffer
}

type ConfigChange struct {
	ServiceConfig *ServiceConfig
	Error         error
}

func (s *ServiceRegistration) Equals(other ServiceRegistration) bool {
	return s.ExternalIP == other.ExternalIP &&
		s.ExternalPort == other.ExternalPort &&
		s.InternalIP == other.InternalIP &&
		s.InternalPort == other.InternalPort
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

func NewServiceRegistry(redisHost string, env, pool string) *ServiceRegistry {
	rwTimeout := 5 * time.Second

	redisPool := &redis.Pool{
		MaxIdle:     1,
		IdleTimeout: 120 * time.Second,
		Dial: func() (redisConn, error) {
			return redis.DialTimeout("tcp", redisHost, rwTimeout, rwTimeout, rwTimeout)
		},
		// should we always test here?
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}

	s := &ServiceRegistry{
		redisPool: redisPool,
		Env:       env,
		Pool:      pool,
	}
	return s
}

func (r *ServiceRegistry) setHostValue(service string, key string, value string) error {
	panic("set value for host")
	return nil
}

func (r *ServiceRegistry) newServiceRegistration(container *docker.Container) *ServiceRegistration {
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
		ID:           time.Now().UnixNano(),
		ExternalIP:   r.HostIP,
		ExternalPort: externalPort,
		InternalIP:   container.NetworkSettings.IPAddress,
		InternalPort: internalPort,
		ContainerID:  container.ID,
		StartedAt:    container.Created,
	}
	return &serviceRegistration
}

func (r *ServiceRegistry) GetServiceConfig(redisKey string, app string) (*ServiceConfig, error) {
	// HGET /r.Env/r.Pool/app
	// id: (won't need this until we have multiple redis servers)
	// version:
	// environment:
	panic("get config hash from redis")
	return nil, nil
}

func (r *ServiceRegistry) RegisterService(container *docker.Container, serviceConfig *ServiceConfig) error {

	hostPath := path.Join(r.Env, r.Pool, "hosts", r.ensureHostname(), "ssh")
	// set host ssh address with ttl
	panic("Set(hostPath, r.HostSSHAddr, r.TTL)" + hostPath)

	registrationPath := path.Join(r.Env, r.Pool, "hosts", r.ensureHostname(), serviceConfig.Name)

	var existingRegistration ServiceRegistration

	panic("get host registration JSON")
	existingJson := []byte{}
	err := json.Unmarshal([]byte(existingJson), &existingRegistration)
	if err != nil {
		return err
	}

	if existingRegistration.StartedAt.After(container.Created) {
		return nil
	}

	serviceRegistration := r.newServiceRegistration(container)
	if serviceRegistration.Equals(existingRegistration) {
		statusLine := strings.Join([]string{
			container.ID[0:12],
			registrationPath,
			container.Config.Image,
			serviceRegistration.ExternalIP + ":" + serviceRegistration.ExternalPort,
			serviceRegistration.InternalIP + ":" + serviceRegistration.InternalPort,
			utils.HumanDuration(time.Now().Sub(container.Created)) + " ago",
			"In " + "some-amount-of-time", // FIXME: what is this?
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
		serviceRegistration.ExternalIP + ":" + serviceRegistration.ExternalPort,
		serviceRegistration.InternalIP + ":" + serviceRegistration.InternalPort,
		utils.HumanDuration(time.Now().Sub(container.Created)) + " ago",
		"In " + "some-amount-of-time", // FIXME: what is this?
	}, " | ")

	r.OutputBuffer.Log(statusLine)

	return nil
}

func (r *ServiceRegistry) UnRegisterService(container *docker.Container, serviceConfig *ServiceConfig) error {

	registrationPath := path.Join(r.Env, r.Pool, "hosts", r.ensureHostname(), serviceConfig.Name)
	panic("DELETE key and check for existence" + registrationPath)

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

// TODO: IsRegistered shoud be bool, or changed to GetServiceRegistration
func (r *ServiceRegistry) IsRegistered(container *docker.Container, serviceConfig *ServiceConfig) (*ServiceRegistration, error) {

	desiredServiceRegistration := r.newServiceRegistration(container)
	regPath := path.Join(r.Env, r.Pool, "hosts", r.ensureHostname(), serviceConfig.Name, "location")

	var existingRegistration ServiceRegistration
	panic("GET " + regPath)
	err := json.Unmarshal([]byte{}, &existingRegistration)
	if err != nil {
		return nil, err
	}

	panic("TTL regPath")

	if existingRegistration.Equals(*desiredServiceRegistration) {
		return &existingRegistration, nil
	}

	return nil, fmt.Errorf("NOT FOUND?")
}

// We need an ID to start from, so we know when something has changed.
// Return nil,nil if mothing has changed (for now)
func (r *ServiceRegistry) Watch(lastID int64, changes chan *ConfigChange, stop chan struct{}) {
	watchPath := path.Join(r.Env, r.Pool, "*")

	go func() {
		// TODO: default polling interval
		ticker := time.NewTicker(2 * time.Second)
		select {
		case <-stop:
			ticker.Stop()
			return
		case <-ticker.C:
			panic("KEYS " + watchPath)
			// GET CONFIGS AND CHECK IDs
			changes <- nil
		}
	}()
}
