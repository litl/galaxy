package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	. "gopkg.in/check.v1"
)

type testServer struct {
	addr     string
	sig      string
	listener net.Listener
	wg       *sync.WaitGroup
}

// Start a tcp server which responds with it's addr after every read.
func NewTestServer(addr string, c Tester) (*testServer, error) {
	s := &testServer{}
	s.wg = new(sync.WaitGroup)

	var err error

	// try really hard to bind this so we don't fail tests
	for i := 0; i < 3; i++ {
		s.listener, err = net.Listen("tcp", addr)
		if err == nil {
			break
		}
		c.Log("Listen error:", err)
		c.Log("Trying again in 1s...")
		time.Sleep(time.Second)
	}

	if err != nil {
		return nil, err
	}

	s.addr = s.listener.Addr().String()
	c.Log("listning on ", s.addr)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				return
			}

			conn.SetDeadline(time.Now().Add(5 * time.Second))
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer conn.Close()
				buff := make([]byte, 1024)
				for {
					if _, err := conn.Read(buff); err != nil {
						if err != io.EOF {
							c.Logf("test server '%s' error: %s", s.addr, err)
						}
						return
					}
					if _, err := io.WriteString(conn, s.addr); err != nil {
						if err != io.EOF {
							c.Logf("test server '%s' error: %s", s.addr, err)
						}
						return
					}
				}
			}()
		}
	}()
	return s, nil
}

func (s *testServer) Stop() {
	s.listener.Close()
	// We may be imediately creating another identical server.
	// Wait until all goroutines return to ensure we can bind again.
	s.wg.Wait()
}

// Backend server for testing HTTP proxies
type testHTTPServer struct {
	*httptest.Server
	addr string
	name string
}

// make the handler a method of the server so we can get the server's address
func (s *testHTTPServer) addrHandler(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, s.addr)
}

// Start a tcp server which responds with it's addr after every read.
func NewHTTPTestServer(addr string, c Tester) (*testHTTPServer, error) {
	s := &testHTTPServer{
		Server: httptest.NewUnstartedServer(nil),
	}

	s.addr = s.Listener.Addr().String()
	if parts := strings.Split(s.addr, ":"); len(parts) == 2 {
		s.name = fmt.Sprintf("http-%s.server.test", parts[1])
	} else {
		c.Fatal("error naming http server")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/addr", s.addrHandler)

	s.Config.Handler = mux
	s.Start()

	return s, nil
}

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
