package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/litl/galaxy/utils"
)

// Interface to wrap AppConfig, so that it can be swapped out with a different
// Backend. This should be temporary as many of these methods won't be useful.
type App interface {
	Name() string
	Env() map[string]string
	EnvSet(key, value string)
	EnvGet(key string) string
	Version() string
	SetVersion(version string)
	VersionID() string
	SetVersionID(versionID string)
	ID() int64
	ContainerName() string
	SetProcesses(pool string, count int)
	GetProcesses(pool string) int
	RuntimePools() []string
	SetMemory(pool string, mem string)
	GetMemory(pool string) string
	SetCPUShares(pool string, cpu string)
	GetCPUShares(pool string) string

	/* TODO: Remove Ports: they don't seem to be used
	Ports() map[string]string
	ClearPorts()
	AddPort(port, portType string)
	*/

}

type AppConfig struct {
	// ID is used for ordering and conflict resolution.
	// Usualy set to time.Now().UnixNano()
	name            string `redis:"name"`
	versionVMap     *utils.VersionedMap
	environmentVMap *utils.VersionedMap
	portsVMap       *utils.VersionedMap
	runtimeVMap     *utils.VersionedMap
}

func NewAppConfig(app, version string) App {
	svcCfg := &AppConfig{
		name:            app,
		versionVMap:     utils.NewVersionedMap(),
		environmentVMap: utils.NewVersionedMap(),
		portsVMap:       utils.NewVersionedMap(),
		runtimeVMap:     utils.NewVersionedMap(),
	}
	svcCfg.SetVersion(version)

	return svcCfg
}

func NewAppConfigWithEnv(app, version string, env map[string]string) App {
	svcCfg := NewAppConfig(app, version).(*AppConfig)

	for k, v := range env {
		svcCfg.environmentVMap.Set(k, v)
	}

	return svcCfg
}

func (s *AppConfig) Name() string {
	return s.name
}

// Env returns a map representing the runtime environment for the container.
// Changes to this map have no effect.
func (s *AppConfig) Env() map[string]string {
	env := map[string]string{}
	for _, k := range s.environmentVMap.Keys() {
		val := s.environmentVMap.Get(k)
		if val != "" {
			env[k] = val
		}
	}
	return env
}

func (s *AppConfig) EnvSet(key, value string) {
	s.environmentVMap.SetVersion(key, value, s.nextID())
}

func (s *AppConfig) EnvGet(key string) string {
	return s.environmentVMap.Get(key)
}

func (s *AppConfig) Version() string {
	return s.versionVMap.Get("version")
}

func (s *AppConfig) SetVersion(version string) {
	s.versionVMap.SetVersion("version", version, s.nextID())
}

func (s *AppConfig) VersionID() string {
	return s.versionVMap.Get("versionID")
}

func (s *AppConfig) SetVersionID(versionID string) {
	s.versionVMap.SetVersion("versionID", versionID, s.nextID())
}

func (s *AppConfig) Ports() map[string]string {
	ports := map[string]string{}
	for _, k := range s.portsVMap.Keys() {
		val := s.portsVMap.Get(k)
		if val != "" {
			ports[k] = val
		}
	}
	return ports
}

func (s *AppConfig) ClearPorts() {
	for _, k := range s.portsVMap.Keys() {
		s.portsVMap.SetVersion(k, "", s.nextID())
	}
}

func (s *AppConfig) AddPort(port, portType string) {
	s.portsVMap.Set(port, portType)
}

func (s *AppConfig) ID() int64 {
	id := int64(0)
	for _, vmap := range []*utils.VersionedMap{
		s.environmentVMap,
		s.versionVMap,
		s.portsVMap,
		s.runtimeVMap,
	} {
		if vmap.LatestVersion() > id {
			id = vmap.LatestVersion()
		}
	}
	return id
}

func (s *AppConfig) ContainerName() string {
	return s.name + "_" + strconv.FormatInt(s.ID(), 10)
}

func (s *AppConfig) nextID() int64 {
	return s.ID() + 1
}

func (s *AppConfig) SetProcesses(pool string, count int) {
	key := fmt.Sprintf("%s-ps", pool)
	s.runtimeVMap.SetVersion(key, strconv.FormatInt(int64(count), 10), s.nextID())
}

func (s *AppConfig) GetProcesses(pool string) int {
	key := fmt.Sprintf("%s-ps", pool)
	ps := s.runtimeVMap.Get(key)
	if ps == "" {
		return -1
	}
	count, _ := strconv.ParseInt(ps, 10, 16)
	return int(count)
}

func (s *AppConfig) RuntimePools() []string {
	keys := s.runtimeVMap.Keys()
	pools := []string{}
	for _, k := range keys {
		pool := k[:strings.Index(k, "-")]
		if !utils.StringInSlice(pool, pools) {
			pools = append(pools, pool)
		}
	}
	return pools
}

func (s *AppConfig) SetMemory(pool string, mem string) {
	key := fmt.Sprintf("%s-mem", pool)
	s.runtimeVMap.SetVersion(key, mem, s.nextID())
}

func (s *AppConfig) GetMemory(pool string) string {
	key := fmt.Sprintf("%s-mem", pool)
	return s.runtimeVMap.Get(key)
}

func (s *AppConfig) SetCPUShares(pool string, cpu string) {
	key := fmt.Sprintf("%s-cpu", pool)
	s.runtimeVMap.SetVersion(key, cpu, s.nextID())
}

func (s *AppConfig) GetCPUShares(pool string) string {
	key := fmt.Sprintf("%s-cpu", pool)
	return s.runtimeVMap.Get(key)
}
