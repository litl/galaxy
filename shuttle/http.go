package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/litl/galaxy/utils"

	"github.com/litl/galaxy/log"
	gotoolslog "github.com/mailgun/gotools-log"
	"github.com/mailgun/vulcan"
	"github.com/mailgun/vulcan/endpoint"
	"github.com/mailgun/vulcan/loadbalance/roundrobin"
	"github.com/mailgun/vulcan/location/httploc"
	"github.com/mailgun/vulcan/request"
	"github.com/mailgun/vulcan/route"
	"github.com/mailgun/vulcan/route/hostroute"
)

var (
	httpRouter *HTTPRouter
)

type RequestLogger struct{}

type HTTPRouter struct {
	router    *hostroute.HostRouter
	balancers map[string]*roundrobin.RoundRobin
}

func (r *RequestLogger) ObserveRequest(req request.Request) {}

func (r *RequestLogger) ObserveResponse(req request.Request, a request.Attempt) {
	err := ""
	statusCode := ""
	if a.GetError() != nil {
		err = " err=" + a.GetError().Error()
	}

	if a.GetResponse() != nil {
		statusCode = " status=" + strconv.FormatInt(int64(a.GetResponse().StatusCode), 10)
	}

	log.Printf("id=%d method=%s clientIp=%s url=%s backend=%s%s duration=%s%s",
		req.GetId(),
		req.GetHttpRequest().Method,
		req.GetHttpRequest().RemoteAddr,
		req.GetHttpRequest().Host+req.GetHttpRequest().RequestURI,
		a.GetEndpoint(),
		statusCode, a.GetDuration(), err)
}

type SSLRedirect struct{}

func (s *SSLRedirect) ProcessRequest(r request.Request) (*http.Response, error) {
	if sslOnly && r.GetHttpRequest().Header.Get("X-Forwarded-Proto") != "https" {

		resp := &http.Response{
			Status:        "301 Moved Permanently",
			StatusCode:    301,
			Proto:         r.GetHttpRequest().Proto,
			ProtoMajor:    r.GetHttpRequest().ProtoMajor,
			ProtoMinor:    r.GetHttpRequest().ProtoMinor,
			Body:          ioutil.NopCloser(bytes.NewBufferString("")),
			ContentLength: 0,
			Request:       r.GetHttpRequest(),
			Header:        http.Header{},
		}
		resp.Header.Set("Location", "https://"+r.GetHttpRequest().Host+r.GetHttpRequest().RequestURI)
		return resp, nil
	}

	return nil, nil
}

func (s *SSLRedirect) ProcessResponse(r request.Request, a request.Attempt) {
}

func NewHTTPRouter() *HTTPRouter {
	return &HTTPRouter{
		balancers: make(map[string]*roundrobin.RoundRobin),
	}
}

func (s *HTTPRouter) AddBackend(name, vhost, url string) error {

	if vhost == "" || url == "" {
		return nil
	}

	var err error
	balancer := s.balancers[vhost]

	if balancer == nil {
		// Create a round robin load balancer with some endpoints
		balancer, err = roundrobin.NewRoundRobin()
		if err != nil {
			return err
		}

		// Create a http location with the load balancer we've just added
		loc, err := httploc.NewLocationWithOptions(name, balancer,
			httploc.Options{
				TrustForwardHeader: true,
			})
		if err != nil {
			return err
		}
		loc.GetObserverChain().Add("logger", &RequestLogger{})
		loc.GetMiddlewareChain().Add("ssl", 0, &SSLRedirect{})

		s.router.SetRouter(vhost, &route.ConstRouter{Location: loc})
		log.Printf("Starting HTTP listener for %s", vhost)
		s.balancers[vhost] = balancer
	}

	// Already registered?
	if balancer.FindEndpointByUrl(url) != nil {
		return nil
	}
	endpoint := endpoint.MustParseUrl(url)
	log.Printf("Adding HTTP endpoint %s to %s", endpoint.GetUrl(), vhost)
	err = balancer.AddEndpoint(endpoint)
	if err != nil {
		return err
	}
	return nil
}

func (s *HTTPRouter) RemoveBackend(vhost, url string) error {

	if vhost == "" || url == "" {
		return nil
	}

	balancer := s.balancers[vhost]
	if balancer == nil {
		return nil
	}

	endpoint := balancer.FindEndpointByUrl(url)
	if endpoint == nil {
		return nil
	}
	log.Printf("Removing HTTP endpoint %s from %s ", endpoint.GetUrl(), vhost)
	balancer.RemoveEndpoint(endpoint)

	endpoints := balancer.GetEndpoints()
	if len(endpoints) == 0 {
		s.RemoveRouter(vhost)
	}
	return nil
}

// Remove all backends for vhost that are not listed in addrs
func (s *HTTPRouter) RemoveBackends(vhost string, addrs []string) {

	if vhost == "" {
		return
	}

	// Remove backends that are no longer registered

	balancer := s.balancers[vhost]
	if balancer == nil {
		return
	}

	endpoints := balancer.GetEndpoints()
	for _, endpoint := range endpoints {
		if !utils.StringInSlice(endpoint.GetUrl().String(), addrs) {
			s.RemoveBackend(vhost, endpoint.GetUrl().String())
		}
	}
}

// Removes a virtual host router
func (s *HTTPRouter) RemoveRouter(vhost string) {
	if vhost == "" {
		return
	}

	log.Printf("Removing balancer for %s", vhost)
	delete(s.balancers, vhost)
	s.router.RemoveRouter(vhost)
}

func (s *HTTPRouter) adminHandler(w http.ResponseWriter, r *http.Request) {
	if len(s.balancers) == 0 {
		w.WriteHeader(503)
		return
	}

	keys := make([]string, 0, len(s.balancers))
	for key := range s.balancers {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, k := range keys {
		balancer := s.balancers[k]
		endpoints := balancer.GetEndpoints()
		fmt.Fprintf(w, "%s\n", k)
		for _, endpoint := range endpoints {
			fmt.Fprintf(w, "  %s\t%d\t%d\t%0.2f\n", endpoint.GetUrl(), endpoint.GetOriginalWeight(), endpoint.GetEffectiveWeight(), endpoint.GetMeter().GetRate())
		}
	}
}

func (s *HTTPRouter) statusHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		host := r.Host
		if strings.Contains(host, ":") {
			host, _, err = net.SplitHostPort(r.Host)
			if err != nil {
				log.Warnf("%s", err)
				h.ServeHTTP(w, r)
				return
			}
		}

		if _, exists := s.balancers[host]; !exists {
			s.adminHandler(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func (s *HTTPRouter) Start() {

	if debug {
		// init the vulcan logging
		gotoolslog.Init([]*gotoolslog.LogConfig{
			&gotoolslog.LogConfig{Name: "console"},
		})
	}

	log.Printf("HTTP server listening at %s", listenAddr)

	s.router = hostroute.NewHostRouter()

	proxy, err := vulcan.NewProxy(s.router)
	if err != nil {
		log.Fatalf("ERROR: %s", err)
	}

	// Proxy acts as http handler:
	server := &http.Server{
		Addr:           listenAddr,
		Handler:        s.statusHandler(proxy),
		ReadTimeout:    60 * time.Second,
		WriteTimeout:   60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	log.Errorf("%s", server.ListenAndServe())
}

func startHTTPServer() {
	defer wg.Done()
	httpRouter = NewHTTPRouter()
	httpRouter.Start()
}
