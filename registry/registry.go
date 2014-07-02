package registry

import (
	"os"
	"path"
	"strings"
	"time"

	"github.com/fsouza/go-dockerclient"
	"github.com/garyburd/redigo/redis"
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

type ServiceRegistry struct {
	redisPool    redis.Pool
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

// Build the Redis Pool
func (r *ServiceRegistry) Connect(redisHost string) {
	r.redisHost = redisHost
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
			if err != nil {
				defer c.Close()
			}
			return err
		},
	}

	r.redisPool = redisPool
}

func (r *ServiceRegistry) reconnectRedis() {
	r.redisPool.Close()
	r.Connect(r.redisHost)
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
		ContainerID: container.ID,
		StartedAt:   container.Created,
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
	conn := r.redisPool.Get()
	defer conn.Close()

	// TODO: convert to SCAN
	// TODO: Should this just sum hosts? (this counts all services on all hosts)
	matches, err := redis.Values(conn.Do("KEYS", path.Join(r.Env, r.Pool, "hosts", "*", app)))
	if err != nil {
		log.Printf("ERROR: could not count instances - %s\n", err)
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

	pools, err := r.ListPools()
	if err != nil {
		return false, err
	}
	return utils.StringInSlice(r.Pool, pools), nil
}

func (r *ServiceRegistry) AppExists(app string) (bool, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	// TODO: convert to SCAN
	matches, err := redis.Values(conn.Do("KEYS", path.Join(r.Env, app, "*")))
	if err != nil {
		return false, err
	}
	return len(matches) > 0, nil
}

func (r *ServiceRegistry) CreatePool(name string) (bool, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	//FIXME: Create an associated auto-scaling groups tied to the
	//pool

	added, err := redis.Int(conn.Do("SADD", path.Join(r.Env, "pools", "*"), name))
	if err != nil {
		return false, err
	}
	return added == 1, nil
}

func (r *ServiceRegistry) DeletePool(name string) (bool, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	//FIXME: Scan keys to make sure there are no deploye apps before
	//deleting the pool.

	//FIXME: Shutdown the associated auto-scaling groups tied to the
	//pool

	removed, err := redis.Int(conn.Do("SREM", path.Join(r.Env, "pools", "*"), name))
	if err != nil {
		return false, err
	}
	return removed == 1, nil
}

func (r *ServiceRegistry) ListPools() ([]string, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	// TODO: convert to scan
	matches, err := redis.Strings(conn.Do("SMEMBERS", path.Join(r.Env, "pools", "*")))
	if err != nil {
		return nil, err
	}

	return matches, nil
}

func (r *ServiceRegistry) CreateApp(app string) (bool, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if exists, err := r.AppExists(app); exists || err != nil {
		return false, err
	}

	if exists, err := r.PoolExists(); !exists || err != nil {
		return false, err
	}

	emptyConfig := NewServiceConfig(app, "")
	emptyConfig.environmentVMap.Set("ENV", r.Env)

	return r.SetServiceConfig(emptyConfig)
}

func (r *ServiceRegistry) DeleteApp(app string) (bool, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	deletedOne := false
	deleted, err := conn.Do("DEL", path.Join(r.Env, app))
	if err != nil {
		return false, err
	}

	deletedOne = deletedOne || deleted.(int64) == 1

	for _, k := range []string{"environment", "version", "ports"} {
		deleted, err = conn.Do("DEL", path.Join(r.Env, app, k))
		if err != nil {
			return false, err
		}
		deletedOne = deletedOne || deleted.(int64) == 1
	}

	return deletedOne, nil
}

func (r *ServiceRegistry) ListApps(pool string) ([]ServiceConfig, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if conn.Err() != nil {
		conn.Close()
		r.reconnectRedis()
		return nil, conn.Err()
	}

	if pool == "" {
		pool = r.Pool
	}

	// TODO: convert to scan
	apps, err := redis.Strings(conn.Do("KEYS", path.Join(r.Env, "*", "environment")))
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
