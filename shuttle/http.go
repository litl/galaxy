package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/ioutil"
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

// This works along with the ServiceRegistry, and the individual Services to
// route http requests based on the Host header. The Resgistry hold the mapping
// of VHost names to individual services, and each service has it's own
// ReeverseProxy to fulfill the request.
// HostRouter contains the ReverseProxy http Listener, and has an http.Handler
// to service the requets.
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

	// TODO: better status lines
	stats := Registry.Stats()
	for _, svc := range stats {
		fmt.Fprintf(w, "%v\n", svc.VirtualHosts)
		for _, b := range svc.Backends {
			js, _ := json.Marshal(b)
			fmt.Fprintf(w, "  %s", string(js))
		}
	}

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

// TODO: request logging

func genId() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func sslRedirect(rw http.ResponseWriter, req *http.Request) bool {
	req.Header.Set("X-Request-Id", genId())

	if sslOnly && req.Header.Get("X-Forwarded-Proto") != "https" {
		//TODO: verify RequestURI
		http.Redirect(rw, req, "https://"+req.Host+req.RequestURI, http.StatusMovedPermanently)
		return false
	}

	return true
}

type ErrorPage struct {
	Location    string
	StatusCodes []int
	Body        []byte
}

// ErrorResponse provides vulcan middleware to process a response and insert
// custom error pages for a virtual host.
type ErrorResponse struct {
	sync.Mutex

	// map them by status for responses
	pages map[int]*ErrorPage

	// keep this handy to refresh the pages
	client *http.Client
}

func NewErrorResponse() *ErrorResponse {
	errors := &ErrorResponse{
		pages: make(map[int]*ErrorPage),
	}

	// aggressively timeout connections
	errors.client = &http.Client{
		Transport: &http.Transport{
			Dial: (&net.Dialer{
				Timeout: 2 * time.Second,
			}).Dial,
			TLSHandshakeTimeout: 2 * time.Second,
		},
		Timeout: 5 * time.Second,
	}

	return errors
}

// Get the error page body
// We permanently cache error pages once we've seen them
func (e *ErrorResponse) Get(code int) []byte {
	e.Lock()
	defer e.Unlock()

	page, ok := e.pages[code]
	if !ok {
		// this is a code we don't handle
		return nil
	}

	if page.Body != nil {
		return page.Body
	}

	// we've never fetched this error
	var err error
	page.Body, err = e.fetch(page.Location)
	if err != nil {
		// TODO: log error?
		return nil
	}

	return page.Body
}

func (e *ErrorResponse) fetch(location string) ([]byte, error) {
	fmt.Println("FETCHING ERROR", location)
	resp, err := e.client.Get(location)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func (e *ErrorResponse) Add(codes []int, location string) {
	e.Lock()
	defer e.Unlock()

	page := &ErrorPage{
		StatusCodes: codes,
		Location:    location,
	}

	for _, code := range codes {
		e.pages[code] = page
	}
}

func (e *ErrorResponse) CheckResponse(rw http.ResponseWriter, res *http.Response, resErr error) bool {
	log.Println("DEBUG: StatusCode:", res.StatusCode)

	errPage := e.Get(res.StatusCode)
	if errPage != nil {
		rw.WriteHeader(res.StatusCode)
		rw.Write(errPage)
		return false
	}

	return true
}
