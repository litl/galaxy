package registry

import (
	"time"

	"github.com/garyburd/redigo/redis"
)

type RedisBackend struct {
	redisPool redis.Pool
	RedisHost string
}

func (r *RedisBackend) Connect() {
	rwTimeout := 5 * time.Second

	r.redisPool = redis.Pool{
		MaxIdle:     1,
		IdleTimeout: 120 * time.Second,
		Dial: func() (redis.Conn, error) {
			return redis.DialTimeout("tcp", r.RedisHost, rwTimeout, rwTimeout, rwTimeout)
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
}

func (r *RedisBackend) Reconnect() {
	r.redisPool.Close()
	r.Connect()
}

func (r *RedisBackend) Keys(key string) ([]string, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if conn.Err() != nil {
		conn.Close()
		r.Reconnect()
		return nil, conn.Err()
	}

	return redis.Strings(conn.Do("KEYS", key))
}

func (r *RedisBackend) Expire(key string, ttl uint64) (int, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if conn.Err() != nil {
		conn.Close()
		r.Reconnect()
		return 0, conn.Err()
	}

	return redis.Int(conn.Do("EXPIRE", key, ttl))
}

func (r *RedisBackend) Ttl(key string) (int, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if conn.Err() != nil {
		conn.Close()
		r.Reconnect()
		return 0, conn.Err()
	}

	return redis.Int(conn.Do("TTL", key))
}

func (r *RedisBackend) Delete(key string) (int, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if conn.Err() != nil {
		conn.Close()
		r.Reconnect()
		return 0, conn.Err()
	}

	return redis.Int(conn.Do("DEL", key))
}

func (r *RedisBackend) AddMember(key, value string) (int, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if conn.Err() != nil {
		conn.Close()
		r.Reconnect()
		return 0, conn.Err()
	}

	return redis.Int(conn.Do("SADD", key, value))
}

func (r *RedisBackend) RemoveMember(key, value string) (int, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if conn.Err() != nil {
		conn.Close()
		r.Reconnect()
		return 0, conn.Err()
	}

	return redis.Int(conn.Do("SREM", key, value))
}

func (r *RedisBackend) Members(key string) ([]string, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if conn.Err() != nil {
		conn.Close()
		r.Reconnect()
		return nil, conn.Err()
	}

	return redis.Strings(conn.Do("SMEMBERS", key))
}

func (r *RedisBackend) Notify(key, value string) (int, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if conn.Err() != nil {
		conn.Close()
		r.Reconnect()
		return 0, conn.Err()
	}

	return redis.Int(conn.Do("PUBLISH", key, value))
}

func (r *RedisBackend) Set(key, field string, value []byte) (string, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if conn.Err() != nil {
		conn.Close()
		r.Reconnect()
		return "", conn.Err()
	}

	return redis.String(conn.Do("HMSET", key, field, value))
}

func (r *RedisBackend) Get(key, field string) ([]byte, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if conn.Err() != nil {
		conn.Close()
		r.Reconnect()
		return []byte{}, conn.Err()
	}

	return redis.Bytes(conn.Do("HGET", key, field))
}
