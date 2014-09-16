package main

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/litl/galaxy/shuttle/client"
)

var (
	benchServer   *httptest.Server
	benchBackends []*testHTTPServer
	benchRouter   *HostRouter
)

func setupBench(b *testing.B) {
	Registry = ServiceRegistry{
		svcs:   make(map[string]*Service),
		vhosts: make(map[string]*VirtualHost),
	}

	benchServer = httptest.NewServer(nil)

	benchRouter = NewHostRouter()
	ready := make(chan bool)
	go benchRouter.Start(ready)
	<-ready

	for i := 0; i < 4; i++ {
		server, err := NewHTTPTestServer("127.0.0.1:0", b)
		if err != nil {
			b.Fatal(err)
		}

		benchBackends = append(benchBackends, server)
	}
}

func tearDownBench(b *testing.B) {
	for _, s := range benchBackends {
		s.Close()
	}
	benchBackends = nil

	for _, svc := range Registry.svcs {
		Registry.RemoveService(svc.Name)
	}

	benchServer.Close()
	benchRouter.Stop()
}

// Make HTTP calls over the TCP proxy for comparison to ReverseProxy
func BenchmarkTCPProxy(b *testing.B) {
	setupBench(b)
	defer tearDownBench(b)

	svcCfg := client.ServiceConfig{
		Name:         "VHostTest",
		Addr:         "127.0.0.1:9000",
		VirtualHosts: []string{"test-vhost"},
	}

	for _, srv := range benchBackends {
		cfg := client.BackendConfig{
			Addr: srv.addr,
			Name: srv.addr,
		}
		svcCfg.Backends = append(svcCfg.Backends, cfg)
	}

	err := Registry.AddService(svcCfg)
	if err != nil {
		b.Fatal(err)
	}

	req, err := http.NewRequest("GET", "http://127.0.0.1:9000/addr", nil)
	if err != nil {
		b.Fatal(err)
	}

	req.Host = "test-vhost"

	http.DefaultTransport.(*http.Transport).DisableKeepAlives = true
	runtime.GC()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			b.Fatal("Error during GET:", err)
		}
		body, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			b.Fatal("Error during Read:", err)
		}
		if len(body) < 7 {
			b.Fatalf("Error in Response: %s", body)
		}
	}

	runtime.GC()
}
func BenchmarkReverseProxy(b *testing.B) {
	setupBench(b)
	defer tearDownBench(b)

	svcCfg := client.ServiceConfig{
		Name:         "VHostTest",
		Addr:         "127.0.0.1:9000",
		VirtualHosts: []string{"test-vhost"},
	}

	for _, srv := range benchBackends {
		cfg := client.BackendConfig{
			Addr: srv.addr,
			Name: srv.addr,
		}
		svcCfg.Backends = append(svcCfg.Backends, cfg)
	}

	err := Registry.AddService(svcCfg)
	if err != nil {
		b.Fatal(err)
	}

	req, err := http.NewRequest("GET", "http://"+listenAddr+"/addr", nil)
	if err != nil {
		b.Fatal(err)
	}

	req.Host = "test-vhost"
	http.DefaultTransport.(*http.Transport).DisableKeepAlives = false

	runtime.GC()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			b.Fatal("Error during GET:", err)
		}
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			b.Fatal("Error during Read:", err)
		}
		if len(body) < 7 {
			b.Fatalf("Error in Response: %s", body)
		}
	}

	runtime.GC()
}
