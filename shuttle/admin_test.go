package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/litl/galaxy/shuttle/client"
	. "gopkg.in/check.v1"
)

type HTTPSuite struct {
	servers     []*testServer
	httpServers []*testHTTPServer
	httpSvr     *httptest.Server
}

var _ = Suite(&HTTPSuite{})

func (s *HTTPSuite) SetUpSuite(c *C) {
	Registry = ServiceRegistry{
		svcs:   make(map[string]*Service),
		vhosts: make(map[string]*Service),
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

		s.httpServers = append(s.httpServers, server)
	}
}

// shutdown our backend servers
func (s *HTTPSuite) TearDownTest(c *C) {
	for _, s := range s.servers {
		s.Stop()
	}

	s.servers = s.servers[:0]

	for _, s := range s.httpServers {
		s.Close()
	}

	s.httpServers = s.httpServers[:0]

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

	for _, srv := range s.httpServers {
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

	svc := Registry.GetVHostService("test-vhost")
	// force a new backend connection for each client
	svc.httpProxy.Transport.(*http.Transport).DisableKeepAlives = true

	for _, srv := range s.httpServers {
		//		checkHTTP("http://"+listenAddr+"/addr", "test-vhost", srv.addr, 200, c)
		req, err := http.NewRequest("GET", "http://"+listenAddr+"/addr", nil)
		if err != nil {
			c.Fatal(err)
		}

		req.Host = "test-vhost"
		req.Header.Add("Connection", "close")

		// new client and transport to prevent any keepalive
		client := http.Client{
			Transport: &http.Transport{},
			Timeout:   time.Second,
		}

		resp, err := client.Do(req)
		if err != nil {
			c.Fatal(err)
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			c.Fatal(err)
		}
		c.Assert(strings.TrimSpace(string(body)), DeepEquals, srv.addr)
	}
}
