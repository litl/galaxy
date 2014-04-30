package main

import (
	"io"
	"net"
	"sync"
)

type testServer struct {
	addr     string
	sig      string
	listener net.Listener
	wg       sync.WaitGroup
}

// FIXME: this still sometimes fails to bind its port

// Start a tcp server which responds with it's addr after every read.
func NewTestServer(addr string, c Tester) (*testServer, error) {
	s := &testServer{
		addr: addr,
	}

	var err error
	s.listener, err = net.Listen("tcp", s.addr)
	if err != nil {
		return nil, err
	}

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
							c.Logf("test server '%s' error: %s", addr, err)
						}
						return
					}
					if _, err := io.WriteString(conn, addr); err != nil {
						if err != io.EOF {
							c.Logf("test server '%s' error: %s", addr, err)
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
