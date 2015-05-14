package config

import (
	"regexp"
	"strings"
)

type RegMemBackend struct {
	maps map[string]map[string]string

	MembersFunc      func(key string) ([]string, error)
	KeysFunc         func(key string) ([]string, error)
	AddMemberFunc    func(key, value string) (int, error)
	RemoveMemberFunc func(key, value string) (int, error)
	NotifyFunc       func(key, value string) (int, error)
	SetMultiFunc     func(key string, values map[string]string) (string, error)
}

func NewRegMemBackend() *RegMemBackend {
	return &RegMemBackend{
		maps: make(map[string]map[string]string),
	}
}

func (r *RegMemBackend) Connect() {
}

func (r *RegMemBackend) Reconnect() {
}

func (r *RegMemBackend) Keys(key string) ([]string, error) {
	if r.KeysFunc != nil {
		return r.KeysFunc(key)
	}

	keys := []string{}
	rp := strings.NewReplacer("*", `.*`)
	p := rp.Replace(key)

	re := regexp.MustCompile(p)
	for k := range r.maps {
		if re.MatchString(k) {
			keys = append(keys, k)
		}
	}

	return keys, nil
}

func (r *RegMemBackend) Expire(key string, ttl uint64) (int, error) {
	return 0, nil
}

func (r *RegMemBackend) TTL(key string) (int, error) {
	return 0, nil
}

func (r *RegMemBackend) Delete(key string) (int, error) {
	if _, ok := r.maps[key]; ok {
		delete(r.maps, key)
		return 1, nil
	}
	return 0, nil
}

func (r *RegMemBackend) Set(key, field string, value string) (string, error) {
	return "OK", nil
}

func (r *RegMemBackend) Get(key, field string) (string, error) {
	return "", nil
}
