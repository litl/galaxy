package config

import (
	"errors"
	"fmt"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/utils"
	"net/url"
	"strings"
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
func (r *Store) Connect(registryURL string) {

	r.registryURL = registryURL
	u, err := url.Parse(registryURL)
	if err != nil {
		log.Fatalf("ERROR: Unable to parse %s", err)
	}

	if strings.ToLower(u.Scheme) == "redis" {
		r.Backend = &RedisBackend{
			RedisHost: u.Host,
		}
		r.Backend.Connect()
	} else {
		log.Fatalf("ERROR: Unsupported registry backend: %s", u)
	}
}

func (r *Store) PoolExists(env, pool string) (bool, error) {
	pools, err := r.ListPools(env)
	if err != nil {
		return false, err
	}

	return utils.StringInSlice(pool, pools), nil
}

func (r *Store) AppExists(app, env string) (bool, error) {
	return r.Backend.AppExists(app, env)
}

func (r *Store) ListAssignments(env, pool string) ([]string, error) {
	return r.Backend.ListAssignments(env, pool)
}

func (r *Store) AssignApp(app, env, pool string) (bool, error) {
	if exists, err := r.AppExists(app, env); !exists || err != nil {
		return false, err
	}

	added, err := r.Backend.AssignApp(app, env, pool)
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
	removed, err := r.Backend.UnassignApp(app, env, pool)
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
	return r.Backend.CreatePool(env, name)
}

func (r *Store) DeletePool(pool, env string) (bool, error) {
	assignments, err := r.ListAssignments(env, pool)
	if err != nil {
		return false, err
	}

	if len(assignments) > 0 {
		return false, nil
	}

	return r.Backend.DeletePool(pool, env)
}

func (r *Store) ListPools(env string) ([]string, error) {
	return r.Backend.ListPools(env)
}

func (r *Store) CreateApp(app, env string) (bool, error) {
	if exists, err := r.AppExists(app, env); exists || err != nil {
		return false, err
	}

	return r.Backend.CreateApp(app, env)

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

	svcCfg, err := r.Backend.GetApp(app, env)
	if err != nil {
		return false, err
	}

	if svcCfg == nil {
		return true, nil
	}

	deleted, err := r.Backend.DeleteApp(svcCfg, env)
	if !deleted || err != nil {
		return deleted, err
	}

	err = r.NotifyEnvChanged(env)
	if err != nil {
		return deleted, err
	}

	return true, nil
}

func (r *Store) ListApps(env string) ([]*AppConfig, error) {
	return r.Backend.ListApps(env)
}

func (r *Store) ListEnvs() ([]string, error) {
	return r.Backend.ListEnvs()
}

func (r *Store) GetApp(app, env string) (*AppConfig, error) {
	exists, err := r.AppExists(app, env)
	if err != nil || !exists {
		return nil, err
	}

	return r.Backend.GetApp(app, env)
}

func (r *Store) UpdateApp(svcCfg *AppConfig, env string) (bool, error) {
	updated, err := r.Backend.UpdateApp(svcCfg, env)
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
	return r.Backend.UpdateHost(env, pool, host)
}

func (r *Store) ListHosts(env, pool string) ([]HostInfo, error) {
	return r.Backend.ListHosts(env, pool)
}

func (r *Store) DeleteHost(env, pool string, host HostInfo) error {
	return r.Backend.DeleteHost(env, pool, host)
}
