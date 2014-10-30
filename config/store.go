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

type HostInfo struct {
	HostIP string
}

type Store struct {
	backend      Backend
	HostIP       string
	Hostname     string
	TTL          uint64
	HostSSHAddr  string
	OutputBuffer *utils.OutputBuffer
	pollCh       chan bool
	redisHost    string
}

func NewStore(hostIp string, ttl uint64, sshAddr string) *Store {
	return &Store{
		HostIP:      hostIp,
		TTL:         ttl,
		HostSSHAddr: sshAddr,
		pollCh:      make(chan bool),
	}

}

// Build the Redis Pool
func (r *Store) Connect(redisHost string) {

	r.redisHost = redisHost
	r.backend = &RedisBackend{
		RedisHost: redisHost,
	}
	r.backend.Connect()
}

func (r *Store) PoolExists(env, pool string) (bool, error) {
	pools, err := r.ListPools(env)
	if err != nil {
		return false, err
	}

	return utils.StringInSlice(pool, pools), nil
}

func (r *Store) AppExists(app, env string) (bool, error) {
	return r.backend.AppExists(app, env)
}

func (r *Store) ListAssignments(env, pool string) ([]string, error) {
	return r.backend.ListAssignments(env, pool)
}

func (r *Store) AssignApp(app, env, pool string) (bool, error) {
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

func (r *Store) UnassignApp(app, env, pool string) (bool, error) {
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

func (r *Store) CreatePool(name, env string) (bool, error) {
	return r.backend.CreatePool(env, name)
}

func (r *Store) DeletePool(pool, env string) (bool, error) {
	assignments, err := r.ListAssignments(env, pool)
	if err != nil {
		return false, err
	}

	if len(assignments) > 0 {
		return false, nil
	}

	return r.backend.DeletePool(pool, env)
}

func (r *Store) ListPools(env string) ([]string, error) {
	return r.backend.ListPools(env)
}

func (r *Store) CreateApp(app, env string) (bool, error) {
	if exists, err := r.AppExists(app, env); exists || err != nil {
		return false, err
	}

	return r.backend.CreateApp(app, env)

}

func (r *Store) DeleteApp(app, env string) (bool, error) {

	pools, err := r.ListPools(env)
	if err != nil {
		return false, err
	}

	for _, pool := range pools {
		assignments, err := r.ListAssignments(env, pool)
		if err != nil {
			return false, err
		}
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

func (r *Store) ListApps(env string) ([]AppConfig, error) {
	return r.backend.ListApps(env)
}

func (r *Store) ListEnvs() ([]string, error) {
	return r.backend.ListEnvs()
}

func (r *Store) GetApp(app, env string) (*AppConfig, error) {
	exists, err := r.AppExists(app, env)
	if err != nil || !exists {
		return nil, err
	}

	return r.backend.GetApp(app, env)
}

func (r *Store) UpdateApp(svcCfg *AppConfig, env string) (bool, error) {
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

func (r *Store) UpdateHost(env, pool string, host HostInfo) error {
	return r.backend.UpdateHost(env, pool, host)
}

func (r *Store) ListHosts(env, pool string) ([]HostInfo, error) {
	return r.backend.ListHosts(env, pool)
}

func (r *Store) DeleteHost(env, pool string, host HostInfo) error {
	return r.backend.DeleteHost(env, pool, host)
}
