package config

import (
	"fmt"
	"strings"
	"testing"

	"github.com/garyburd/redigo/redis"
)

type TestConn struct {
	History   []string
	CloseFn   func() error
	ErrFn     func() error
	DoFn      func(commandName string, args ...interface{}) (reply interface{}, err error)
	SendFn    func(commandName string, args ...interface{}) error
	FlushFn   func() error
	ReceiveFn func() (reply interface{}, err error)
}

func (t *TestConn) Record(cmd string, args ...interface{}) {
	sa := []string{}
	for _, v := range args {
		if v == nil {
			continue
		}
		sa = append(sa, v.(string))
	}
	t.History = append(t.History, fmt.Sprintf("%s %s", cmd, strings.Join(sa, " ")))
}

func (t *TestConn) Close() error {
	t.Record("Close()", nil)
	if t.CloseFn != nil {
		return t.CloseFn()
	}
	return nil
}

func (t *TestConn) Err() error {
	t.Record("Err()", nil)
	if t.ErrFn != nil {
		return t.ErrFn()
	}
	return nil
}

func (t *TestConn) Do(commandName string, args ...interface{}) (reply interface{}, err error) {
	t.Record(commandName, args...)

	if t.DoFn != nil {
		return t.DoFn(commandName, args...)
	}
	return nil, nil
}

func (t *TestConn) Send(commandName string, args ...interface{}) error {
	t.Record(commandName, args...)

	if t.SendFn != nil {
		return t.SendFn(commandName, args...)
	}
	return nil
}

func (t *TestConn) Flush() error {
	t.Record("Flush()", nil)

	if t.FlushFn != nil {
		return t.FlushFn()
	}
	return nil
}

func (t *TestConn) Receive() (reply interface{}, err error) {
	t.Record("Receive()", nil)
	if t.ReceiveFn != nil {
		return t.ReceiveFn()
	}
	return nil, nil
}

func NewTestRedisBackend() (*RedisBackend, *TestConn) {
	c := &TestConn{}
	return &RedisBackend{
		redisPool: redis.Pool{
			Dial: func() (redis.Conn, error) {
				return c, nil
			},
		},
	}, c
}

func TestAppExistsKeyFormat(t *testing.T) {

	r, c := NewTestRedisBackend()
	r.AppExists("foo", "dev")
	assertInHistory(t, c.History, "KEYS dev/foo/*")
}

func assertInHistory(t *testing.T, history []string, cmd string) {
	found := false
	for _, v := range history {
		if v == cmd {
			found = true
		}
	}
	if !found {
		t.Fatalf("Expected %s in [%s]", cmd, strings.Join(history, ","))
	}
}
