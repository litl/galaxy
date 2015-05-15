package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/fsouza/go-dockerclient"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/utils"
)

const (
	DefaultTTL = 60
)

type HostInfo struct {
	HostIP string
}

type Store struct {
	Backend      Backend
	Hostname     string
	TTL          uint64
	OutputBuffer *utils.OutputBuffer
	pollCh       chan bool
	registryURL  string
}

func NewStore(ttl uint64) *Store {
	return &Store{
		TTL:    ttl,
		pollCh: make(chan bool),
	}

}

// Build the Redis Pool
func (s *Store) Connect(registryURL string) {

	s.registryURL = registryURL
	u, err := url.Parse(registryURL)
	if err != nil {
		log.Fatalf("ERROR: Unable to parse %s", err)
	}

	if strings.ToLower(u.Scheme) == "redis" {
		s.Backend = &RedisBackend{
			RedisHost: u.Host,
		}
		s.Backend.Connect()
	} else {
		log.Fatalf("ERROR: Unsupported registry backend: %s", u)
	}
}

func (s *Store) PoolExists(env, pool string) (bool, error) {
	pools, err := s.ListPools(env)
	if err != nil {
		return false, err
	}

	return utils.StringInSlice(pool, pools), nil
}

func (s *Store) AppExists(app, env string) (bool, error) {
	return s.Backend.AppExists(app, env)
}

func (s *Store) ListAssignments(env, pool string) ([]string, error) {
	return s.Backend.ListAssignments(env, pool)
}

func (s *Store) ListAssignedPools(env, app string) ([]string, error) {
	pools, err := s.ListPools(env)
	if err != nil {
		return nil, err
	}

	assignments := []string{}
	for _, pool := range pools {
		apps, err := s.ListAssignments(env, pool)
		if err != nil {
			return nil, err
		}

		if utils.StringInSlice(app, apps) && !utils.StringInSlice(pool, assignments) {
			assignments = append(assignments, pool)
		}
	}
	return assignments, nil
}

func (s *Store) AssignApp(app, env, pool string) (bool, error) {
	if exists, err := s.AppExists(app, env); !exists || err != nil {
		return false, err
	}

	added, err := s.Backend.AssignApp(app, env, pool)
	if err != nil {
		return false, err
	}

	err = s.NotifyRestart(app, env)
	if err != nil {
		return added, err
	}

	return added, nil
}

func (s *Store) UnassignApp(app, env, pool string) (bool, error) {
	removed, err := s.Backend.UnassignApp(app, env, pool)
	if !removed || err != nil {
		return removed, err
	}

	err = s.NotifyRestart(app, env)
	if err != nil {
		return removed, err
	}

	return removed, nil
}

func (s *Store) CreatePool(name, env string) (bool, error) {
	return s.Backend.CreatePool(env, name)
}

func (s *Store) DeletePool(pool, env string) (bool, error) {
	assignments, err := s.ListAssignments(env, pool)
	if err != nil {
		return false, err
	}

	if len(assignments) > 0 {
		return false, nil
	}

	return s.Backend.DeletePool(env, pool)
}

func (s *Store) ListPools(env string) ([]string, error) {
	return s.Backend.ListPools(env)
}

func (s *Store) CreateApp(app, env string) (bool, error) {
	if exists, err := s.AppExists(app, env); exists || err != nil {
		return false, err
	}

	return s.Backend.CreateApp(app, env)

}

func (s *Store) DeleteApp(app, env string) (bool, error) {

	pools, err := s.ListPools(env)
	if err != nil {
		return false, err
	}

	for _, pool := range pools {
		assignments, err := s.ListAssignments(env, pool)
		if err != nil {
			return false, err
		}
		if utils.StringInSlice(app, assignments) {
			return false, errors.New(fmt.Sprintf("app is assigned to pool %s", pool))
		}
	}

	svcCfg, err := s.Backend.GetApp(app, env)
	if err != nil {
		return false, err
	}

	if svcCfg == nil {
		return true, nil
	}

	deleted, err := s.Backend.DeleteApp(svcCfg, env)
	if !deleted || err != nil {
		return deleted, err
	}

	err = s.NotifyEnvChanged(env)
	if err != nil {
		return deleted, err
	}

	return true, nil
}

func (s *Store) ListApps(env string) ([]App, error) {
	return s.Backend.ListApps(env)
}

func (s *Store) ListEnvs() ([]string, error) {
	return s.Backend.ListEnvs()
}

func (s *Store) GetApp(app, env string) (App, error) {
	exists, err := s.AppExists(app, env)
	if err != nil {
		return nil, err
	}

	if !exists {
		return nil, fmt.Errorf("app %s does not exist", app)
	}

	return s.Backend.GetApp(app, env)
}

func (s *Store) UpdateApp(svcCfg App, env string) (bool, error) {
	updated, err := s.Backend.UpdateApp(svcCfg, env)
	if !updated || err != nil {
		return updated, err
	}

	err = s.NotifyEnvChanged(env)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) UpdateHost(env, pool string, host HostInfo) error {
	return s.Backend.UpdateHost(env, pool, host)
}

func (s *Store) ListHosts(env, pool string) ([]HostInfo, error) {
	return s.Backend.ListHosts(env, pool)
}

func (s *Store) DeleteHost(env, pool string, host HostInfo) error {
	return s.Backend.DeleteHost(env, pool, host)
}

func (s *Store) newServiceRegistration(container *docker.Container, hostIP, galaxyPort string) *ServiceRegistration {
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

func (s *Store) RegisterService(env, pool, hostIP string, container *docker.Container) (*ServiceRegistration, error) {
	environment := s.EnvFor(container)

	name := environment["GALAXY_APP"]
	if name == "" {
		return nil, fmt.Errorf("GALAXY_APP not set on container %s", container.ID[0:12])
	}

	registrationPath := path.Join(env, pool, "hosts", hostIP, name, container.ID[0:12])

	serviceRegistration := s.newServiceRegistration(container, hostIP, environment["GALAXY_PORT"])
	serviceRegistration.Name = name
	serviceRegistration.ImageId = container.Config.Image

	vhosts := environment["VIRTUAL_HOST"]
	if strings.TrimSpace(vhosts) != "" {
		serviceRegistration.VirtualHosts = strings.Split(vhosts, ",")
	}

	errorPages := make(map[string]string)

	// scan environment variables for the VIRTUAL_HOST_%d pattern
	// but save the original variable and url.
	for vhostCode, url := range environment {
		code := 0
		n, err := fmt.Sscanf(vhostCode, "VIRTUAL_HOST_%d", &code)
		if err != nil || n == 0 {
			continue
		}

		errorPages[vhostCode] = url
	}

	if len(errorPages) > 0 {
		serviceRegistration.ErrorPages = errorPages
	}

	jsonReg, err := json.Marshal(serviceRegistration)
	if err != nil {
		return nil, err
	}

	// TODO: use a compare-and-swap SCRIPT
	_, err = s.Backend.Set(registrationPath, "location", string(jsonReg))
	if err != nil {
		return nil, err
	}

	_, err = s.Backend.Expire(registrationPath, s.TTL)
	if err != nil {
		return nil, err
	}
	serviceRegistration.Expires = time.Now().UTC().Add(time.Duration(s.TTL) * time.Second)

	return serviceRegistration, nil
}

func (s *Store) UnRegisterService(env, pool, hostIP string, container *docker.Container) (*ServiceRegistration, error) {

	environment := s.EnvFor(container)

	name := environment["GALAXY_APP"]
	if name == "" {
		return nil, fmt.Errorf("GALAXY_APP not set on container %s", container.ID[0:12])
	}

	registrationPath := path.Join(env, pool, "hosts", hostIP, name, container.ID[0:12])

	registration, err := s.GetServiceRegistration(env, pool, hostIP, container)
	if err != nil || registration == nil {
		return registration, err
	}

	if registration.ContainerID != container.ID {
		return nil, nil
	}

	_, err = s.Backend.Delete(registrationPath)
	if err != nil {
		return registration, err
	}

	return registration, nil
}

func (s *Store) GetServiceRegistration(env, pool, hostIP string, container *docker.Container) (*ServiceRegistration, error) {

	environment := s.EnvFor(container)

	name := environment["GALAXY_APP"]
	if name == "" {
		return nil, fmt.Errorf("GALAXY_APP not set on container %s", container.ID[0:12])
	}

	regPath := path.Join(env, pool, "hosts", hostIP, name, container.ID[0:12])

	existingRegistration := ServiceRegistration{
		Path: regPath,
	}

	location, err := s.Backend.Get(regPath, "location")

	if err != nil {
		return nil, err
	}

	if location != "" {
		err = json.Unmarshal([]byte(location), &existingRegistration)
		if err != nil {
			return nil, err
		}

		expires, err := s.Backend.TTL(regPath)
		if err != nil {
			return nil, err
		}
		existingRegistration.Expires = time.Now().UTC().Add(time.Duration(expires) * time.Second)
		return &existingRegistration, nil
	}

	return nil, nil
}

func (s *Store) IsRegistered(env, pool, hostIP string, container *docker.Container) (bool, error) {

	reg, err := s.GetServiceRegistration(env, pool, hostIP, container)
	return reg != nil, err
}

// TODO: get all ServiceRegistrations
func (s *Store) ListRegistrations(env string) ([]ServiceRegistration, error) {

	// TODO: convert to scan
	keys, err := s.Backend.Keys(path.Join(env, "*", "hosts", "*", "*", "*"))
	if err != nil {
		return nil, err
	}

	var regList []ServiceRegistration
	for _, key := range keys {

		val, err := s.Backend.Get(key, "location")
		if err != nil {
			log.Warnf("WARN: Unable to get location for %s: %s", key, err)
			continue
		}

		svcReg := ServiceRegistration{
			Name: path.Base(key),
		}
		err = json.Unmarshal([]byte(val), &svcReg)
		if err != nil {
			log.Warnf("WARN: Unable to unmarshal JSON for %s: %s", key, err)
			continue
		}

		svcReg.Path = key

		regList = append(regList, svcReg)
	}

	return regList, nil
}

func (s *Store) EnvFor(container *docker.Container) map[string]string {
	env := map[string]string{}
	for _, item := range container.Config.Env {
		sep := strings.Index(item, "=")
		k := item[0:sep]
		v := item[sep+1:]
		env[k] = v
	}
	return env
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
