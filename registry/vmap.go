package registry

import (
	"errors"

	"github.com/garyburd/redigo/redis"
	"github.com/litl/galaxy/utils"
)

func (r *ServiceRegistry) loadVMap(key string, dest *utils.VersionedMap) error {
	conn := r.redisPool.Get()
	defer conn.Close()

	matches, err := redis.Values(conn.Do("HGETALL", key))
	if err != nil {
		return err
	}

	serialized := make(map[string]string)
	for i := 0; i < len(matches); i += 2 {
		key := string(matches[i].([]byte))
		value := string(matches[i+1].([]byte))
		serialized[key] = value
	}

	dest.UnmarshalMap(serialized)
	return nil
}

func (r *ServiceRegistry) saveVMap(key string, vmap *utils.VersionedMap) error {
	conn := r.redisPool.Get()
	defer conn.Close()

	serialized := vmap.MarshalMap()
	if len(serialized) == 0 {
		return nil
	}

	redisArgs := redis.Args{}.Add(key).AddFlat(serialized)
	created, err := redis.String(conn.Do("HMSET", redisArgs...))

	if err != nil {
		return err
	}

	if created != "OK" {
		return errors.New("not saved")
	}

	r.gcVMap(key, vmap)
	return nil
}

func (r *ServiceRegistry) gcVMap(key string, vmap *utils.VersionedMap) error {
	conn := r.redisPool.Get()
	defer conn.Close()

	serialized := vmap.MarshalExpiredMap(5)
	if len(serialized) > 0 {
		keys := []string{}
		for k, _ := range serialized {
			keys = append(keys, k)
		}
		redisArgs := redis.Args{}.Add(key).AddFlat(keys)
		deleted, err := redis.Int(conn.Do("HDEL", redisArgs...))

		if err != nil {
			return err
		}

		if deleted != 1 {
			return errors.New("not deleted")
		}
	}
	return nil
}
