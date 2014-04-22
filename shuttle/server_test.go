package main

import (
	"io"
	"net"
)

type testServer struct {
	addr     string
	sig      string
	listener net.Listener
}

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

	go func() {
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				return
			}

			go func() {
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
}
