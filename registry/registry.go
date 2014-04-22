package registry

import (
	"encoding/json"
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

type ServiceConfig struct {
	// ID is used for ordering and conflict resolution.
	// Usualy set to time.Now().UnixNano()
	ID              int64  `redis:"id"`
	Name            string `redis:"name"`
	Version         string
	Env             map[string]string
	versionVMap     *utils.VersionedMap
	environmentVMap *utils.VersionedMap
}

type ServiceRegistration struct {
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

// Build the Redis Pool
func (r *ServiceRegistry) Connect(redisHost string) {
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

	r.redisPool = redisPool
}

func NewServiceConfig(app, version string, cfg map[string]string) *ServiceConfig {
	svcCfg := &ServiceConfig{
		ID:              time.Now().UnixNano(),
		Name:            app,
		Version:         version,
		Env:             cfg,
		versionVMap:     utils.NewVersionedMap(),
		environmentVMap: utils.NewVersionedMap(),
	}

	return svcCfg
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

	svcCfg := &ServiceConfig{
		Name:            path.Base(app),
		Env:             make(map[string]string),
		versionVMap:     utils.NewVersionedMap(),
		environmentVMap: utils.NewVersionedMap(),
	}

	matches, err := redis.Values(conn.Do("HGETALL", path.Join(r.Env, r.Pool, app, "environment")))
	if err != nil {
		return nil, err
	}

	// load environmentVMap from redis hash
	serialized := make(map[string]string)
	for i := 0; i < len(matches); i += 2 {
		key := string(matches[i].([]byte))
		value := string(matches[i+1].([]byte))
		serialized[key] = value
	}

	svcCfg.environmentVMap.UnmarshalMap(serialized)
	for _, k := range svcCfg.environmentVMap.Keys() {
		val := svcCfg.environmentVMap.Get(k)
		if val != "" {
			svcCfg.Env[k] = val
		}
	}

	matches, err = redis.Values(conn.Do("HGETALL", path.Join(r.Env, r.Pool, app, "version")))
	if err != nil {
		return nil, err
	}

	// load versionVMap from redis hash
	serialized = make(map[string]string)
	for i := 0; i < len(matches); i += 2 {
		key := string(matches[i].([]byte))
		value := string(matches[i+1].([]byte))
		serialized[key] = value
	}

	svcCfg.versionVMap.UnmarshalMap(serialized)
	svcCfg.Version = svcCfg.versionVMap.Get("version")

	return svcCfg, nil
}

func (r *ServiceRegistry) saveVersionedMap(key string, vmap *utils.VersionedMap) (string, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	serialized := vmap.MarshalMap()
	if len(serialized) > 0 {
		redisArgs := redis.Args{}.Add(key).AddFlat(serialized)
		created, err := conn.Do("HMSET", redisArgs...)
		if err != nil {
			return "", err
		}
		return created.(string), err
	}
	return "OK", nil
}

func (r *ServiceRegistry) SetServiceConfig(svcCfg *ServiceConfig) (bool, error) {

	for k, v := range svcCfg.Env {
		if svcCfg.environmentVMap.Get(k) != v {
			svcCfg.environmentVMap.Set(k, v, time.Now().UnixNano())
		}
	}

	if svcCfg.versionVMap.Get("version") != svcCfg.Version {
		svcCfg.versionVMap.Set("version", svcCfg.Version, time.Now().UnixNano())
	}

	//TODO: user MULTI/EXEC
	created, err := r.saveVersionedMap(path.Join(r.Env, r.Pool, svcCfg.Name, "environment"),
		svcCfg.environmentVMap)

	if err != nil {
		return false, err
	}

	if created == "OK" {
		created, err = r.saveVersionedMap(path.Join(r.Env, r.Pool, svcCfg.Name, "version"),
			svcCfg.versionVMap)

		if err != nil {
			return false, err
		}
	}

	return created == "OK", nil
}

func (r *ServiceRegistry) RegisterService(container *docker.Container, serviceConfig *ServiceConfig) error {
	registrationPath := path.Join(r.Env, r.Pool, "hosts", r.ensureHostname(), serviceConfig.Name)

	serviceRegistration := r.newServiceRegistration(container)

	jsonReg, err := json.Marshal(serviceRegistration)
	if err != nil {
		return err
	}

	conn := r.redisPool.Get()
	defer conn.Close()

	// TODO: use a compare-and-swap SCRIPT
	_, err = conn.Do("HMSET", registrationPath, "location", jsonReg)
	if err != nil {
		return err
	}

	_, err = conn.Do("EXPIRE", registrationPath, r.TTL)
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
		"In " + utils.HumanDuration(time.Duration(r.TTL)*time.Second),
	}, " | ")

	r.OutputBuffer.Log(statusLine)

	return nil
}

func (r *ServiceRegistry) UnRegisterService(container *docker.Container, serviceConfig *ServiceConfig) error {

	registrationPath := path.Join(r.Env, r.Pool, "hosts", r.ensureHostname(), serviceConfig.Name)

	conn := r.redisPool.Get()
	defer conn.Close()

	_, err := conn.Do("DEL", registrationPath)
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

func (r *ServiceRegistry) GetServiceRegistration(container *docker.Container, serviceConfig *ServiceConfig) (*ServiceRegistration, error) {
	desiredServiceRegistration := r.newServiceRegistration(container)
	regPath := path.Join(r.Env, r.Pool, "hosts", r.ensureHostname(), serviceConfig.Name)

	existingRegistration := ServiceRegistration{
		Path: regPath,
	}

	conn := r.redisPool.Get()
	defer conn.Close()

	val, err := conn.Do("HGET", regPath, "location")

	if err != nil {
		return nil, err
	}

	if val != nil {
		location, err := redis.Bytes(val, err)
		err = json.Unmarshal(location, &existingRegistration)
		if err != nil {
			return nil, err
		}

		if existingRegistration.Equals(*desiredServiceRegistration) {
			expires, err := redis.Int(conn.Do("TTL", regPath))
			if err != nil {
				return nil, err
			}
			existingRegistration.Expires = time.Now().Add(time.Duration(expires) * time.Second)
			return &existingRegistration, nil
		}
	}

	return nil, nil
}

func (r *ServiceRegistry) IsRegistered(container *docker.Container, serviceConfig *ServiceConfig) (bool, error) {

	reg, err := r.GetServiceRegistration(container, serviceConfig)
	return reg != nil, err
}

// We need an ID to start from, so we know when something has changed.
// Return nil,nil if mothing has changed (for now)
func (r *ServiceRegistry) Watch(lastID int64, changes chan *ConfigChange, stop chan struct{}) {

	go func() {

		lastVersion := make(map[string]int64)
		serviceConfigs, err := r.ListApps()
		if err != nil {
			changes <- &ConfigChange{
				Error: err,
			}
			return
		}

		for _, config := range serviceConfigs {
			lastVersion[config.Name] = config.ID
		}

		// TODO: default polling interval
		ticker := time.NewTicker(2 * time.Second)
		for {
			select {
			case <-stop:
				ticker.Stop()
				return
			case <-ticker.C:
				serviceConfigs, err := r.ListApps()
				if err != nil {
					changes <- &ConfigChange{
						Error: err,
					}
					break
				}
				for _, changedConfig := range serviceConfigs {
					if changedConfig.ID != lastVersion[changedConfig.Name] {
						lastVersion[changedConfig.Name] = changedConfig.ID
						changes <- &ConfigChange{
							ServiceConfig: &changedConfig,
						}

					}
				}
			}
		}
	}()
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
	matches, err := redis.Values(conn.Do("KEYS", path.Join(r.Env, r.Pool, app, "*")))
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

	emptyConfig := NewServiceConfig(app, "", make(map[string]string))

	// Always set an ENV env var for containers
	emptyConfig.Env["ENV"] = r.Env
	return r.SetServiceConfig(emptyConfig)
}

func (r *ServiceRegistry) DeleteApp(app string) (bool, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	deletedOne := false
	deleted, err := conn.Do("DEL", path.Join(r.Env, r.Pool, app))
	if err != nil {
		return false, err
	}

	deletedOne = deletedOne || deleted.(int64) == 1

	for _, k := range []string{"environment", "version"} {
		deleted, err = conn.Do("DEL", path.Join(r.Env, r.Pool, app, k))
		if err != nil {
			return false, err
		}
		deletedOne = deletedOne || deleted.(int64) == 1
	}

	return deletedOne, nil
}

func (r *ServiceRegistry) ListApps() ([]ServiceConfig, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	// TODO: convert to scan
	apps, err := redis.Strings(conn.Do("KEYS", path.Join(r.Env, r.Pool, "*", "environment")))
	if err != nil {
		return nil, err
	}

	// TODO: is it OK to error out early?
	var appList []ServiceConfig
	for _, app := range apps {
		parts := strings.Split(app, "/")

		// app entries should be 3 parts, /env/pool/app
		if len(parts) != 4 {
			continue
		}

		// we don't want host keys
		if parts[2] == "hosts" {
			continue
		}

		cfg, err := r.GetServiceConfig(parts[2])
		if err != nil {
			return nil, err
		}
		appList = append(appList, *cfg)
	}

	return appList, nil
}

// TODO: get all ServiceRegistrations
func (r *ServiceRegistry) ListRegistrations() ([]ServiceRegistration, error) { return nil, nil }
