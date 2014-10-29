package config

import (
	"errors"
	"fmt"
	"path"
	"strings"

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

type ConfigStore struct {
	backend      ConfigBackend
	HostIP       string
	Hostname     string
	TTL          uint64
	HostSSHAddr  string
	OutputBuffer *utils.OutputBuffer
	pollCh       chan bool
	redisHost    string
}

func NewConfigStore(hostIp string, ttl uint64, sshAddr string) *ConfigStore {
	return &ConfigStore{
		HostIP:      hostIp,
		TTL:         ttl,
		HostSSHAddr: sshAddr,
		pollCh:      make(chan bool),
	}

}

// Build the Redis Pool
func (r *ConfigStore) Connect(redisHost string) {

	r.redisHost = redisHost
	r.backend = &RedisBackend{
		RedisHost: redisHost,
	}
	r.backend.Connect()
}

func (r *ConfigStore) PoolExists(env, pool string) (bool, error) {
	pools, err := r.ListPools(env)
	if err != nil {
		return false, err
	}
	_, ok := pools[pool]
	return ok, nil
}

func (r *ConfigStore) AppExists(app, env string) (bool, error) {
	matches, err := r.backend.Keys(path.Join(env, app, "*"))
	if err != nil {
		return false, err
	}
	return len(matches) > 0, nil
}

func (r *ConfigStore) ListAssignments(env, pool string) ([]string, error) {
	return r.backend.Members(path.Join(env, "pools", pool))
}

func (r *ConfigStore) AssignApp(app, env, pool string) (bool, error) {
	if exists, err := r.AppExists(app, env); !exists || err != nil {
		return false, err
	}

	if exists, err := r.PoolExists(env, pool); !exists || err != nil {
		return false, errors.New(fmt.Sprintf("pool %s does not exist", pool))
	}

	added, err := r.backend.AddMember(path.Join(env, "pools", pool), app)
	if err != nil {
		return false, err
	}

	err = r.NotifyRestart(app, env)
	if err != nil {
		return added == 1, err
	}

	return added == 1, nil
}

func (r *ConfigStore) UnassignApp(app, env, pool string) (bool, error) {
	//FIXME: Scan keys to make sure there are no deploye apps before
	//deleting the pool.

	//FIXME: Shutdown the associated auto-scaling groups tied to the
	//pool

	removed, err := r.backend.RemoveMember(path.Join(env, "pools", pool), app)
	if removed == 0 || err != nil {
		return false, err
	}

	err = r.NotifyRestart(app, env)
	if err != nil {
		return removed == 1, err
	}

	return removed == 1, nil
}

func (r *ConfigStore) CreatePool(name, env string) (bool, error) {
	//FIXME: Create an associated auto-scaling groups tied to the
	//pool

	added, err := r.backend.AddMember(path.Join(env, "pools", "*"), name)
	if err != nil {
		return false, err
	}
	return added == 1, nil
}

func (r *ConfigStore) DeletePool(pool, env string) (bool, error) {
	//FIXME: Scan keys to make sure there are no deploye apps before
	//deleting the pool.

	//FIXME: Shutdown the associated auto-scaling groups tied to the
	//pool

	assignments, err := r.ListAssignments(env, pool)
	if err != nil {
		return false, err
	}

	if len(assignments) > 0 {
		return false, nil
	}

	removed, err := r.backend.RemoveMember(path.Join(env, "pools", "*"), pool)
	if err != nil {
		return false, err
	}
	return removed == 1, nil
}

func (r *ConfigStore) ListPools(env string) (map[string][]string, error) {
	assignments := make(map[string][]string)

	matches, err := r.backend.Members(path.Join(env, "pools", "*"))
	if err != nil {
		return assignments, err
	}

	for _, pool := range matches {

		members, err := r.ListAssignments(env, pool)
		if err != nil {
			return assignments, err
		}
		assignments[pool] = members
	}

	return assignments, nil
}

func (r *ConfigStore) CreateApp(app, env string) (bool, error) {
	if exists, err := r.AppExists(app, env); exists || err != nil {
		return false, err
	}

	emptyConfig := NewServiceConfig(app, "")
	emptyConfig.environmentVMap.Set("ENV", env)

	return r.SetServiceConfig(emptyConfig, env)
}

func (r *ConfigStore) DeleteApp(app, env string) (bool, error) {

	pools, err := r.ListPools(env)
	if err != nil {
		return false, err
	}

	for pool, assignments := range pools {
		if utils.StringInSlice(app, assignments) {
			return false, errors.New(fmt.Sprintf("app is assigned to pool %s", pool))
		}
	}

	svcCfg, err := r.GetServiceConfig(app, env)
	if err != nil {
		return false, err
	}

	if svcCfg == nil {
		return true, nil
	}

	return r.DeleteServiceConfig(svcCfg, env)
}

func (r *ConfigStore) ListApps(env string) ([]ServiceConfig, error) {
	// TODO: convert to scan
	apps, err := r.backend.Keys(path.Join(env, "*", "environment"))
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

		cfg, err := r.GetServiceConfig(parts[1], env)
		if err != nil {
			return nil, err
		}

		appList = append(appList, *cfg)
	}

	return appList, nil
}

func (r *ConfigStore) ListEnvs() ([]string, error) {
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

func (r *ConfigStore) Get(app, env string) (*ServiceConfig, error) {
	return r.GetServiceConfig(app, env)
}

func (r *ConfigStore) GetServiceConfig(app, env string) (*ServiceConfig, error) {
	exists, err := r.AppExists(app, env)
	if err != nil || !exists {
		return nil, err
	}

	svcCfg := NewServiceConfig(path.Base(app), "")

	err = r.LoadVMap(path.Join(env, app, "environment"), svcCfg.environmentVMap)
	if err != nil {
		return nil, err
	}
	err = r.LoadVMap(path.Join(env, app, "version"), svcCfg.versionVMap)
	if err != nil {
		return nil, err
	}

	err = r.LoadVMap(path.Join(env, app, "ports"), svcCfg.portsVMap)
	if err != nil {
		return nil, err
	}

	return svcCfg, nil
}

func (r *ConfigStore) SetServiceConfig(svcCfg *ServiceConfig, env string) (bool, error) {

	for k, v := range svcCfg.Env() {
		if svcCfg.environmentVMap.Get(k) != v {
			svcCfg.environmentVMap.Set(k, v)
		}
	}

	for k, v := range svcCfg.Ports() {
		if svcCfg.portsVMap.Get(k) != v {
			svcCfg.portsVMap.Set(k, v)
		}
	}

	//TODO: user MULTI/EXEC
	err := r.SaveVMap(path.Join(env, svcCfg.Name, "environment"),
		svcCfg.environmentVMap)

	if err != nil {
		return false, err
	}

	err = r.SaveVMap(path.Join(env, svcCfg.Name, "version"),
		svcCfg.versionVMap)

	if err != nil {
		return false, err
	}

	err = r.SaveVMap(path.Join(env, svcCfg.Name, "ports"),
		svcCfg.portsVMap)

	if err != nil {
		return false, err
	}

	err = r.notifyChanged(env)
	if err != nil {
		return false, err
	}

	return true, nil
}

func (r *ConfigStore) DeleteServiceConfig(svcCfg *ServiceConfig, env string) (bool, error) {
	deletedOne := false
	deleted, err := r.backend.Delete(path.Join(env, svcCfg.Name))
	if err != nil {
		return false, err
	}

	deletedOne = deletedOne || deleted == 1

	for _, k := range []string{"environment", "version", "ports"} {
		deleted, err = r.backend.Delete(path.Join(env, svcCfg.Name, k))
		if err != nil {
			return false, err
		}
		deletedOne = deletedOne || deleted == 1
	}

	if deletedOne {
		err = r.notifyChanged(env)
		if err != nil {
			return deletedOne, err
		}
	}

	return deletedOne, nil
}
