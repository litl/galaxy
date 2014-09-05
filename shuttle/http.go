package main

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/litl/galaxy/log"
)

var (
	httpRouter *HostRouter
)

type RequestLogger struct{}

type HostRouter struct {
	sync.Mutex
	// the http frontend
	server *http.Server

	// track our listener so we can kill the server
	listener net.Listener
}

func NewHostRouter() *HostRouter {
	return &HostRouter{}
}

func (r *HostRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var err error
	host := req.Host
	if strings.Contains(host, ":") {
		host, _, err = net.SplitHostPort(req.Host)
		if err != nil {
			log.Warnf("%s", err)
		}
	}

	svc := Registry.GetVHostService(host)

	if svc != nil && svc.httpProxy != nil {
		// The vhost has a service registered, give it to the proxy
		svc.ServeHTTP(w, req)
		return
	}

	r.adminHandler(w, req)
}

func (r *HostRouter) adminHandler(w http.ResponseWriter, req *http.Request) {
	r.Lock()
	defer r.Unlock()

	vhosts := Registry.GetVHosts()

	if len(vhosts) == 0 {
		http.Error(w, "no backends available", http.StatusServiceUnavailable)
		return
	}

	http.Error(w, "ADMIN NOT IMPLEMENTED", http.StatusServiceUnavailable)
	return
}

// Start the HTTP Router frontend.
// Takes a channel to notify when the listener is started
// to safely synchronize tests.
func (r *HostRouter) Start(ready chan bool) {
	//FIXME: poor locking strategy
	r.Lock()

	log.Printf("HTTP server listening at %s", listenAddr)

	// Proxy acts as http handler:
	r.server = &http.Server{
		Addr:           listenAddr,
		Handler:        r,
		ReadTimeout:    60 * time.Second,
		WriteTimeout:   60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	var err error
	r.listener, err = net.Listen("tcp", listenAddr)
	if err != nil {
		log.Errorf("%s", err)
		r.Unlock()
		return
	}

	r.Unlock()
	if ready != nil {
		close(ready)
	}

	// This will log a closed connection error every time we Stop
	// but that's mostly a testing issue.
	log.Errorf("%s", r.server.Serve(r.listener))
}

func (r *HostRouter) Stop() {
	r.listener.Close()
}

func startHTTPServer() {
	//FIXME: this global wg?
	defer wg.Done()
	httpRouter = NewHostRouter()
	httpRouter.Start(nil)
}

/*

TODO: implement more existing functionality

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

	log.Printf("cnt=%d id=%s method=%s clientIp=%s url=%s backend=%s%s duration=%s agent=%s%s",
		req.GetId(),
		req.GetHttpRequest().Header.Get("X-Request-Id"),
		req.GetHttpRequest().Method,
		req.GetHttpRequest().RemoteAddr,
		req.GetHttpRequest().Host+req.GetHttpRequest().RequestURI,
		a.GetEndpoint(),
		statusCode, a.GetDuration(),
		req.GetHttpRequest().UserAgent(), err)
}

type SSLRedirect struct{}

func genId() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func (s *SSLRedirect) ProcessRequest(r request.Request) (*http.Response, error) {
	r.GetHttpRequest().Header.Set("X-Request-Id", genId())

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
*/
