package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"sync"

	"github.com/litl/galaxy/shuttle/client"
	. "gopkg.in/check.v1"
)

type HTTPSuite struct {
	servers        []*testServer
	backendServers []*testHTTPServer
	httpSvr        *httptest.Server
}

var _ = Suite(&HTTPSuite{})

func (s *HTTPSuite) SetUpSuite(c *C) {
	Registry = ServiceRegistry{
		svcs:   make(map[string]*Service),
		vhosts: make(map[string]*VirtualHost),
	}

	addHandlers()
	s.httpSvr = httptest.NewServer(nil)

	httpRouter = NewHostRouter()
	ready := make(chan bool)
	go httpRouter.Start(ready)
	<-ready
}

func (s *HTTPSuite) TearDownSuite(c *C) {
	s.httpSvr.Close()
	httpRouter.Stop()
}

func (s *HTTPSuite) SetUpTest(c *C) {
	// start 4 possible backend servers
	for i := 0; i < 4; i++ {
		server, err := NewTestServer("127.0.0.1:0", c)
		if err != nil {
			c.Fatal(err)
		}
		s.servers = append(s.servers, server)
	}

	for i := 0; i < 4; i++ {
		server, err := NewHTTPTestServer("127.0.0.1:0", c)
		if err != nil {
			c.Fatal(err)
		}

		s.backendServers = append(s.backendServers, server)
	}
}

// shutdown our backend servers
func (s *HTTPSuite) TearDownTest(c *C) {
	for _, s := range s.servers {
		s.Stop()
	}

	s.servers = s.servers[:0]

	for _, s := range s.backendServers {
		s.Close()
	}

	s.backendServers = s.backendServers[:0]

	for _, svc := range Registry.svcs {
		Registry.RemoveService(svc.Name)
	}

}

// These don't yet *really* test anything other than code coverage
func (s *HTTPSuite) TestAddService(c *C) {
	svcDef := bytes.NewReader([]byte(`{"address": "127.0.0.1:9000"}`))
	req, _ := http.NewRequest("PUT", s.httpSvr.URL+"/testService", svcDef)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)
	c.Assert(Registry.String(), DeepEquals, string(body))
}

func (s *HTTPSuite) TestAddBackend(c *C) {
	svcDef := bytes.NewReader([]byte(`{"address": "127.0.0.1:9000"}`))
	req, _ := http.NewRequest("PUT", s.httpSvr.URL+"/testService", svcDef)
	_, err := http.DefaultClient.Do(req)
	if err != nil {
		c.Fatal(err)
	}

	backendDef := bytes.NewReader([]byte(`{"address": "127.0.0.1:9001"}`))
	req, _ = http.NewRequest("PUT", s.httpSvr.URL+"/testService/testBackend", backendDef)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)
	c.Assert(Registry.String(), DeepEquals, string(body))
}

func (s *HTTPSuite) TestReAddBackend(c *C) {
	svcDef := bytes.NewReader([]byte(`{"address": "127.0.0.1:9000"}`))
	req, _ := http.NewRequest("PUT", s.httpSvr.URL+"/testService", svcDef)
	_, err := http.DefaultClient.Do(req)
	if err != nil {
		c.Fatal(err)
	}

	backendDef := bytes.NewReader([]byte(`{"address": "127.0.0.1:9001"}`))
	req, _ = http.NewRequest("PUT", s.httpSvr.URL+"/testService/testBackend", backendDef)
	firstResp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.Fatal(err)
	}
	defer firstResp.Body.Close()

	firstBody, _ := ioutil.ReadAll(firstResp.Body)

	backendDef = bytes.NewReader([]byte(`{"address": "127.0.0.1:9001"}`))
	req, _ = http.NewRequest("PUT", s.httpSvr.URL+"/testService/testBackend", backendDef)
	secResp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.Fatal(err)
	}
	defer secResp.Body.Close()

	secBody, _ := ioutil.ReadAll(secResp.Body)

	c.Assert(string(secBody), DeepEquals, string(firstBody))
}

func (s *HTTPSuite) TestSimulAdd(c *C) {
	start := make(chan struct{})
	testWG := new(sync.WaitGroup)

	svcCfg := client.ServiceConfig{
		Name:         "TestService",
		Addr:         "127.0.0.1:9000",
		VirtualHosts: []string{"test-vhost"},
		Backends: []client.BackendConfig{
			client.BackendConfig{
				Name: "vhost1",
				Addr: "127.0.0.1:9001",
			},
			client.BackendConfig{
				Name: "vhost2",
				Addr: "127.0.0.1:9002",
			},
		},
	}

	for i := 0; i < 8; i++ {
		testWG.Add(1)
		go func() {
			defer testWG.Done()
			//wait to start all at once
			<-start
			svcDef := bytes.NewReader(svcCfg.Marshal())
			req, _ := http.NewRequest("PUT", s.httpSvr.URL+"/testService", svcDef)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				c.Fatal(err)
			}

			body, _ := ioutil.ReadAll(resp.Body)

			respCfg := []client.ServiceConfig{}
			json.Unmarshal(body, &respCfg)

			// We're only checking to ensure we have 1 service with the proper number of backens
			c.Assert(len(respCfg), Equals, 1)
			c.Assert(len(respCfg[0].Backends), Equals, 2)
			c.Assert(len(respCfg[0].VirtualHosts), Equals, 1)
		}()
	}

	close(start)
	testWG.Wait()
}

func (s *HTTPSuite) TestRouter(c *C) {
	svcCfg := client.ServiceConfig{
		Name:         "VHostTest",
		Addr:         "127.0.0.1:9000",
		VirtualHosts: []string{"test-vhost"},
	}

	for _, srv := range s.backendServers {
		cfg := client.BackendConfig{
			Addr: srv.addr,
			Name: srv.addr,
		}
		svcCfg.Backends = append(svcCfg.Backends, cfg)
	}

	err := Registry.AddService(svcCfg)
	if err != nil {
		c.Fatal(err)
	}

	for _, srv := range s.backendServers {
		checkHTTP("http://"+listenAddr+"/addr", "test-vhost", srv.addr, 200, c)
	}
}

func (s *HTTPSuite) TestAddRemoveVHosts(c *C) {
	svcCfg := client.ServiceConfig{
		Name:         "VHostTest",
		Addr:         "127.0.0.1:9000",
		VirtualHosts: []string{"test-vhost"},
	}

	for _, srv := range s.backendServers {
		cfg := client.BackendConfig{
			Addr: srv.addr,
			Name: srv.addr,
		}
		svcCfg.Backends = append(svcCfg.Backends, cfg)
	}

	err := Registry.AddService(svcCfg)
	if err != nil {
		c.Fatal(err)
	}

	// now update the service with another vhost
	svcCfg.VirtualHosts = append(svcCfg.VirtualHosts, "test-vhost-2")
	err = Registry.UpdateService(svcCfg)
	if err != nil {
		c.Fatal(err)
	}

	if Registry.VHostsLen() != 2 {
		c.Fatal("missing new vhost")
	}

	// remove the first vhost
	svcCfg.VirtualHosts = []string{"test-vhost-2"}
	err = Registry.UpdateService(svcCfg)
	if err != nil {
		c.Fatal(err)
	}

	if Registry.VHostsLen() != 1 {
		c.Fatal("extra vhost:", Registry.VHostsLen())
	}

	// check responses from this new vhost
	for _, srv := range s.backendServers {
		checkHTTP("http://"+listenAddr+"/addr", "test-vhost-2", srv.addr, 200, c)
	}
}

// Add multiple services under the same VirtualHost
// Each proxy request should round-robin through the two of them
func (s *HTTPSuite) TestMultiServiceVHost(c *C) {
	svcCfgOne := client.ServiceConfig{
		Name:         "VHostTest",
		Addr:         "127.0.0.1:9000",
		VirtualHosts: []string{"test-vhost"},
	}

	svcCfgTwo := client.ServiceConfig{
		Name:         "VHostTest2",
		Addr:         "127.0.0.1:9001",
		VirtualHosts: []string{"test-vhost-2"},
	}

	var backends []client.BackendConfig
	for _, srv := range s.backendServers {
		cfg := client.BackendConfig{
			Addr: srv.addr,
			Name: srv.addr,
		}
		backends = append(backends, cfg)
	}

	svcCfgOne.Backends = backends
	svcCfgTwo.Backends = backends

	err := Registry.AddService(svcCfgOne)
	if err != nil {
		c.Fatal(err)
	}

	err = Registry.AddService(svcCfgTwo)
	if err != nil {
		c.Fatal(err)
	}

	for _, srv := range s.backendServers {
		checkHTTP("http://"+listenAddr+"/addr", "test-vhost", srv.addr, 200, c)
		checkHTTP("http://"+listenAddr+"/addr", "test-vhost-2", srv.addr, 200, c)
	}

}
