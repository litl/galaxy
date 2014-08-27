package registry

import (
	"log"
	"sync"
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

func (r *RedisBackend) subscribeChannel(key string, msgs chan string) {
	var wg sync.WaitGroup

	newPubSubPool := func() redis.Pool {
		return redis.Pool{
			MaxIdle:     1,
			IdleTimeout: 0,
			Dial: func() (redis.Conn, error) {
				c, err := redis.Dial("tcp", r.RedisHost)
				if err != nil {
					return nil, err
				}
				return c, err
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

	redisPool := newPubSubPool()
	for {

		conn := redisPool.Get()
		defer conn.Close()
		if conn.Err() != nil {
			conn.Close()
			redisPool.Close()
			log.Printf("ERROR: %v\n", conn.Err())
			time.Sleep(5 * time.Second)
			redisPool = newPubSubPool()
			continue
		}

		wg.Add(2)
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
					redisPool.Close()
					log.Printf("ERROR: %v\n", n)
					return
				}
			}
		}()

		go func() {
			defer wg.Done()
			psc.Subscribe(key)
			log.Printf("Monitoring for config changes on channel: %s\n", key)
		}()
		wg.Wait()
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

	if conn.Err() != nil {
		conn.Close()
		r.Reconnect()
		return "", conn.Err()
	}

	return redis.String(conn.Do("HMSET", key, field, value))
}

func (r *RedisBackend) Get(key, field string) (string, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if conn.Err() != nil {
		conn.Close()
		r.Reconnect()
		return "", conn.Err()
	}

	return redis.String(conn.Do("HGET", key, field))
}

func (r *RedisBackend) GetAll(key string) (map[string]string, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if conn.Err() != nil {
		conn.Close()
		r.Reconnect()
		return nil, conn.Err()
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

	if conn.Err() != nil {
		conn.Close()
		r.Reconnect()
		return "", conn.Err()
	}

	redisArgs := redis.Args{}.Add(key).AddFlat(values)
	return redis.String(conn.Do("HMSET", redisArgs...))
}

func (r *RedisBackend) DeleteMulti(key string, fields ...string) (int, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	if conn.Err() != nil {
		conn.Close()
		r.Reconnect()
		return 0, conn.Err()
	}

	args := []string{}
	for _, field := range fields {
		args = append(args, field)
	}
	redisArgs := redis.Args{}.Add(key).AddFlat(args)
	return redis.Int(conn.Do("HDEL", redisArgs...))

}
