package main

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	. "launchpad.net/gocheck"
)

type HTTPSuite struct {
	servers []*testServer
	httpSvr *httptest.Server
}

var _ = Suite(&HTTPSuite{})

func (s *HTTPSuite) SetUpSuite(c *C) {
	addHandlers()
	s.httpSvr = httptest.NewServer(nil)
}

func (s *HTTPSuite) TearDownSuite(c *C) {
	s.httpSvr.Close()
}

func (s *HTTPSuite) SetUpTest(c *C) {
	// start 4 possible backend servers
	ports := []string{"9001", "9002", "9003", "9004"}
	for _, p := range ports {
		server, err := NewTestServer("127.0.0.1:"+p, c)
		if err != nil {
			c.Fatal(err)
		}
		s.servers = append(s.servers, server)
	}
}

// shutdown our backend servers
func (s *HTTPSuite) TearDownTest(c *C) {
	for _, s := range s.servers {
		s.Stop()
	}

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
