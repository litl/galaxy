package main

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	. "gopkg.in/check.v1"
)

type HTTPBackendSuite struct {
	servers []*testHTTPServer
	httpSvr *httptest.Server
}

var _ = Suite(&HTTPBackendSuite{})

// Connect to http server, and check response for value
func checkHTTP(url, host, expected string, status int, c Tester) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		c.Fatal(err)
	}

	req.Host = host

	c.Log("GET ", req.Host, req.URL.Path)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		c.Fatal(err)
	}

	c.Assert(resp.StatusCode, Equals, status)

	c.Assert(string(body), Equals, expected)
}

func (s *HTTPBackendSuite) SetUpTest(c *C) {
	s.httpSvr = httptest.NewServer(nil)

	httpRouter = NewHostRouter()

	ready := make(chan bool)
	go httpRouter.Start(ready)
	<-ready

	for i := 0; i < 4; i++ {
		server, err := NewHTTPTestServer("127.0.0.1:0", c)
		if err != nil {
			c.Fatal(err)
		}

		s.servers = append(s.servers, server)

		httpRouter.AddBackend(server.addr, server.name, "http://"+server.addr)
	}
}

func (s *HTTPBackendSuite) TearDownTest(c *C) {
	for _, s := range s.servers {
		s.Close()
	}
	s.servers = nil
	httpRouter.Stop()
}

// check that our backend works as expected without the LB
func (s *HTTPBackendSuite) TestHTTPBackendTest(c *C) {
	for _, s := range s.servers {
		checkHTTP("http://"+s.addr+"/addr", s.addr, s.addr, 200, c)
	}
}
