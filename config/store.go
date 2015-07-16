package config

import (
	"errors"
	"fmt"
	"net/url"
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
	// The Pool field is currently only used for commander dump and restore
	Pool string
}

type Store struct {
	Backend     Backend
	TTL         uint64
	pollCh      chan bool
	restartChan chan *ConfigChange
}

func NewStore(ttl uint64, registryURL string) *Store {
	s := &Store{
		TTL:         ttl,
		pollCh:      make(chan bool),
		restartChan: make(chan *ConfigChange, 10),
	}

	u, err := url.Parse(registryURL)
	if err != nil {
		log.Fatalf("ERROR: Unable to parse %s", err)
	}

	switch strings.ToLower(u.Scheme) {
	case "redis":
		s.Backend = &RedisBackend{
			RedisHost: u.Host,
		}
		s.Backend.connect()
	case "consul":
		s.Backend = NewConsulBackend()
	default:
		log.Fatalf("ERROR: Unsupported registry backend: %s", u)
	}

	return s
}

// FIXME: We still have a function that returns just an *AppConfig for the
//        RedisBackend. Unify these somehow, and preferebly decouple this from
//        config.Store.
func (s *Store) NewAppConfig(app, version string) App {
	var appCfg App
	switch s.Backend.(type) {
	case *RedisBackend:
		appCfg = &AppConfig{
			name:            app,
			versionVMap:     utils.NewVersionedMap(),
			environmentVMap: utils.NewVersionedMap(),
			portsVMap:       utils.NewVersionedMap(),
			runtimeVMap:     utils.NewVersionedMap(),
		}
	case *ConsulBackend:
		appCfg = &AppDefinition{
			AppName:     app,
			Environment: make(map[string]string),
		}
	default:
		panic("unknown backend")
	}

	appCfg.SetVersion(version)
	return appCfg
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

func (s *Store) RegisterService(env, pool, hostIP string, container *docker.Container) (*ServiceRegistration, error) {

	environment := s.EnvFor(container)

	name := environment["GALAXY_APP"]
	if name == "" {
		return nil, fmt.Errorf("GALAXY_APP not set on container %s", container.ID[0:12])
	}

	serviceRegistration := newServiceRegistration(container, hostIP, environment["GALAXY_PORT"])
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

	serviceRegistration.Expires = time.Now().UTC().Add(time.Duration(s.TTL) * time.Second)

	err := s.Backend.RegisterService(env, pool, serviceRegistration)
	return serviceRegistration, err
}

func (s *Store) UnRegisterService(env, pool, hostIP string, container *docker.Container) (*ServiceRegistration, error) {

	environment := s.EnvFor(container)

	name := environment["GALAXY_APP"]
	if name == "" {
		return nil, fmt.Errorf("GALAXY_APP not set on container %s", container.ID[0:12])
	}

	registration, err := s.Backend.UnregisterService(env, pool, hostIP, name, container.ID)
	if err != nil || registration == nil {
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

	serviceReg, err := s.Backend.GetServiceRegistration(env, pool, hostIP, name, container.ID)
	if err != nil {
		return nil, err
	}
	return serviceReg, nil
}

func (s *Store) IsRegistered(env, pool, hostIP string, container *docker.Container) (bool, error) {

	reg, err := s.GetServiceRegistration(env, pool, hostIP, container)
	return reg != nil, err
}

func (s *Store) ListRegistrations(env string) ([]ServiceRegistration, error) {
	return s.Backend.ListRegistrations(env)
}

func (s *Store) EnvFor(container *docker.Container) map[string]string {
	env := map[string]string{}
	for _, item := range container.Config.Env {
		sep := strings.Index(item, "=")
		if sep < 0 {
			continue
		}
		k, v := item[0:sep], item[sep+1:]
		env[k] = v
	}
	return env
}
