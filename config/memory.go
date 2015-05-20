package config

import (
	"regexp"
	"strings"

	"github.com/litl/galaxy/utils"
)

type Value struct {
	value interface{}
	ttl   int
}

type MemoryBackend struct {
	maps        map[string]map[string]string
	apps        map[string][]App // env -> []app
	assignments map[string][]string

	AppExistsFunc       func(app, env string) (bool, error)
	CreateAppFunc       func(app, env string) (bool, error)
	GetAppFunc          func(app, env string) (App, error)
	UpdateAppFunc       func(svcCfg App, env string) (bool, error)
	DeleteAppFunc       func(svcCfg App, env string) (bool, error)
	ListAppFunc         func(env string) ([]AppConfig, error)
	AssignAppFunc       func(app, env, pool string) (bool, error)
	UnassignAppFunc     func(app, env, pool string) (bool, error)
	ListAssignmentsFunc func(env, pool string) ([]string, error)
	CreatePoolFunc      func(env, pool string) (bool, error)
	DeletePoolFunc      func(env, pool string) (bool, error)
	ListPoolsFunc       func(env string) ([]string, error)
	ListEnvsFunc        func() ([]string, error)
	ListHostsFunc       func(env, pool string) ([]HostInfo, error)

	MembersFunc      func(key string) ([]string, error)
	KeysFunc         func(key string) ([]string, error)
	AddMemberFunc    func(key, value string) (int, error)
	RemoveMemberFunc func(key, value string) (int, error)
	NotifyFunc       func(key, value string) (int, error)
	SetMultiFunc     func(key string, values map[string]string) (string, error)
}

func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{
		maps:        make(map[string]map[string]string),
		apps:        make(map[string][]App),
		assignments: make(map[string][]string),
	}
}

func (r *MemoryBackend) AppExists(app, env string) (bool, error) {
	if r.AppExistsFunc != nil {
		return r.AppExistsFunc(app, env)
	}

	for _, s := range r.apps[env] {
		if s.Name() == app {
			return true, nil
		}
	}
	return false, nil
}

func (r *MemoryBackend) CreateApp(app, env string) (bool, error) {

	if r.CreateAppFunc != nil {
		return r.CreateAppFunc(app, env)
	}

	if exists, err := r.AppExists(app, env); !exists && err == nil {
		r.apps[env] = append(r.apps[env], NewAppConfig(app, ""))
		return true, nil
	}

	return false, nil
}

func (r *MemoryBackend) ListApps(env string) ([]App, error) {
	return r.apps[env], nil
}

func (r *MemoryBackend) GetApp(app, env string) (App, error) {
	if r.GetAppFunc != nil {
		return r.GetAppFunc(app, env)
	}

	for _, cfg := range r.apps[env] {
		if cfg.Name() == app {
			return cfg, nil
		}
	}
	return nil, nil
}

func (r *MemoryBackend) UpdateApp(svcCfg App, env string) (bool, error) {
	if r.UpdateAppFunc != nil {
		return r.UpdateAppFunc(svcCfg, env)
	}
	return false, nil
}

func (r *MemoryBackend) DeleteApp(svcCfg App, env string) (bool, error) {
	if r.DeleteAppFunc != nil {
		return r.DeleteAppFunc(svcCfg, env)
	}

	cfgs := []App{}
	for _, cfg := range r.apps[env] {
		if cfg.Name() != svcCfg.Name() {
			cfgs = append(cfgs, cfg)
		}
	}
	r.apps[env] = cfgs
	return true, nil
}

func (r *MemoryBackend) AssignApp(app, env, pool string) (bool, error) {
	if r.AssignAppFunc != nil {
		return r.AssignAppFunc(app, env, pool)
	}

	key := env + "/" + pool
	if !utils.StringInSlice(app, r.assignments[key]) {
		r.assignments[key] = append(r.assignments[key], app)
	}

	return true, nil
}

func (r *MemoryBackend) UnassignApp(app, env, pool string) (bool, error) {
	if r.UnassignAppFunc != nil {
		return r.UnassignAppFunc(app, env, pool)
	}

	key := env + "/" + pool
	if !utils.StringInSlice(app, r.assignments[key]) {
		return false, nil
	}
	r.assignments[key] = utils.RemoveStringInSlice(app, r.assignments[key])

	return true, nil
}

func (r *MemoryBackend) ListAssignments(env, pool string) ([]string, error) {
	if r.ListAssignmentsFunc != nil {
		return r.ListAssignmentsFunc(env, pool)
	}

	key := env + "/" + pool
	return r.assignments[key], nil
}

func (r *MemoryBackend) CreatePool(env, pool string) (bool, error) {
	if r.CreatePoolFunc != nil {
		return r.CreatePoolFunc(env, pool)
	}

	key := env + "/" + pool
	r.assignments[key] = []string{}
	return true, nil
}

func (r *MemoryBackend) DeletePool(env, pool string) (bool, error) {
	if r.DeletePoolFunc != nil {
		return r.DeletePoolFunc(env, pool)
	}

	key := env + "/" + pool
	delete(r.assignments, key)
	return true, nil
}

func (r *MemoryBackend) ListPools(env string) ([]string, error) {
	if r.ListPoolsFunc != nil {
		return r.ListPools(env)
	}

	p := []string{}
	for k, _ := range r.assignments {
		parts := strings.Split(k, "/")
		p = append(p, parts[1])
	}
	return p, nil
}

func (r *MemoryBackend) ListEnvs() ([]string, error) {
	if r.ListEnvsFunc != nil {
		return r.ListEnvsFunc()
	}

	p := []string{}
	for k, _ := range r.assignments {
		parts := strings.Split(k, "/")
		env := parts[0]
		if !utils.StringInSlice(env, p) {
			p = append(p, parts[0])
		}
	}
	return p, nil
}

func (r *MemoryBackend) connect() {
}

func (r *MemoryBackend) reconnect() {
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

func (r *MemoryBackend) AddMember(key, value string) (int, error) {
	if r.AddMemberFunc != nil {
		return r.AddMemberFunc(key, value)
	}

	set := r.maps[key]
	if set == nil {
		set = make(map[string]string)
		r.maps[key] = set
	}
	set[value] = "1"
	return 1, nil
}

func (r *MemoryBackend) RemoveMember(key, value string) (int, error) {
	if r.RemoveMemberFunc != nil {
		return r.RemoveMemberFunc(key, value)
	}

	set := r.maps[key]
	if set == nil {
		return 0, nil
	}

	if _, ok := set[value]; ok {
		delete(set, value)
		return 1, nil
	}
	return 0, nil

}

func (r *MemoryBackend) Members(key string) ([]string, error) {
	if r.MembersFunc != nil {
		return r.MembersFunc(key)
	}

	values := []string{}
	set := r.maps[key]
	for v := range set {
		values = append(values, v)
	}
	return values, nil
}

func (r *MemoryBackend) Notify(key, value string) (int, error) {
	if r.NotifyFunc != nil {
		return r.NotifyFunc(key, value)
	}
	return 0, nil
}

func (r *MemoryBackend) Subscribe(key string) chan string {
	return make(chan string)
}

func (r *MemoryBackend) Set(key, field string, value string) (string, error) {
	return "OK", nil
}

func (r *MemoryBackend) Get(key, field string) (string, error) {
	return "", nil
}

func (r *MemoryBackend) GetAll(key string) (map[string]string, error) {
	return r.maps[key], nil
}

func (r *MemoryBackend) SetMulti(key string, values map[string]string) (string, error) {
	if r.SetMultiFunc != nil {
		return r.SetMultiFunc(key, values)
	}

	r.maps[key] = values
	return "OK", nil
}

func (r *MemoryBackend) DeleteMulti(key string, fields ...string) (int, error) {
	m := r.maps[key]
	for _, field := range fields {
		delete(m, field)
	}
	return len(fields), nil
}

func (r *MemoryBackend) UpdateHost(env, pool string, host HostInfo) error {
	panic("not implemented")
}

func (r *MemoryBackend) ListHosts(env, pool string) ([]HostInfo, error) {
	if r.ListHostsFunc != nil {
		return r.ListHostsFunc(env, pool)
	}
	panic("not implemented")
}

func (r *MemoryBackend) DeleteHost(env, pool string, host HostInfo) error {
	panic("not implemented")
}
