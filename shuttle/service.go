package main

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/shuttle/client"
)

var (
	Registry = ServiceRegistry{
		svcs: make(map[string]*Service, 0),
	}
)

type Service struct {
	sync.Mutex
	Name          string
	Addr          string
	VirtualHosts  []string
	Backends      []*Backend
	Balance       string
	CheckInterval int
	Fall          int
	Rise          int
	ClientTimeout time.Duration
	ServerTimeout time.Duration
	DialTimeout   time.Duration
	Sent          int64
	Rcvd          int64
	Errors        int64

	// Next returns the backends in priority order.
	next func() []*Backend

	// the last backend we used and the number of times we used it
	lastBackend int
	lastCount   int

	// Each Service owns it's own netowrk listener
	listener net.Listener
}

// Stats returned about a service
type ServiceStat struct {
	Name          string        `json:"name"`
	Addr          string        `json:"address"`
	VirtualHosts  []string      `json:"virtual_hosts"`
	Backends      []BackendStat `json:"backends"`
	Balance       string        `json:"balance"`
	CheckInterval int           `json:"check_interval"`
	Fall          int           `json:"fall"`
	Rise          int           `json:"rise"`
	ClientTimeout int           `json:"client_timeout"`
	ServerTimeout int           `json:"server_timeout"`
	DialTimeout   int           `json:"connect_timeout"`
	Sent          int64         `json:"sent"`
	Rcvd          int64         `json:"received"`
	Errors        int64         `json:"errors"`
	Conns         int64         `json:"connections"`
	Active        int64         `json:"active"`
}

// Create a Service from a config struct
func NewService(cfg client.ServiceConfig) *Service {
	s := &Service{
		Name:          cfg.Name,
		Addr:          cfg.Addr,
		CheckInterval: cfg.CheckInterval,
		Fall:          cfg.Fall,
		Rise:          cfg.Rise,
		VirtualHosts:  cfg.VirtualHosts,
		ClientTimeout: time.Duration(cfg.ClientTimeout) * time.Millisecond,
		ServerTimeout: time.Duration(cfg.ServerTimeout) * time.Millisecond,
		DialTimeout:   time.Duration(cfg.DialTimeout) * time.Millisecond,
	}

	if s.CheckInterval == 0 {
		s.CheckInterval = 2000
	}
	if s.Rise == 0 {
		s.Rise = 2
	}
	if s.Fall == 0 {
		s.Fall = 2
	}

	for _, b := range cfg.Backends {
		s.add(NewBackend(b))
	}

	switch cfg.Balance {
	case "RR", "":
		s.next = s.roundRobin
	case "LC":
		s.next = s.leastConn
	default:
		log.Printf("invalid balancing algorithm '%s'", cfg.Balance)
	}

	return s
}

func (s *Service) Stats() ServiceStat {
	s.Lock()
	defer s.Unlock()

	stats := ServiceStat{
		Name:          s.Name,
		Addr:          s.Addr,
		VirtualHosts:  s.VirtualHosts,
		Balance:       s.Balance,
		CheckInterval: s.CheckInterval,
		Fall:          s.Fall,
		Rise:          s.Rise,
		ClientTimeout: int(s.ClientTimeout / time.Millisecond),
		ServerTimeout: int(s.ServerTimeout / time.Millisecond),
		DialTimeout:   int(s.DialTimeout / time.Millisecond),
	}

	for _, b := range s.Backends {
		stats.Backends = append(stats.Backends, b.Stats())
		stats.Sent += b.Sent
		stats.Rcvd += b.Rcvd
		stats.Errors += b.Errors
		stats.Conns += b.Conns
		stats.Active += b.Active
	}

	return stats
}

func (s *Service) Config() client.ServiceConfig {
	s.Lock()
	defer s.Unlock()

	config := client.ServiceConfig{
		Name:          s.Name,
		Addr:          s.Addr,
		VirtualHosts:  s.VirtualHosts,
		Balance:       s.Balance,
		CheckInterval: s.CheckInterval,
		Fall:          s.Fall,
		Rise:          s.Rise,
		ClientTimeout: int(s.ClientTimeout / time.Millisecond),
		ServerTimeout: int(s.ServerTimeout / time.Millisecond),
		DialTimeout:   int(s.DialTimeout / time.Millisecond),
	}
	for _, b := range s.Backends {
		config.Backends = append(config.Backends, b.Config())
	}

	return config
}

func (s *Service) String() string {
	return string(marshal(s.Config()))
}

func (s *Service) get(name string) *Backend {
	s.Lock()
	defer s.Unlock()

	for _, b := range s.Backends {
		if b.Name == name {
			return b
		}
	}
	return nil
}

// Add or replace a Backend in this service
func (s *Service) add(backend *Backend) {
	s.Lock()
	defer s.Unlock()

	backend.up = true
	backend.rwTimeout = s.ServerTimeout
	backend.dialTimeout = s.DialTimeout
	backend.checkInterval = time.Duration(s.CheckInterval) * time.Millisecond

	// replace an existing backend if we have it.
	for i, b := range s.Backends {
		if b.Name == backend.Name {
			b.Stop()
			s.Backends[i] = backend
			backend.Start()
			return
		}
	}

	s.Backends = append(s.Backends, backend)

	backend.Start()
}

// Remove a Backend by name
func (s *Service) remove(name string) bool {
	s.Lock()
	defer s.Unlock()

	for i, b := range s.Backends {
		if b.Name == name {
			last := len(s.Backends) - 1
			deleted := b
			s.Backends[i], s.Backends[last] = s.Backends[last], nil
			s.Backends = s.Backends[:last]
			deleted.Stop()
			return true
		}
	}
	return false
}

// Fill out and verify service
func (s *Service) start() (err error) {
	s.Lock()
	defer s.Unlock()
	log.Println("Starting service", s.Name)

	s.listener, err = newTimeoutListener(s.Addr, s.ClientTimeout)
	if err != nil {
		return err
	}

	if s.Backends == nil {
		s.Backends = make([]*Backend, 0)
	}

	s.run()
	return nil
}

// Start the Service's Accept loop
func (s *Service) run() {
	go func() {
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				if err, ok := err.(*net.OpError); ok && err.Temporary() {
					log.Warnln("WARN:", err)
					continue
				}
				// we must be getting shut down
				return
			}

			go s.connect(conn)
		}
	}()
}

func (s *Service) connect(cliConn net.Conn) {
	backends := s.next()

	// Try the first backend given, but if that fails, cycle through them all
	// to make a best effort to connect the client.
	for _, b := range backends {
		srvConn, err := net.DialTimeout("tcp", b.Addr, b.dialTimeout)
		if err != nil {
			log.Errorf("ERROR: connecting to backend %s/%s: %s", s.Name, b.Name, err)
			atomic.AddInt64(&b.Errors, 1)
			continue
		}

		b.Proxy(srvConn, cliConn)
		return
	}

	log.Errorf("ERROR: no backend for", s.Name)
	cliConn.Close()
}

// Stop the Service's Accept loop by closing the Listener,
// and stop all backends for this service.
func (s *Service) stop() {
	s.Lock()
	defer s.Unlock()

	log.Println("Stopping Service", s.Name)
	for _, backend := range s.Backends {
		backend.Stop()
	}

	// the service may have been bad, and the listener failed
	if s.listener == nil {
		return
	}

	err := s.listener.Close()
	if err != nil {
		log.Println(err)
	}
}

// A net.Listener that provides a read/write timeout
type timeoutListener struct {
	net.Listener
	rwTimeout time.Duration
}

func newTimeoutListener(addr string, timeout time.Duration) (net.Listener, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	tl := &timeoutListener{
		Listener:  l,
		rwTimeout: timeout,
	}
	return tl, nil
}
func (l *timeoutListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	c, ok := conn.(*net.TCPConn)
	if ok {
		tc := &timeoutConn{
			TCPConn:   c,
			rwTimeout: l.rwTimeout,
		}
		return tc, nil
	}
	return conn, nil
}
