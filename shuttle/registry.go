package main

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/litl/galaxy/log"
)

var (
	ErrNoService        = fmt.Errorf("service does not exist")
	ErrNoBackend        = fmt.Errorf("backend does not exist")
	ErrDuplicateService = fmt.Errorf("service already exists")
	ErrDuplicateBackend = fmt.Errorf("backend already exists")
)

// marshal whatever we've got with out default indentation
// swallowing errors.
func marshal(i interface{}) []byte {
	jsonBytes, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		log.Println("error encoding json:", err)
	}
	return append(jsonBytes, '\n')
}

// ServiceRegistry is a global container for all configured services.
type ServiceRegistry struct {
	sync.Mutex
	svcs map[string]*Service
}

func (s *ServiceRegistry) GetService(name string) *Service {
	s.Lock()
	defer s.Unlock()
	return s.svcs[name]
}

// Add a new service to the Registry.
// Do not replace an existing service.
func (s *ServiceRegistry) AddService(cfg ServiceConfig) error {
	s.Lock()
	defer s.Unlock()

	log.Debug("Adding service:", cfg.Name)
	if _, ok := s.svcs[cfg.Name]; ok {
		log.Debug("Service already exists:", cfg.Name)
		return ErrDuplicateService
	}

	service := NewService(cfg)
	s.svcs[service.Name] = service

	return service.start()
}

// Replace the service's configuration, or update its list of backends.
// Replacing a configuration will shutdown the existing service, and start a
// new one, which will cause the listening socket to be temporarily
// unavailable.
func (s *ServiceRegistry) UpdateService(newCfg ServiceConfig, backendsOnly bool) error {
	s.Lock()
	defer s.Unlock()

	log.Debug("Updating Service:", newCfg.Name)
	service, ok := s.svcs[newCfg.Name]
	if !ok {
		log.Debug("Service not found:", newCfg.Name)
		return ErrNoService
	}

	currentCfg := service.Config()

	// if we're not doing only the backends, just wipe the service and start fresh.
	// No need to stop the service if nothing has changed though.
	if !backendsOnly && !currentCfg.Equal(newCfg) {
		log.Debug("Replacing Service:", service.Name)
		delete(s.svcs, service.Name)
		service.stop()

		service = NewService(newCfg)
		s.svcs[service.Name] = service
		return service.start()
	}

	// Lots of looping here (including fetching the Config, but the cardinality
	// of Backends shouldn't be very large, and the default RoundRobin balancing
	// is much simpler with a slice.

	// we're going to update just the backends for this config
	// get a map of what's already running
	currentBackends := make(map[string]BackendConfig)
	for _, backendCfg := range currentCfg.Backends {
		currentBackends[backendCfg.Name] = backendCfg
	}

	// Keep existing backends when they have equivalent config.
	// Update changed backends, and add new ones.
	for _, newBackend := range newCfg.Backends {
		current, ok := currentBackends[newBackend.Name]
		if ok && current.Equal(newBackend) {
			log.Debugf("Backend %s/%s unchanged", service.Name, current.Name)
			// no change for this one
			delete(currentBackends, current.Name)
			continue
		}

		log.Debugf("Adding Backend %s/%s", service.Name, current.Name)
		service.add(NewBackend(current))
		delete(currentBackends, current.Name)
	}

	// remove any left over backends
	for name := range currentBackends {
		log.Debugf("Removing Backend %s/%s", service.Name, name)
		service.remove(name)
	}
	return nil
}

func (s *ServiceRegistry) RemoveService(name string) error {
	s.Lock()
	defer s.Unlock()

	svc, ok := s.svcs[name]
	if ok {
		log.Debugf("Removing Service %s", svc.Name)
		delete(s.svcs, name)
		svc.stop()
		return nil
	}
	return ErrNoService
}

func (s *ServiceRegistry) ServiceStats(serviceName string) (ServiceStat, error) {
	s.Lock()
	defer s.Unlock()

	service, ok := s.svcs[serviceName]
	if !ok {
		return ServiceStat{}, ErrNoService
	}
	return service.Stats(), nil
}

func (s *ServiceRegistry) ServiceConfig(serviceName string) (ServiceConfig, error) {
	s.Lock()
	defer s.Unlock()

	service, ok := s.svcs[serviceName]
	if !ok {
		return ServiceConfig{}, ErrNoService
	}
	return service.Config(), nil
}

func (s *ServiceRegistry) BackendStats(serviceName, backendName string) (BackendStat, error) {
	s.Lock()
	defer s.Unlock()

	service, ok := s.svcs[serviceName]
	if !ok {
		return BackendStat{}, ErrNoService
	}

	for _, backend := range service.Backends {
		if backendName == backend.Name {
			return backend.Stats(), nil
		}
	}
	return BackendStat{}, ErrNoBackend
}

// Add or update a Backend on an existing Service.
func (s *ServiceRegistry) AddBackend(svcName string, backendCfg BackendConfig) error {
	s.Lock()
	defer s.Unlock()

	service, ok := s.svcs[svcName]
	if !ok {
		return ErrNoService
	}

	log.Debugf("Adding Backend %s/%s", service.Name, backendCfg.Name)
	service.add(NewBackend(backendCfg))
	return nil
}

// Remove a Backend from an existing Service.
func (s *ServiceRegistry) RemoveBackend(svcName, backendName string) error {
	s.Lock()
	defer s.Unlock()

	log.Debugf("Removing Backend %s/%s", svcName, backendName)
	service, ok := s.svcs[svcName]
	if !ok {
		return ErrNoService
	}

	if !service.remove(backendName) {
		return ErrNoBackend
	}
	return nil
}

func (s *ServiceRegistry) Stats() []ServiceStat {
	s.Lock()
	defer s.Unlock()

	stats := []ServiceStat{}
	for _, service := range s.svcs {
		stats = append(stats, service.Stats())
	}

	return stats
}

func (s *ServiceRegistry) Config() []ServiceConfig {
	s.Lock()
	defer s.Unlock()

	var configs []ServiceConfig
	for _, service := range s.svcs {
		configs = append(configs, service.Config())
	}

	return configs
}

func (s *ServiceRegistry) String() string {
	return string(marshal(s.Config()))
}
