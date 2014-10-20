package main

import (
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/shuttle/client"
	"github.com/litl/galaxy/utils"
)

var (
	ErrNoService        = fmt.Errorf("service does not exist")
	ErrNoBackend        = fmt.Errorf("backend does not exist")
	ErrDuplicateService = fmt.Errorf("service already exists")
	ErrDuplicateBackend = fmt.Errorf("backend already exists")
)

type VirtualHost struct {
	sync.Mutex
	Name string
	// All services registered under this vhost name.
	services []*Service
	// The last one we returned so we can RoundRobin them.
	last int
}

func (v *VirtualHost) Len() int {
	v.Lock()
	defer v.Unlock()
	return len(v.services)
}

// Insert a service
// do nothing if the service already is registered
func (v *VirtualHost) Add(svc *Service) {
	v.Lock()
	defer v.Unlock()
	for _, s := range v.services {
		if s.Name == svc.Name {
			log.Debugf("Service %s already registered in VirtualHost %s", svc.Name, v.Name)
			return
		}
	}

	// TODO: is this the best place to log these?
	svcCfg := svc.Config()
	for _, backend := range svcCfg.Backends {
		log.Printf("Adding HTTP endpoint http://%s to %s", backend.Addr, v.Name)
	}
	v.services = append(v.services, svc)
}

func (v *VirtualHost) Remove(svc *Service) {
	v.Lock()
	defer v.Unlock()

	found := -1
	for i, s := range v.services {
		if s.Name == svc.Name {
			found = i
			break
		}
	}

	if found < 0 {
		log.Debugf("Service %s not found under VirtualHost %s", svc.Name, v.Name)
		return
	}

	// safe way to get the backends info for logging
	svcCfg := svc.Config()

	// Now removing this Service
	for _, backend := range svcCfg.Backends {
		log.Printf("Removing HTTP endpoint http://%s from %s", backend.Addr, v.Name)
	}

	v.services = append(v.services[:found], v.services[found+1:]...)
}

// Return a *Service for this VirtualHost
func (v *VirtualHost) Service() *Service {
	v.Lock()
	defer v.Unlock()

	if len(v.services) == 0 {
		log.Warnf("No Services registered for VirtualHost %s", v.Name)
		return nil
	}

	// start cycling through the services in case one has no backends available
	for i := 1; i <= len(v.services); i++ {
		idx := (v.last + i) % len(v.services)
		if v.services[idx].Available() > 0 {
			v.last = idx
			return v.services[idx]
		}
	}

	// even if all backends are down, return a service so that the request can
	// be processed normally (we may have a custom 502 error page for this)
	return v.services[v.last]
}

//TODO: notify or prevent vhost name conflicts between services.
// ServiceRegistry is a global container for all configured services.
type ServiceRegistry struct {
	sync.Mutex
	svcs map[string]*Service
	// Multiple services may respond from a single vhost
	vhosts map[string]*VirtualHost
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

	if vhost := s.vhosts[name]; vhost != nil {
		return vhost.Service()
	}
	return nil
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

	for _, name := range cfg.VirtualHosts {
		vhost := s.vhosts[name]
		if vhost == nil {
			vhost = &VirtualHost{Name: name}
			s.vhosts[name] = vhost
		}
		vhost.Add(service)
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

	if err := service.UpdateDefaults(newCfg); err != nil {
		return err
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

	// replace error pages if there's any change
	if !reflect.DeepEqual(service.errPagesCfg, newCfg.ErrorPages) {
		log.Debugf("Updating ErrorPages")
		service.errPagesCfg = newCfg.ErrorPages
		service.errorPages.Update(newCfg.ErrorPages)
	}

	s.updateVHosts(service, newCfg.VirtualHosts)

	return nil
}

// update the VirtualHost entries for this service
// only to be called from UpdateService.
func (s *ServiceRegistry) updateVHosts(service *Service, newHosts []string) {
	// We could just clear the vhosts and the new list since we're doing
	// this all while the registry is locked, but because we want sane log
	// messages about adding remove endpoints, we have to diff the slices
	// anyway.

	oldHosts := service.VirtualHosts
	sort.Strings(oldHosts)
	sort.Strings(newHosts)

	// find the relative compliments of each set of hostnames
	var remove, add []string
	i, j := 0, 0
	for i < len(oldHosts) && j < len(newHosts) {
		if oldHosts[i] != newHosts[j] {
			if oldHosts[i] < newHosts[j] {
				// oldHosts[i] can't be in newHosts
				remove = append(remove, oldHosts[i])
				i++
				continue
			} else {
				// newHosts[j] can't be in oldHosts
				add = append(add, newHosts[j])
				j++
				continue
			}
		}
		i++
		j++
	}
	if i < len(oldHosts) {
		// there's more!
		remove = append(remove, oldHosts[i:]...)
	}
	if j < len(newHosts) {
		add = append(add, newHosts[j:]...)
	}

	// remove existing vhost entries for this service, and add new ones
	for _, name := range remove {
		vhost := s.vhosts[name]
		if vhost != nil {
			vhost.Remove(service)
		}
		if vhost.Len() == 0 {
			log.Println("Removing empty VirtualHost", name)
			delete(s.vhosts, name)
		}
	}

	for _, name := range add {
		vhost := s.vhosts[name]
		if vhost == nil {
			vhost = &VirtualHost{Name: name}
			s.vhosts[name] = vhost
		}
		vhost.Add(service)
	}

	// and replace the list
	service.VirtualHosts = newHosts
}

func (s *ServiceRegistry) RemoveService(name string) error {
	s.Lock()
	defer s.Unlock()

	svc, ok := s.svcs[name]
	if ok {
		log.Debugf("Removing Service %s", svc.Name)
		delete(s.svcs, name)
		svc.stop()

		for host, vhost := range s.vhosts {
			vhost.Remove(svc)

			removeVhost := true
			for _, service := range s.svcs {
				if utils.StringInSlice(host, service.VirtualHosts) {
					removeVhost = false
					break
				}
			}
			if removeVhost {
				log.Debugf("Removing VirtualHost %s", host)
				delete(s.vhosts, host)

			}
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
