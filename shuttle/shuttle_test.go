package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"sync"
	"testing"
	"time"

	. "gopkg.in/check.v1"
)

func init() {
	// log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetOutput(ioutil.Discard)
}

// something that can wrap a gocheck.C testing.T or testing.B
// Just add more methods as we need them.
type Tester interface {
	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})
	Log(args ...interface{})
	Logf(format string, args ...interface{})
}

func Test(t *testing.T) { TestingT(t) }

type BasicSuite struct {
	servers []*testServer
	service *Service
}

var _ = Suite(&BasicSuite{})

// Make Setup and TearDown more generic, so we can bypass the gocheck Suite if
// needed.
func mySetup(s *BasicSuite, t Tester) {
	// start 4 possible backend servers
	ports := []string{"2001", "2002", "2003", "2004"}
	for _, p := range ports {
		server, err := NewTestServer("127.0.0.1:"+p, t)
		if err != nil {
			t.Fatal(err)
		}
		s.servers = append(s.servers, server)
	}

	svcCfg := ServiceConfig{
		Name: "testService",
		Addr: "127.0.0.1:2222",
	}

	if err := Registry.AddService(svcCfg); err != nil {
		t.Fatal(err)
	}

	s.service = Registry.GetService(svcCfg.Name)
}

// shutdown our backend servers
func myTearDown(s *BasicSuite, t Tester) {
	for _, s := range s.servers {
		s.Stop()
	}

	// get rid of the servers refs too!
	s.servers = nil

	err := Registry.RemoveService(s.service.Name)
	if err != nil {
		t.Fatalf("could not remove service '%s': %s", s.service.Name, err)
	}
}

func (s *BasicSuite) SetUpTest(c *C) {
	mySetup(s, c)
}

func (s *BasicSuite) TearDownTest(c *C) {
	myTearDown(s, c)
}

// Add a default backend for the next server we have running
func (s *BasicSuite) AddBackend(c Tester) {
	// get the backends via Config to use the Service's locking.
	svcCfg := s.service.Config()
	next := len(svcCfg.Backends)
	if next >= len(s.servers) {
		c.Fatal("no more servers")
	}

	name := fmt.Sprintf("backend_%d", next)
	cfg := BackendConfig{
		Name:      name,
		Addr:      s.servers[next].addr,
		CheckAddr: s.servers[next].addr,
	}

	s.service.add(NewBackend(cfg))
}

// Connect to address, and check response after write.
func checkResp(addr, expected string, c Tester) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		c.Fatal(err)
	}
	defer conn.Close()

	if _, err := io.WriteString(conn, "testing\n"); err != nil {
		c.Fatal(err)
	}

	buff := make([]byte, 1024)
	n, err := conn.Read(buff)
	if err != nil {
		c.Fatal(err)
	}

	resp := string(buff[:n])
	if resp == "" {
		c.Fatal("No response")
	}

	if expected != "" && resp != expected {
		c.Fatal("Expected ", expected, ", got ", resp)
	}
}

func (s *BasicSuite) TestSingleBackend(c *C) {
	s.AddBackend(c)

	checkResp(s.service.Addr, s.servers[0].addr, c)
}

func (s *BasicSuite) TestRoundRobin(c *C) {
	s.AddBackend(c)
	s.AddBackend(c)

	checkResp(s.service.Addr, s.servers[0].addr, c)
	checkResp(s.service.Addr, s.servers[1].addr, c)
	checkResp(s.service.Addr, s.servers[0].addr, c)
	checkResp(s.service.Addr, s.servers[1].addr, c)
}

func (s *BasicSuite) TestWeightedRoundRobin(c *C) {
	s.AddBackend(c)
	s.AddBackend(c)
	s.AddBackend(c)

	s.service.Backends[0].Weight = 1
	s.service.Backends[1].Weight = 2
	s.service.Backends[2].Weight = 3

	// we already checked that we connect to the correct backends,
	// so skip the tcp connection this time.

	// one from the first server
	c.Assert(s.service.next()[0].Name, Equals, "backend_0")
	// A weight of 2 should return twice
	c.Assert(s.service.next()[0].Name, Equals, "backend_1")
	c.Assert(s.service.next()[0].Name, Equals, "backend_1")
	// And a weight of 3 should return thrice
	c.Assert(s.service.next()[0].Name, Equals, "backend_2")
	c.Assert(s.service.next()[0].Name, Equals, "backend_2")
	c.Assert(s.service.next()[0].Name, Equals, "backend_2")
	// and once around or good measure
	c.Assert(s.service.next()[0].Name, Equals, "backend_0")
}

func (s *BasicSuite) TestLeastConn(c *C) {
	// replace out default service with one using LeastConn balancing
	Registry.RemoveService("testService")
	svcCfg := ServiceConfig{
		Name:    "testService",
		Addr:    "127.0.0.1:2223",
		Balance: "LC",
	}

	if err := Registry.AddService(svcCfg); err != nil {
		c.Fatal(err)
	}
	s.service = Registry.GetService("testService")

	s.AddBackend(c)
	s.AddBackend(c)

	// tie up 4 connections to the backends
	for i := 0; i < 4; i++ {
		conn, e := net.Dial("tcp", s.service.Addr)
		if e != nil {
			c.Fatal(e)
		}
		defer conn.Close()
	}

	s.AddBackend(c)

	checkResp(s.service.Addr, s.servers[2].addr, c)
	checkResp(s.service.Addr, s.servers[2].addr, c)
}

// Test health check by taking down a server from a configured backend
func (s *BasicSuite) TestFailedCheck(c *C) {
	s.service.CheckInterval = 500
	s.service.Fall = 1
	s.AddBackend(c)

	stats := s.service.Stats()
	c.Assert(stats.Backends[0].Up, Equals, true)

	// Stop the server, and see if the backend shows Down after our check
	// interval.
	s.servers[0].Stop()
	time.Sleep(800 * time.Millisecond)

	stats = s.service.Stats()
	c.Assert(stats.Backends[0].Up, Equals, false)
	c.Assert(stats.Backends[0].CheckFail, Equals, 1)

	// now try and connect to the service
	conn, err := net.Dial("tcp", s.service.Addr)
	if err != nil {
		// we should still get an initial connection
		c.Fatal(err)
	}

	b := make([]byte, 1024)
	n, err := conn.Read(b)
	if n != 0 || err != io.EOF {
		c.Fatal("connection should have been closed")
	}

	// now bring that server back up
	server, err := NewTestServer(s.servers[0].addr, c)
	if err != nil {
		c.Fatal(err)
	}
	s.servers[0] = server

	time.Sleep(800 * time.Millisecond)
	stats = s.service.Stats()
	c.Assert(stats.Backends[0].Up, Equals, true)
}

// Make sure the connection is re-dispatched when Dialing a backend fails
func (s *BasicSuite) TestConnectAny(c *C) {
	s.service.CheckInterval = 2000
	s.service.Fall = 2
	s.AddBackend(c)
	s.AddBackend(c)

	// kill the first server
	s.servers[0].Stop()

	stats := s.service.Stats()
	c.Assert(stats.Backends[0].Up, Equals, true)

	// Backend 0 still shows up, but we should get connected to backend 1
	checkResp(s.service.Addr, s.servers[1].addr, c)
}

// Update a backend in place
func (s *BasicSuite) TestUpdateBackend(c *C) {
	s.service.CheckInterval = 500
	s.service.Fall = 1
	s.AddBackend(c)

	cfg := s.service.Config()
	backendCfg := cfg.Backends[0]

	c.Assert(backendCfg.CheckAddr, Equals, backendCfg.Addr)

	backendCfg.CheckAddr = ""
	s.service.add(NewBackend(backendCfg))

	// see if the config reflects the new backend
	cfg = s.service.Config()
	c.Assert(len(cfg.Backends), Equals, 1)
	c.Assert(cfg.Backends[0].CheckAddr, Equals, "")

	// Stopping the server should not take down the backend
	// since there is no longer a Check address.
	s.servers[0].Stop()
	time.Sleep(800 * time.Millisecond)

	stats := s.service.Stats()
	c.Assert(stats.Backends[0].Up, Equals, true)
	// should have been no check failures
	c.Assert(stats.Backends[0].CheckFail, Equals, 0)
}

// Test removal of a single Backend from a service with multiple.
func (s *BasicSuite) TestRemoveBackend(c *C) {
	s.AddBackend(c)
	s.AddBackend(c)

	stats, err := Registry.ServiceStats("testService")
	if err != nil {
		c.Fatal(err)
	}

	c.Assert(len(stats.Backends), Equals, 2)

	backend1 := stats.Backends[0].Name

	err = Registry.RemoveBackend("testService", backend1)
	if err != nil {
		c.Fatal(err)
	}

	stats, err = Registry.ServiceStats("testService")
	if err != nil {
		c.Fatal(err)
	}

	c.Assert(len(stats.Backends), Equals, 1)

	_, err = Registry.BackendStats("testService", backend1)
	c.Assert(err, Equals, ErrNoBackend)
}

func (s *BasicSuite) TestUpdateService(c *C) {
	svcCfg := ServiceConfig{
		Name: "Update",
		Addr: "127.0.0.1:9324",
	}

	if err := Registry.AddService(svcCfg); err != nil {
		c.Fatal(err)
	}

	svc := Registry.GetService("Update")
	if svc == nil {
		c.Fatal(ErrNoService)
	}

	svcCfg = ServiceConfig{
		Name: "Update",
		Addr: "127.0.0.1:9425",
	}

	// Make sure we can't add it through AddService
	if err := Registry.AddService(svcCfg); err == nil {
		c.Fatal(err)
	}

	// Now update the service
	if err := Registry.UpdateService(svcCfg, false); err != nil {
		c.Fatal(err)
	}

	svc = Registry.GetService("Update")
	if svc == nil {
		c.Fatal(ErrNoService)
	}
	c.Assert(svc.Addr, Equals, "127.0.0.1:9425")

	if err := Registry.RemoveService("Update"); err != nil {
		c.Fatal(err)
	}
}

// Add backends and run response tests in parallel
// FIXME: there's still some racy-ness in here somewhere.
func (s *BasicSuite) TestParallel(c *C) {
	var wg sync.WaitGroup

	client := func(i int) {
		s.AddBackend(c)
		// do a bunch of new connections in unison
		for i := 0; i < 100; i++ {
			checkResp(s.service.Addr, "", c)
		}

		conn, err := net.Dial("tcp", s.service.Addr)
		if err != nil {
			// we should still get an initial connection
			c.Fatal(err)
		}
		defer conn.Close()

		// now do some more continuous ping-pongs with the server
		buff := make([]byte, 1024)

		for i := 0; i < 1000; i++ {
			n, err := io.WriteString(conn, "Testing testing\n")
			if err != nil || n == 0 {
				c.Fatal("couldn't write:", err)
			}

			n, err = conn.Read(buff)
			if err != nil || n == 0 {
				c.Fatal("no response:", err)
			}
		}
		wg.Done()
	}

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go client(i)
	}

	wg.Wait()
}
