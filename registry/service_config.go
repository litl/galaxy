package registry

import (
	"path"
	"strconv"
	"strings"

	"github.com/litl/galaxy/utils"
)

type ServiceConfig struct {
	// ID is used for ordering and conflict resolution.
	// Usualy set to time.Now().UnixNano()
	Name            string `redis:"name"`
	versionVMap     *utils.VersionedMap
	environmentVMap *utils.VersionedMap
	portsVMap       *utils.VersionedMap
}

func NewServiceConfig(app, version string) *ServiceConfig {
	svcCfg := &ServiceConfig{
		Name:            app,
		versionVMap:     utils.NewVersionedMap(),
		environmentVMap: utils.NewVersionedMap(),
		portsVMap:       utils.NewVersionedMap(),
	}
	svcCfg.SetVersion(version)

	return svcCfg
}

func NewServiceConfigWithEnv(app, version string, env map[string]string) *ServiceConfig {
	svcCfg := NewServiceConfig(app, version)

	for k, v := range env {
		svcCfg.environmentVMap.Set(k, v)
	}

	return svcCfg
}

// Env returns a map representing the runtime environment for the container.
// Changes to this map have no effect.
func (s *ServiceConfig) Env() map[string]string {
	env := map[string]string{}
	for _, k := range s.environmentVMap.Keys() {
		val := s.environmentVMap.Get(k)
		if val != "" {
			env[k] = val
		}
	}
	return env
}

func (s *ServiceConfig) EnvSet(key, value string) {
	if s.environmentVMap.Get(key) != value {
		s.environmentVMap.SetVersion(key, value, s.nextID())
	}
}

func (s *ServiceConfig) EnvGet(key string) string {
	return s.environmentVMap.Get(key)
}

func (s *ServiceConfig) Version() string {
	return s.versionVMap.Get("version")
}

func (s *ServiceConfig) SetVersion(version string) {
	if s.versionVMap.Get("version") != version {
		s.versionVMap.SetVersion("version", version, s.nextID())
	}
}

func (s *ServiceConfig) Ports() map[string]string {
	ports := map[string]string{}
	for _, k := range s.portsVMap.Keys() {
		val := s.portsVMap.Get(k)
		if val != "" {
			ports[k] = val
		}
	}
	return ports
}

func (s *ServiceConfig) ClearPorts() {
	for _, k := range s.portsVMap.Keys() {
		s.portsVMap.SetVersion(k, "", s.nextID())
	}
}

func (s *ServiceConfig) AddPort(port, portType string) {
	s.portsVMap.Set(port, portType)
}

func (s *ServiceConfig) ID() int64 {
	id := int64(0)
	for _, vmap := range []*utils.VersionedMap{
		s.environmentVMap,
		s.versionVMap,
		s.portsVMap,
	} {
		if vmap.LatestVersion() > id {
			id = vmap.LatestVersion()
		}
	}
	return id
}

func (s *ServiceConfig) ContainerName() string {
	return s.Name + "_" + strconv.FormatInt(s.ID(), 10)
}

// IsContainerVersion takes a container name and return true if
// is is a container name that could be returned from this
// ServiceConfig
func (s *ServiceConfig) IsContainerVersion(name string) bool {
	if !strings.Contains(name, "_") {
		return false
	}

	parts := strings.Split(name, "_")
	name, version := parts[0], parts[1]
	if name != s.Name {
		return false
	}

	_, err := strconv.ParseUint(version, 10, 64)
	if err != nil {
		return false
	}
	return true
}

func (s *ServiceConfig) nextID() int64 {
	return s.ID() + 1
}

func (r *ServiceRegistry) GetServiceConfig(app string) (*ServiceConfig, error) {
	conn := r.redisPool.Get()
	defer conn.Close()

	exists, err := r.AppExists(app)
	if err != nil || !exists {
		return nil, err
	}

	svcCfg := NewServiceConfig(path.Base(app), "")

	err = r.loadVMap(path.Join(r.Env, app, "environment"), svcCfg.environmentVMap)
	if err != nil {
		return nil, err
	}
	err = r.loadVMap(path.Join(r.Env, app, "version"), svcCfg.versionVMap)
	if err != nil {
		return nil, err
	}

	err = r.loadVMap(path.Join(r.Env, app, "ports"), svcCfg.portsVMap)
	if err != nil {
		return nil, err
	}

	return svcCfg, nil
}

func (r *ServiceRegistry) SetServiceConfig(svcCfg *ServiceConfig) (bool, error) {

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
	err := r.saveVMap(path.Join(r.Env, svcCfg.Name, "environment"),
		svcCfg.environmentVMap)

	if err != nil {
		return false, err
	}

	err = r.saveVMap(path.Join(r.Env, svcCfg.Name, "version"),
		svcCfg.versionVMap)

	if err != nil {
		return false, err
	}

	err = r.saveVMap(path.Join(r.Env, r.Pool, svcCfg.Name, "ports"),
		svcCfg.portsVMap)

	if err != nil {
		return false, err
	}

	err = r.notifyChanged()
	if err != nil {
		return false, err
	}

	return true, nil
}
