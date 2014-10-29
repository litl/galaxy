package config

import (
	"errors"
	"fmt"

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
	backend      Backend
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
	return r.backend.AppExists(app, env)
}

func (r *ConfigStore) ListAssignments(env, pool string) ([]string, error) {
	return r.backend.ListAssignments(env, pool)
}

func (r *ConfigStore) AssignApp(app, env, pool string) (bool, error) {
	if exists, err := r.AppExists(app, env); !exists || err != nil {
		return false, err
	}

	if exists, err := r.PoolExists(env, pool); !exists || err != nil {
		return false, errors.New(fmt.Sprintf("pool %s does not exist", pool))
	}

	added, err := r.backend.AssignApp(app, env, pool)
	if err != nil {
		return false, err
	}

	err = r.NotifyRestart(app, env)
	if err != nil {
		return added, err
	}

	return added, nil
}

func (r *ConfigStore) UnassignApp(app, env, pool string) (bool, error) {
	removed, err := r.backend.UnassignApp(app, env, pool)
	if !removed || err != nil {
		return removed, err
	}

	err = r.NotifyRestart(app, env)
	if err != nil {
		return removed, err
	}

	return removed, nil
}

func (r *ConfigStore) CreatePool(name, env string) (bool, error) {
	return r.backend.CreatePool(env, name)
}

func (r *ConfigStore) DeletePool(pool, env string) (bool, error) {
	assignments, err := r.ListAssignments(env, pool)
	if err != nil {
		return false, err
	}

	if len(assignments) > 0 {
		return false, nil
	}

	return r.backend.DeletePool(pool, env)
}

func (r *ConfigStore) ListPools(env string) (map[string][]string, error) {

	assignments := make(map[string][]string)

	matches, err := r.backend.ListPools(env)
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

	return r.backend.CreateApp(app, env)

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

	svcCfg, err := r.backend.GetApp(app, env)
	if err != nil {
		return false, err
	}

	if svcCfg == nil {
		return true, nil
	}

	deleted, err := r.backend.DeleteApp(svcCfg, env)
	if !deleted || err != nil {
		return deleted, err
	}

	err = r.NotifyEnvChanged(env)
	if err != nil {
		return deleted, err
	}

	return true, nil
}

func (r *ConfigStore) ListApps(env string) ([]ServiceConfig, error) {
	return r.backend.ListApps(env)
}

func (r *ConfigStore) ListEnvs() ([]string, error) {
	return r.backend.ListEnvs()
}

func (r *ConfigStore) GetApp(app, env string) (*ServiceConfig, error) {
	exists, err := r.AppExists(app, env)
	if err != nil || !exists {
		return nil, err
	}

	return r.backend.GetApp(app, env)
}

func (r *ConfigStore) UpdateApp(svcCfg *ServiceConfig, env string) (bool, error) {
	updated, err := r.backend.UpdateApp(svcCfg, env)
	if !updated || err != nil {
		return updated, err
	}

	err = r.NotifyEnvChanged(env)
	if err != nil {
		return false, err
	}
	return true, nil
}
