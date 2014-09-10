package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"

	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/shuttle/client"
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

//TODO: notify or prevent vhost name conflicts between services.
// ServiceRegistry is a global container for all configured services.
type ServiceRegistry struct {
	sync.Mutex
	svcs   map[string]*Service
	vhosts map[string]*Service
}

// Return a service by name.
func (s *ServiceRegistry) GetService(name string) *Service {
	s.Lock()
	defer s.Unlock()
	return s.svcs[name]
}

// Return a service that handles a particular vhost by name.
func (s *ServiceRegistry) GetVHostService(name string) *Service {
	s.Lock()
	defer s.Unlock()

	return s.vhosts[name]
}

func (s *ServiceRegistry) VHostsLen() int {
	s.Lock()
	defer s.Unlock()
	return len(s.vhosts)
}

// Add a new service to the Registry.
// Do not replace an existing service.
func (s *ServiceRegistry) AddService(cfg client.ServiceConfig) error {
	s.Lock()
	defer s.Unlock()

	log.Debug("Adding service:", cfg.Name)
	if _, ok := s.svcs[cfg.Name]; ok {
		log.Debug("Service already exists:", cfg.Name)
		return ErrDuplicateService
	}

	service := NewService(cfg)
	s.svcs[service.Name] = service

	for _, host := range cfg.VirtualHosts {
		s.vhosts[host] = service
	}

	return service.start()
}

// Replace the service's configuration, or update its list of backends.
// Replacing a configuration will shutdown the existing service, and start a
// new one, which will cause the listening socket to be temporarily
// unavailable.
func (s *ServiceRegistry) UpdateService(newCfg client.ServiceConfig) error {
	s.Lock()
	defer s.Unlock()

	log.Debug("Updating Service:", newCfg.Name)
	service, ok := s.svcs[newCfg.Name]
	if !ok {
		log.Debug("Service not found:", newCfg.Name)
		return ErrNoService
	}

	currentCfg := service.Config()

	// Lots of looping here (including fetching the Config, but the cardinality
	// of Backends shouldn't be very large, and the default RoundRobin balancing
	// is much simpler with a slice.

	// we're going to update just the backends for this config
	// get a map of what's already running
	currentBackends := make(map[string]client.BackendConfig)
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

		// we need to remove and re-add this backend
		log.Debugf("Updating Backend %s/%s", service.Name, newBackend.Name)
		service.remove(newBackend.Name)
		service.add(NewBackend(newBackend))
		delete(currentBackends, newBackend.Name)
	}

	// remove any left over backends
	for name := range currentBackends {
		log.Debugf("Removing Backend %s/%s", service.Name, name)
		service.remove(name)
	}

	if currentCfg.Equal(newCfg) {
		log.Debugf("Service Unchanged %s", service.Name)
		return nil
	}

	// remove existing vhost entries for this service, and add new ones
	for _, host := range service.VirtualHosts {
		delete(s.vhosts, host)
	}
	for _, host := range newCfg.VirtualHosts {
		s.vhosts[host] = service
	}

	// replace error pages if there's any change
	if !reflect.DeepEqual(service.errPagesCfg, newCfg.ErrorPages) {
		log.Debugf("Updating ErrorPages")
		service.errPagesCfg = newCfg.ErrorPages
		service.errorPages.Update(newCfg.ErrorPages)
	}

	service.VirtualHosts = newCfg.VirtualHosts
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

		for host := range s.vhosts {
			delete(s.vhosts, host)
		}

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

func (s *ServiceRegistry) ServiceConfig(serviceName string) (client.ServiceConfig, error) {
	s.Lock()
	defer s.Unlock()

	service, ok := s.svcs[serviceName]
	if !ok {
		return client.ServiceConfig{}, ErrNoService
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
func (s *ServiceRegistry) AddBackend(svcName string, backendCfg client.BackendConfig) error {
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

func (s *ServiceRegistry) Config() []client.ServiceConfig {
	s.Lock()
	defer s.Unlock()

	var configs []client.ServiceConfig
	for _, service := range s.svcs {
		configs = append(configs, service.Config())
	}

	return configs
}

func (s *ServiceRegistry) String() string {
	return string(marshal(s.Config()))
}
