package registry

import (
	"regexp"
	"strings"
)

type Value struct {
	value interface{}
	ttl   int
}

type MemoryBackend struct {
	maps map[string]map[string]string

	MembersFunc      func(key string) ([]string, error)
	KeysFunc         func(key string) ([]string, error)
	AddMemberFunc    func(key, value string) (int, error)
	RemoveMemberFunc func(key, value string) (int, error)
	NotifyFunc       func(key, value string) (int, error)
	SetMultiFunc     func(key string, values map[string]string) (string, error)
}

func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{
		maps: make(map[string]map[string]string),
	}
}

func (r *MemoryBackend) Connect() {
}

func (r *MemoryBackend) Reconnect() {
}

func (r *MemoryBackend) Keys(key string) ([]string, error) {
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

func (r *MemoryBackend) Expire(key string, ttl uint64) (int, error) {
	return 0, nil
}

func (r *MemoryBackend) TTL(key string) (int, error) {
	return 0, nil
}

func (r *MemoryBackend) Delete(key string) (int, error) {
	if _, ok := r.maps[key]; ok {
		delete(r.maps, key)
		return 1, nil
	}
	return 0, nil
}

func (r *MemoryBackend) Set(key, field string, value string) (string, error) {
	return "OK", nil
}

func (r *MemoryBackend) Get(key, field string) (string, error) {
	return "", nil
}
