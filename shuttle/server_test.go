package main

import (
	"io"
	"net"
	"sync"
	"time"
)

type testServer struct {
	addr     string
	sig      string
	listener net.Listener
	wg       sync.WaitGroup
}

// Start a tcp server which responds with it's addr after every read.
func NewTestServer(addr string, c Tester) (*testServer, error) {
	s := &testServer{}

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
