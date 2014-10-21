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
			req, _ := http.NewRequest("PUT", s.httpSvr.URL+"/TestService", svcDef)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				c.Fatal(err)
			}

			body, _ := ioutil.ReadAll(resp.Body)

			respCfg := client.Config{}
			err = json.Unmarshal(body, &respCfg)

			// We're only checking to ensure we have 1 service with the proper number of backends
			c.Assert(len(respCfg.Services), Equals, 1)
			c.Assert(len(respCfg.Services[0].Backends), Equals, 2)
			c.Assert(len(respCfg.Services[0].VirtualHosts), Equals, 1)
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

func (s *HTTPSuite) TestAddRemoveBackends(c *C) {
	svcCfg := client.ServiceConfig{
		Name: "VHostTest",
		Addr: "127.0.0.1:9000",
	}

	err := Registry.AddService(svcCfg)
	if err != nil {
		c.Fatal(err)
	}

	for _, srv := range s.backendServers {
		cfg := client.BackendConfig{
			Addr: srv.addr,
			Name: srv.addr,
		}
		svcCfg.Backends = append(svcCfg.Backends, cfg)
	}

	err = Registry.UpdateService(svcCfg)
	if err != nil {
		c.Fatal(err)
	}

	cfg := Registry.Config()
	if !svcCfg.DeepEqual(cfg.Services[0]) {
		c.Errorf("we should have 1 service, we have %d", len(cfg.Services))
		c.Errorf("we should have 4 backends, we have %d", len(cfg.Services[0].Backends))
	}

	svcCfg.Backends = svcCfg.Backends[:3]
	err = Registry.UpdateService(svcCfg)
	if err != nil {
		c.Fatal(err)
	}

	cfg = Registry.Config()
	if !svcCfg.DeepEqual(cfg.Services[0]) {
		c.Errorf("we should have 1 service, we have %d", len(cfg.Services))
		c.Errorf("we should have 3 backends, we have %d", len(cfg.Services[0].Backends))
	}

}

func (s *HTTPSuite) TestHTTPAddRemoveBackends(c *C) {
	svcCfg := client.ServiceConfig{
		Name: "VHostTest",
		Addr: "127.0.0.1:9000",
	}

	err := Registry.AddService(svcCfg)
	if err != nil {
		c.Fatal(err)
	}

	for _, srv := range s.backendServers {
		cfg := client.BackendConfig{
			Addr: srv.addr,
			Name: srv.addr,
		}
		svcCfg.Backends = append(svcCfg.Backends, cfg)
	}

	req, _ := http.NewRequest("PUT", s.httpSvr.URL+"/VHostTest", bytes.NewReader(svcCfg.Marshal()))
	_, err = http.DefaultClient.Do(req)
	if err != nil {
		c.Fatal(err)
	}

	cfg := Registry.Config()
	if !svcCfg.DeepEqual(cfg.Services[0]) {
		c.Errorf("we should have 1 service, we have %d", len(cfg.Services))
		c.Errorf("we should have 4 backends, we have %d", len(cfg.Services[0].Backends))
	}

	// remove a backend from the config and submit it again
	svcCfg.Backends = svcCfg.Backends[:3]
	err = Registry.UpdateService(svcCfg)
	if err != nil {
		c.Fatal(err)
	}

	req, _ = http.NewRequest("PUT", s.httpSvr.URL+"/VHostTest", bytes.NewReader(svcCfg.Marshal()))
	_, err = http.DefaultClient.Do(req)
	if err != nil {
		c.Fatal(err)
	}

	// now check the config via what's returned from the http server
	resp, err := http.Get(s.httpSvr.URL + "/_config")
	if err != nil {
		c.Fatal(err)
	}
	defer resp.Body.Close()

	cfg = client.Config{}
	body, _ := ioutil.ReadAll(resp.Body)
	err = json.Unmarshal(body, &cfg)
	if err != nil {
		c.Fatal(err)
	}

	if !svcCfg.DeepEqual(cfg.Services[0]) {
		c.Errorf("we should have 1 service, we have %d", len(cfg.Services))
		c.Errorf("we should have 3 backends, we have %d", len(cfg.Services[0].Backends))
	}
}

func (s *HTTPSuite) TestErrorPage(c *C) {
	svcCfg := client.ServiceConfig{
		Name:         "VHostTest",
		Addr:         "127.0.0.1:9000",
		VirtualHosts: []string{"test-vhost"},
	}

	okServer := s.backendServers[0]
	errServer := s.backendServers[1]

	// Add one backend to service requests
	cfg := client.BackendConfig{
		Addr: okServer.addr,
		Name: okServer.addr,
	}
	svcCfg.Backends = append(svcCfg.Backends, cfg)

	// use another backend to provide the error page
	svcCfg.ErrorPages = map[string][]int{
		"http://" + errServer.addr + "/error": []int{400, 503},
	}

	err := Registry.AddService(svcCfg)
	if err != nil {
		c.Fatal(err)
	}

	// check that the normal response comes from srv1
	checkHTTP("http://"+listenAddr+"/addr", "test-vhost", okServer.addr, 200, c)
	// verify that an unregistered error doesn't give the cached page
	checkHTTP("http://"+listenAddr+"/error?code=504", "test-vhost", okServer.addr, 504, c)
	// now see if the registered error comes from srv2
	checkHTTP("http://"+listenAddr+"/error?code=503", "test-vhost", errServer.addr, 503, c)

	// now check that we got the header cached in the error page as well
	req, err := http.NewRequest("GET", "http://"+listenAddr+"/error?code=503", nil)
	if err != nil {
		c.Fatal(err)
	}

	req.Host = "test-vhost"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.Fatal(err)
	}

	c.Assert(resp.StatusCode, Equals, 503)
	c.Assert(resp.Header.Get("Last-Modified"), Equals, errServer.addr)
}

func (s *HTTPSuite) TestUpdateServiceDefaults(c *C) {
	svcCfg := client.ServiceConfig{
		Name: "TestService",
		Addr: "127.0.0.1:9000",
		Backends: []client.BackendConfig{
			client.BackendConfig{
				Name: "Backend1",
				Addr: "127.0.0.1:9001",
			},
		},
	}

	svcDef := bytes.NewBuffer(svcCfg.Marshal())
	req, _ := http.NewRequest("PUT", s.httpSvr.URL+"/TestService", svcDef)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.Fatal(err)
	}

	// Now update the Service in-place
	svcCfg.ServerTimeout = 1234
	svcDef.Reset()
	svcDef.Write(svcCfg.Marshal())

	req, _ = http.NewRequest("PUT", s.httpSvr.URL+"/TestService", svcDef)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		c.Fatal(err)
	}

	body, _ := ioutil.ReadAll(resp.Body)
	resp.Body.Close()

	config := client.Config{}
	err = json.Unmarshal(body, &config)
	if err != nil {
		c.Fatal(err)
	}

	// make sure we don't see a second value
	found := false

	for _, svc := range config.Services {
		if svc.Name == "TestService" {
			if svc.ServerTimeout != svcCfg.ServerTimeout {
				c.Fatal("Service not updated")
			} else if found {
				c.Fatal("Multiple Service Definitions")
			}
			found = true
		}
	}
}
