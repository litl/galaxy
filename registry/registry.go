package registry

import (
	"errors"
	"fmt"
	"path"
	"strings"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/utils"
)

/*
All config opbects in redis will be stored in a hash with an id key.
Services will have id, version and environment keys; while Hosts will have id
and location keys.

TODO: IMPORTANT: make an atomic compare-and-swap script to save configs, or
      switch to ORDERED SETS and log changes
*/

const (
	DefaultTTL = 60
)

type ServiceRegistry struct {
	backend      RegistryBackend
	Env          string
	Pool         string
	HostIP       string
	Hostname     string
	TTL          uint64
	HostSSHAddr  string
	OutputBuffer *utils.OutputBuffer
	pollCh       chan bool
	redisHost    string
}

type ConfigChange struct {
	ServiceConfig *ServiceConfig
	Restart       bool
	Error         error
}

func NewServiceRegistry(env, pool, hostIp string, ttl uint64, sshAddr string) *ServiceRegistry {
	return &ServiceRegistry{
		Env:         env,
		Pool:        pool,
		HostIP:      hostIp,
		TTL:         ttl,
		HostSSHAddr: sshAddr,
		pollCh:      make(chan bool),
	}

}

// Build the Redis Pool
func (r *ServiceRegistry) Connect(redisHost string) {

	r.redisHost = redisHost
	r.backend = &RedisBackend{
		RedisHost: redisHost,
	}
	r.backend.Connect()
}

func (r *ServiceRegistry) newServiceRegistration(container *docker.Container) *ServiceRegistration {
	//FIXME: We're using the first found port and assuming it's tcp.
	//How should we handle a service that exposes multiple ports
	//as well as tcp vs udp ports.
	var externalPort, internalPort string
	for k, v := range container.NetworkSettings.Ports {
		if len(v) > 0 {
			externalPort = v[0].HostPort
			internalPort = k.Port()
			break
		}
	}

	serviceRegistration := ServiceRegistration{
		ContainerName: container.Name,
		ContainerID:   container.ID,
		StartedAt:     container.Created,
		Image:         container.Config.Image,
	}

	if externalPort != "" && internalPort != "" {
		serviceRegistration.ExternalIP = r.HostIP
		serviceRegistration.InternalIP = container.NetworkSettings.IPAddress
		serviceRegistration.ExternalPort = externalPort
		serviceRegistration.InternalPort = internalPort
	}
	return &serviceRegistration
}

// TODO: log or return error?
func (r *ServiceRegistry) CountInstances(app string) int {
	// TODO: convert to SCAN
	// TODO: Should this just sum hosts? (this counts all services on all hosts)
	matches, err := r.backend.Keys(path.Join(r.Env, r.Pool, "hosts", "*", app))
	if err != nil {
		log.Printf("ERROR: could not count instances - %s\n", err)
	}

	return len(matches)
}

func (r *ServiceRegistry) EnvExists() (bool, error) {
	// TODO: convert to SCAN
	matches, err := r.backend.Keys(path.Join(r.Env, "*"))
	if err != nil {
		return false, err
	}
	return len(matches) > 0, nil
}

func (r *ServiceRegistry) PoolExists() (bool, error) {
	pools, err := r.ListPools()
	if err != nil {
		return false, err
	}
	_, ok := pools[r.Pool]
	return ok, nil
}

func (r *ServiceRegistry) AppExists(app string) (bool, error) {
	matches, err := r.backend.Keys(path.Join(r.Env, app, "*"))
	if err != nil {
		return false, err
	}
	return len(matches) > 0, nil
}

func (r *ServiceRegistry) ListAssignments(pool string) ([]string, error) {
	return r.backend.Members(path.Join(r.Env, "pools", pool))
}

func (r *ServiceRegistry) AssignApp(app string) (bool, error) {
	if exists, err := r.AppExists(app); !exists || err != nil {
		return false, err
	}

	added, err := r.backend.AddMember(path.Join(r.Env, "pools", r.Pool), app)
	if err != nil {
		return false, err
	}

	err = r.NotifyRestart(app)
	if err != nil {
		return added == 1, err
	}

	return added == 1, nil
}

func (r *ServiceRegistry) UnassignApp(app string) (bool, error) {
	//FIXME: Scan keys to make sure there are no deploye apps before
	//deleting the pool.

	//FIXME: Shutdown the associated auto-scaling groups tied to the
	//pool

	removed, err := r.backend.RemoveMember(path.Join(r.Env, "pools", r.Pool), app)
	if removed == 0 || err != nil {
		return false, err
	}

	err = r.NotifyRestart(app)
	if err != nil {
		return removed == 1, err
	}

	return removed == 1, nil
}

func (r *ServiceRegistry) CreatePool(name string) (bool, error) {
	//FIXME: Create an associated auto-scaling groups tied to the
	//pool

	added, err := r.backend.AddMember(path.Join(r.Env, "pools", "*"), name)
	if err != nil {
		return false, err
	}
	return added == 1, nil
}

func (r *ServiceRegistry) DeletePool(name string) (bool, error) {
	//FIXME: Scan keys to make sure there are no deploye apps before
	//deleting the pool.

	//FIXME: Shutdown the associated auto-scaling groups tied to the
	//pool

	assignments, err := r.ListAssignments(name)
	if err != nil {
		return false, err
	}

	if len(assignments) > 0 {
		return false, nil
	}

	removed, err := r.backend.RemoveMember(path.Join(r.Env, "pools", "*"), name)
	if err != nil {
		return false, err
	}
	return removed == 1, nil
}

func (r *ServiceRegistry) ListPools() (map[string][]string, error) {
	assignments := make(map[string][]string)

	matches, err := r.backend.Members(path.Join(r.Env, "pools", "*"))
	if err != nil {
		return assignments, err
	}

	for _, pool := range matches {

		members, err := r.ListAssignments(pool)
		if err != nil {
			return assignments, err
		}
		assignments[pool] = members
	}

	return assignments, nil
}

func (r *ServiceRegistry) CreateApp(app string) (bool, error) {
	if exists, err := r.AppExists(app); exists || err != nil {
		return false, err
	}

	emptyConfig := NewServiceConfig(app, "")
	emptyConfig.environmentVMap.Set("ENV", r.Env)

	return r.SetServiceConfig(emptyConfig)
}

func (r *ServiceRegistry) DeleteApp(app string) (bool, error) {

	pools, err := r.ListPools()
	if err != nil {
		return false, err
	}

	for pool, assignments := range pools {
		if utils.StringInSlice(app, assignments) {
			return false, errors.New(fmt.Sprintf("app is assigned to pool %s", pool))
		}
	}

	svcCfg, err := r.GetServiceConfig(app)
	if err != nil {
		return false, err
	}

	if svcCfg == nil {
		return true, nil
	}

	return r.DeleteServiceConfig(svcCfg)
}

func (r *ServiceRegistry) ListApps() ([]ServiceConfig, error) {
	// TODO: convert to scan
	apps, err := r.backend.Keys(path.Join(r.Env, "*", "environment"))
	if err != nil {
		return nil, err
	}

	// TODO: is it OK to error out early?
	var appList []ServiceConfig
	for _, app := range apps {
		parts := strings.Split(app, "/")

		// app entries should be 3 parts, /env/pool/app
		if len(parts) != 3 {
			continue
		}

		// we don't want host keys
		if parts[1] == "hosts" {
			continue
		}

		cfg, err := r.GetServiceConfig(parts[1])
		if err != nil {
			return nil, err
		}

		appList = append(appList, *cfg)
	}

	return appList, nil
}

func (r *ServiceRegistry) ListEnvs() ([]string, error) {
	envs := []string{}
	apps, err := r.backend.Keys(path.Join("*", "*", "environment"))
	if err != nil {
		return nil, err
	}

	for _, app := range apps {
		parts := strings.Split(app, "/")
		if !utils.StringInSlice(parts[0], envs) {
			envs = append(envs, parts[0])
		}
	}
	return envs, nil
}
