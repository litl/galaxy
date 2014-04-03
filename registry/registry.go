package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/fsouza/go-dockerclient"
	"github.com/garyburd/redigo/redis"
	"github.com/litl/galaxy/utils"
)

/*
All config opbects in redis will be stored in a hash with an id key.
Services will have id, version and environment keys; while Hosts will have id
and location keys.

TODO: IMPORTANT: make an atomic compare-and-swap script to save configs, or
      switch to ORDERED SETS and log changes
*/

type ServiceConfig struct {
	ID      int64
	Name    string
	Version string
	Env     map[string]string
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

func NewServiceRegistry(redisHost string, env, pool, hostIP string) *ServiceRegistry {
	rwTimeout := 5 * time.Second

	redisPool := redis.Pool{
		MaxIdle:     1,
		IdleTimeout: 120 * time.Second,
		Dial: func() (redis.Conn, error) {
			return redis.DialTimeout("tcp", redisHost, rwTimeout, rwTimeout, rwTimeout)
		},
		// test every connection for now
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}

	s := &ServiceRegistry{
		redisPool: redisPool,
		Env:       env,
		Pool:      pool,
		HostIP:    hostIP,
	}
	return s
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

func (r *ServiceRegistry) GetServiceConfig(app string) (*ServiceConfig, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	matches, err := redis.Values(conn.Do("HGETALL", path.Join(r.Env, r.Pool, app)))
	if err != nil {
		fmt.Printf("ERROR: could not get ServiceConfig - %s\n", err)
	}

	svcCfg := &ServiceConfig{}
	err = redis.ScanStruct(matches, svcCfg)
	if err != nil {
		return nil, err
	}
	return svcCfg, nil
}

func (r *ServiceRegistry) RegisterService(container *docker.Container, serviceConfig *ServiceConfig) error {
	// TODO: WHAT IS THIS SSH setting?
	hostPath := path.Join(r.Env, r.Pool, "hosts", r.ensureHostname(), "ssh")
	_ = hostPath

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

	conn := r.redisPool.Get()
	defer conn.Close()

	_, err = conn.Do("HMSET", registrationPath, "id", serviceRegistration.ID, "location", jsonReg)
	if err != nil {
		return err
	}

	// TODO: SET TTL

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

	conn := r.redisPool.Get()
	defer conn.Close()

	_, err := conn.Do("DELETE", registrationPath)
	if err != nil {
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

// TODO: IsRegistered shoud return bool, or be changed to GetServiceRegistration
func (r *ServiceRegistry) IsRegistered(container *docker.Container, serviceConfig *ServiceConfig) (*ServiceRegistration, error) {

	desiredServiceRegistration := r.newServiceRegistration(container)
	regPath := path.Join(r.Env, r.Pool, "hosts", r.ensureHostname(), serviceConfig.Name)

	var existingRegistration ServiceRegistration

	conn := r.redisPool.Get()
	defer conn.Close()

	location, err := redis.Bytes(conn.Do("HGET", regPath, "location"))
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(location, &existingRegistration)
	if err != nil {
		return nil, err
	}

	if existingRegistration.Equals(*desiredServiceRegistration) {
		return &existingRegistration, nil
	}

	return nil, fmt.Errorf("NOT FOUND")
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

func (r *ServiceRegistry) CountInstances(app string) int {
	conn := r.redisPool.Get()
	defer conn.Close()

	// TODO: convert to SCAN
	matches, err := redis.Values(conn.Do("KEYS", path.Join(r.Env, r.Pool, "hosts", "*", app)))
	if err != nil {
		fmt.Printf("ERROR: could not count instances - %s\n", err)
	}
	return len(matches)
}

func (r *ServiceRegistry) EnvExists() (bool, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	// TODO: convert to SCAN
	matches, err := redis.Values(conn.Do("KEYS", path.Join(r.Env, "*")))
	if err != nil {
		return false, err
	}
	return len(matches) > 0, nil
}

func (r *ServiceRegistry) PoolExists() (bool, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	// TODO: convert to SCAN
	matches, err := redis.Values(conn.Do("KEYS", path.Join(r.Env, r.Pool, "*")))
	if err != nil {
		return false, err
	}
	return len(matches) > 0, nil
}

func (r *ServiceRegistry) AppExists(app string) (bool, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	// TODO: convert to SCAN
	matches, err := redis.Values(conn.Do("KEYS", path.Join(r.Env, r.Pool, app)))
	if err != nil {
		return false, err
	}
	return len(matches) > 0, nil
}

func (r *ServiceRegistry) ListApps() ([]ServiceConfig, error) {
	// get all service config versions
	panic("list apps")
	return nil, nil
}

func (r *ServiceRegistry) DeleteApp(app string) error {
	panic("delete app!")
	return nil
}
