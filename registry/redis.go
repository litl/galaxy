package registry

import "github.com/garyburd/redigo/redis"

type RedisBackend struct {
	redisPool redis.Pool
}

func (r *RedisBackend) Keys(key string) ([]string, error) {
	conn := r.redisPool.Get()
	defer conn.Close()
	return redis.Strings(conn.Do("KEYS", key))
}

func (r *RedisBackend) AddMember(key, value string) (int, error) {
	conn := r.redisPool.Get()
	defer conn.Close()
	return redis.Int(conn.Do("SADD", key, value))
}

func (r *RedisBackend) RemoveMember(key, value string) (int, error) {
	conn := r.redisPool.Get()
	defer conn.Close()
	return redis.Int(conn.Do("SREM", key, value))
}

func (r *RedisBackend) Members(key string) ([]string, error) {
	conn := r.redisPool.Get()
	defer conn.Close()
	return redis.Strings(conn.Do("SMEMBERS", key))
}

func (r *RedisBackend) Notify(key, value string) (int, error) {
	conn := r.redisPool.Get()
	defer conn.Close()
	return redis.Int(conn.Do("PUBLISH", key, value))
}
