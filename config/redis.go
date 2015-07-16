package config

import (
	"encoding/json"
	"errors"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/utils"
)

type RedisBackend struct {
	redisPool redis.Pool
	RedisHost string
}

func (r *RedisBackend) AppExists(app, env string) (bool, error) {
	matches, err := r.Keys(path.Join(env, app, "*"))
	if err != nil {
		return false, err
	}
	return len(matches) > 0, nil
}

func (r *RedisBackend) CreateApp(app, env string) (bool, error) {
	emptyConfig := NewAppConfig(app, "")
	return r.UpdateApp(emptyConfig, env)
}

func (r *RedisBackend) ListApps(env string) ([]App, error) {
	// TODO: convert to scan
	apps, err := r.Keys(path.Join(env, "*", "version"))
	if err != nil {
		return nil, err
	}

	// TODO: is it OK to error out early?
	var appList []App
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

		cfg, err := r.GetApp(parts[1], env)
		if err != nil {
			return nil, err
		}

		appList = append(appList, cfg)
	}

	return appList, nil
}

func (r *RedisBackend) UpdateApp(cfg App, env string) (bool, error) {
	svcCfg := cfg.(*AppConfig)
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
	err := r.SaveVMap(path.Join(env, svcCfg.name, "environment"),
		svcCfg.environmentVMap)

	if err != nil {
		return false, err
	}

	err = r.SaveVMap(path.Join(env, svcCfg.name, "version"),
		svcCfg.versionVMap)

	if err != nil {
		return false, err
	}

	err = r.SaveVMap(path.Join(env, svcCfg.name, "ports"),
		svcCfg.portsVMap)

	if err != nil {
		return false, err
	}

	err = r.SaveVMap(path.Join(env, svcCfg.name, "runtime"),
		svcCfg.runtimeVMap)

	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *RedisBackend) GetApp(app, env string) (App, error) {
	svcCfg := NewAppConfig(path.Base(app), "").(*AppConfig)

	err := r.LoadVMap(path.Join(env, app, "environment"), svcCfg.environmentVMap)
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

	err = r.LoadVMap(path.Join(env, app, "runtime"), svcCfg.runtimeVMap)
	if err != nil {
		return nil, err
	}
	return svcCfg, nil
}

func (r *RedisBackend) DeleteApp(svcCfg App, env string) (bool, error) {
	deletedOne := false
	deleted, err := r.Delete(path.Join(env, svcCfg.Name()))
	if err != nil {
		return false, err
	}

	deletedOne = deletedOne || deleted == 1

	for _, k := range []string{"environment", "version", "ports", "runtime"} {
		deleted, err = r.Delete(path.Join(env, svcCfg.Name(), k))
		if err != nil {
			return false, err
		}
		deletedOne = deletedOne || deleted == 1
	}

	return deletedOne, nil
}

func (r *RedisBackend) AssignApp(app, env, pool string) (bool, error) {
	added, err := r.AddMember(path.Join(env, "pools", pool), app)
	if err != nil {
		return false, err
	}
	return added == 1, err
}

func (r *RedisBackend) UnassignApp(app, env, pool string) (bool, error) {
	removed, err := r.RemoveMember(path.Join(env, "pools", pool), app)
	return removed == 1, err
}

func (r *RedisBackend) ListAssignments(env, pool string) ([]string, error) {
	return r.Members(path.Join(env, "pools", pool))
}

func (r *RedisBackend) CreatePool(env, pool string) (bool, error) {
	//FIXME: Create an associated auto-scaling groups tied to the
	//pool

	added, err := r.AddMember(path.Join(env, "pools", "*"), pool)
	return added == 1, err
}

func (r *RedisBackend) DeletePool(env, pool string) (bool, error) {
	apps, err := r.Members(path.Join(env, "pools", pool))
	if err != nil {
		return false, err
	}

	if len(apps) > 0 {
		return false, nil
	}

	_, err = r.RemoveMember(path.Join(env, "pools", "*"), pool)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *RedisBackend) ListPools(env string) ([]string, error) {
	// This is the host entry created by commander
	// when it starts up.  It can dynamically create
	// a pool
	key := path.Join(env, "*", "hosts", "*", "info")
	keys, err := r.Keys(key)
	if err != nil {
		return nil, err
	}

	pools := []string{}

	for _, k := range keys {
		parts := strings.Split(k, "/")
		pool := parts[1]
		if !utils.StringInSlice(pool, pools) {
			pools = append(pools, pool)
		}
	}

	// This is the pools that have been manually created.  It's
	// possible to assign an app to a pool that has no running
	// hosts so we add these to the pools list as well.
	key = path.Join(env, "pools", "*")
	keys, err = r.Keys(key)
	if err != nil {
		return nil, err
	}

	for _, k := range keys {
		parts := strings.Split(k, "/")
		pool := parts[2]

		if pool != "*" {
			continue
		}

		members, err := r.Members(k)
		if err != nil {
			return nil, err
		}

		for _, m := range members {
			if !utils.StringInSlice(m, pools) {
				pools = append(pools, m)
			}
		}
	}

	return pools, nil
}

func (r *RedisBackend) ListEnvs() ([]string, error) {
	envs := []string{}
	pools, err := r.Keys(path.Join("*", "pools", "*"))
	if err != nil {
		return nil, err
	}

	for _, pool := range pools {
		parts := strings.Split(pool, "/")
		if !utils.StringInSlice(parts[0], envs) {
			envs = append(envs, parts[0])
		}
	}
	return envs, nil
}

func (r *RedisBackend) LoadVMap(key string, dest *utils.VersionedMap) error {
	serialized, err := r.GetAll(key)
	if err != nil {
		return err
	}

	dest.UnmarshalMap(serialized)
	return nil
}

func (r *RedisBackend) SaveVMap(key string, vmap *utils.VersionedMap) error {

	serialized := vmap.MarshalMap()
	if len(serialized) == 0 {
		return nil
	}

	created, err := r.SetMulti(key, serialized)

	if err != nil {
		return err
	}

	if created != "OK" {
		return errors.New("not saved")
	}

	r.GcVMap(key, vmap)
	return nil
}

func (r *RedisBackend) GcVMap(key string, vmap *utils.VersionedMap) error {
	serialized := vmap.MarshalExpiredMap(5)
	if len(serialized) > 0 {
		keys := []string{}
		for k, _ := range serialized {
			keys = append(keys, k)
		}

		deleted, err := r.DeleteMulti(key, keys...)

		if err != nil {
			return err
		}

		if deleted != 1 {
			return errors.New("not deleted")
		}
	}
	return nil
}

func (r *RedisBackend) dialTimeout() (redis.Conn, error) {
	rwTimeout := 5 * time.Second
	return redis.DialTimeout("tcp", r.RedisHost, rwTimeout, rwTimeout, rwTimeout)
}

func (r *RedisBackend) testOnBorrow(c redis.Conn, t time.Time) error {
	_, err := c.Do("PING")
	if err != nil {
		defer c.Close()
	}
	return err
}

func (r *RedisBackend) connect() {
	r.redisPool = redis.Pool{
		MaxIdle:      1,
		IdleTimeout:  120 * time.Second,
		Dial:         r.dialTimeout,
		TestOnBorrow: r.testOnBorrow,
	}
}

// not needed with a redis.Pool
func (r *RedisBackend) reconnect() {}

func (r *RedisBackend) Keys(key string) ([]string, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if err := conn.Err(); err != nil {
		return nil, err
	}

	return redis.Strings(conn.Do("KEYS", key))
}

func (r *RedisBackend) Expire(key string, ttl uint64) (int, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if err := conn.Err(); err != nil {
		return 0, err
	}

	return redis.Int(conn.Do("EXPIRE", key, ttl))
}

func (r *RedisBackend) TTL(key string) (int, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if err := conn.Err(); err != nil {
		return 0, err
	}

	return redis.Int(conn.Do("TTL", key))
}

func (r *RedisBackend) Delete(key string) (int, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if err := conn.Err(); err != nil {
		return 0, err
	}

	return redis.Int(conn.Do("DEL", key))
}

func (r *RedisBackend) AddMember(key, value string) (int, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if err := conn.Err(); err != nil {
		return 0, err
	}

	return redis.Int(conn.Do("SADD", key, value))
}

func (r *RedisBackend) RemoveMember(key, value string) (int, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if err := conn.Err(); err != nil {
		return 0, err
	}

	return redis.Int(conn.Do("SREM", key, value))
}

func (r *RedisBackend) Members(key string) ([]string, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if err := conn.Err(); err != nil {
		return nil, err
	}

	return redis.Strings(conn.Do("SMEMBERS", key))
}

func (r *RedisBackend) Notify(key, value string) (int, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if err := conn.Err(); err != nil {
		return 0, err
	}

	return redis.Int(conn.Do("PUBLISH", key, value))
}

func (r *RedisBackend) subscribeChannel(key string, msgs chan string) {
	var wg sync.WaitGroup

	redisPool := redis.Pool{
		MaxIdle:     1,
		IdleTimeout: 0,
		Dial: func() (redis.Conn, error) {
			return redis.DialTimeout("tcp", r.RedisHost, time.Second, 0, 0)
		},
		// test every connection for now
		TestOnBorrow: r.testOnBorrow,
	}

	for {
		conn := redisPool.Get()
		// no defer, doesn't return
		if err := conn.Err(); err != nil {
			conn.Close()
			log.Printf("ERROR: %v\n", err)
			time.Sleep(5 * time.Second)
			continue
		}

		wg.Add(1)
		psc := redis.PubSubConn{Conn: conn}
		go func() {
			defer wg.Done()
			for {
				switch n := psc.Receive().(type) {
				case redis.Message:
					msg := string(n.Data)
					msgs <- msg
				case error:
					psc.Close()
					log.Printf("ERROR: %v\n", n)
					return
				}
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			psc.Subscribe(key)
			log.Printf("Monitoring for config changes on channel: %s\n", key)
		}()
		wg.Wait()
		conn.Close()
	}
}

func (r *RedisBackend) Subscribe(key string) chan string {
	msgs := make(chan string)
	go r.subscribeChannel(key, msgs)
	return msgs
}

func (r *RedisBackend) Set(key, field string, value string) (string, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if err := conn.Err(); err != nil {
		return "", err
	}

	return redis.String(conn.Do("HMSET", key, field, value))
}

func (r *RedisBackend) Get(key, field string) (string, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if err := conn.Err(); err != nil {
		return "", err
	}

	ret, err := redis.String(conn.Do("HGET", key, field))
	if err != nil && err == redis.ErrNil {
		return "", nil
	}

	return ret, err
}

func (r *RedisBackend) GetAll(key string) (map[string]string, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if err := conn.Err(); err != nil {
		return nil, err
	}

	matches, err := redis.Values(conn.Do("HGETALL", key))
	if err != nil {
		return nil, err
	}

	serialized := make(map[string]string)
	for i := 0; i < len(matches); i += 2 {
		key := string(matches[i].([]byte))
		value := string(matches[i+1].([]byte))
		serialized[key] = value
	}
	return serialized, nil

}

func (r *RedisBackend) SetMulti(key string, values map[string]string) (string, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if err := conn.Err(); err != nil {
		return "", err
	}

	redisArgs := redis.Args{}.Add(key).AddFlat(values)
	return redis.String(conn.Do("HMSET", redisArgs...))
}

func (r *RedisBackend) DeleteMulti(key string, fields ...string) (int, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if err := conn.Err(); err != nil {
		return 0, err
	}

	args := []string{}
	for _, field := range fields {
		args = append(args, field)
	}
	redisArgs := redis.Args{}.Add(key).AddFlat(args)
	return redis.Int(conn.Do("HDEL", redisArgs...))

}

func (r *RedisBackend) DeleteHost(env, pool string, host HostInfo) error {
	key := path.Join(env, pool, "hosts", host.HostIP, "info")
	_, err := r.Delete(key)
	return err
}

func (r *RedisBackend) UpdateHost(env, pool string, host HostInfo) error {
	key := path.Join(env, pool, "hosts", host.HostIP, "info")
	existing := utils.NewVersionedMap()

	err := r.LoadVMap(key, existing)
	if err != nil {
		return err
	}

	save := false
	if existing.Get("HostIP") != host.HostIP {
		existing.Set("HostIP", host.HostIP)
		save = true
	}

	if save {
		err = r.SaveVMap(key, existing)
		if err != nil {
			return err
		}
	}

	_, err = r.Expire(key, DefaultTTL)
	return err
}

func (r *RedisBackend) ListHosts(env, pool string) ([]HostInfo, error) {
	key := path.Join(env, pool, "hosts", "*", "info")
	keys, err := r.Keys(key)
	if err != nil {
		return nil, err
	}

	hosts := []HostInfo{}

	for _, k := range keys {
		existing := utils.NewVersionedMap()

		err := r.LoadVMap(k, existing)
		if err != nil {
			return nil, err
		}
		hosts = append(hosts, HostInfo{
			HostIP: existing.Get("HostIP"),
		})
	}
	return hosts, nil
}

func (r *RedisBackend) RegisterService(env, pool string, reg *ServiceRegistration) error {
	registrationPath := path.Join(env, pool, "hosts", reg.ExternalIP, reg.Name, reg.ContainerID[0:12])

	jsonReg, err := json.Marshal(reg)
	if err != nil {
		return err
	}

	_, err = r.Set(registrationPath, "location", string(jsonReg))
	if err != nil {
		return err
	}

	_, err = r.Expire(registrationPath, DefaultTTL)

	if err != nil {
		return err
	}
	return nil
}

func (r *RedisBackend) UnregisterService(env, pool, hostIP, name, containerID string) (*ServiceRegistration, error) {
	registrationPath := path.Join(env, pool, "hosts", hostIP, name, containerID[0:12])

	registration, err := r.GetServiceRegistration(env, pool, hostIP, name, containerID)
	if err != nil || registration == nil {
		return registration, err
	}

	if registration.ContainerID != containerID {
		return nil, nil
	}

	_, err = r.Delete(registrationPath)
	if err != nil {
		return registration, err
	}

	return registration, nil
}

func (r *RedisBackend) GetServiceRegistration(env, pool, hostIP, name, containerID string) (*ServiceRegistration, error) {
	regPath := path.Join(env, pool, "hosts", hostIP, name, containerID[0:12])

	existingRegistration := ServiceRegistration{
		Path: regPath,
	}

	location, err := r.Get(regPath, "location")

	if err != nil {
		return nil, err
	}

	if location == "" {
		return nil, nil
	}

	err = json.Unmarshal([]byte(location), &existingRegistration)
	if err != nil {
		return nil, err
	}

	expires, err := r.TTL(regPath)
	if err != nil {
		return nil, err
	}
	existingRegistration.Expires = time.Now().UTC().Add(time.Duration(expires) * time.Second)
	return &existingRegistration, nil
}

func (r *RedisBackend) ListRegistrations(env string) ([]ServiceRegistration, error) {
	keys, err := r.Keys(path.Join(env, "*", "hosts", "*", "*", "*"))
	if err != nil {
		return nil, err
	}

	var regList []ServiceRegistration
	for _, key := range keys {

		pool := strings.Split(key, "/")[1]

		val, err := r.Get(key, "location")
		if err != nil {
			log.Warnf("WARN: Unable to get location for %s: %s", key, err)
			continue
		}

		svcReg := ServiceRegistration{
			Name: path.Base(key),
			Pool: pool,
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
