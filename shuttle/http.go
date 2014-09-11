package main

import (
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

	if Registry.VHostsLen() == 0 {
		http.Error(w, "no backends available", http.StatusServiceUnavailable)
		return
	}

	// TODO: better status lines
	stats := Registry.Stats()
	for _, svc := range stats {
		if len(svc.VirtualHosts) == 0 {
			continue
		}
		fmt.Fprintf(w, "%v\n", svc.VirtualHosts)
		for _, b := range svc.Backends {
			js, _ := json.Marshal(b)
			fmt.Fprintf(w, "\t%s\n", string(js))
		}
	}

	fmt.Fprintf(w, "\n")
	return
}

// TODO: collect more stats?

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

func sslRedirect(pr *ProxyRequest) bool {
	pr.Request.Header.Set("X-Request-Id", genId())

	if sslOnly && pr.Request.Header.Get("X-Forwarded-Proto") != "https" {
		//TODO: verify RequestURI
		redirLoc := "https://" + pr.Request.Host + pr.Request.RequestURI
		http.Redirect(pr.ResponseWriter, pr.Request, redirLoc, http.StatusMovedPermanently)
		return false
	}

	return true
}

type ErrorPage struct {
	// The Mutex protects access to the body slice.
	// Everything else should be static once the ErrorPage is created.
	sync.Mutex

	Location    string
	StatusCodes []int

	// body contains the cached error page
	body []byte
}

func (e *ErrorPage) Body() []byte {
	e.Lock()
	defer e.Unlock()
	return e.body
}

func (e *ErrorPage) SetBody(b []byte) {
	e.Lock()
	defer e.Unlock()
	e.body = b
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

func NewErrorResponse(pages map[string][]int) *ErrorResponse {
	errors := &ErrorResponse{
		pages: make(map[int]*ErrorPage),
	}

	if pages != nil {
		errors.Update(pages)
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

	go errors.update()
	return errors
}

// attempt to fetch and cache all error pages
func (e *ErrorResponse) update() {
	e.Lock()
	// find the set of error pages
	pages := make(map[*ErrorPage]bool)
	for _, page := range e.pages {
		pages[page] = true
	}

	// Unlock while fetching the pages so we don't block any incoming errors
	e.Unlock()

	for page := range pages {
		if page.Body() != nil {
			continue
		}

		e.fetch(page)
	}
}

// Get the error page body
// We permanently cache error pages once we've seen them
func (e *ErrorResponse) Get(code int) []byte {
	e.Lock()
	page, ok := e.pages[code]
	e.Unlock()

	if !ok {
		// this is a code we don't handle
		return nil
	}

	body := page.Body()
	if body != nil {
		return body
	}

	// we haven't successfully fetched this error
	e.fetch(page)
	return page.Body()
}

func (e *ErrorResponse) fetch(page *ErrorPage) {
	log.Debugf("Fetching error page from %s", page.Location)
	resp, err := e.client.Get(page.Location)
	if err != nil {
		log.Warnf("Could not fetch %s: %s", page.Location, err.Error())
		return
	}
	defer resp.Body.Close()

	// If the StatusCode matches any of our registered codes, it's OK
	for _, code := range page.StatusCodes {
		if resp.StatusCode == code {
			break
		}
	}

	if resp.StatusCode != http.StatusOK {
		log.Warnf("Server returned %d when fetching %s", resp.StatusCode, page.Location)
		return
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Warnf("Error reading response from %s: %s", page.Location, err.Error())
		return
	}

	if len(body) > 0 {
		page.SetBody(body)
		return
	}
	log.Warnf("Empty response from %s", page.Location)
}

// This replaces all existing ErrorPages
func (e *ErrorResponse) Update(pages map[string][]int) {
	e.Lock()
	defer e.Unlock()

	e.pages = make(map[int]*ErrorPage)

	for loc, codes := range pages {
		page := &ErrorPage{
			StatusCodes: codes,
			Location:    loc,
		}

		for _, code := range codes {
			e.pages[code] = page
		}
	}
	go e.update()
}

func (e *ErrorResponse) CheckResponse(pr *ProxyRequest) bool {

	errPage := e.Get(pr.Response.StatusCode)
	if errPage != nil {
		pr.ResponseWriter.WriteHeader(pr.Response.StatusCode)
		pr.ResponseWriter.Write(errPage)
		return false
	}

	return true
}

func logProxyRequest(pr *ProxyRequest) bool {
	// TODO: we may to be able to switch this off
	if pr == nil {
		return true
	}

	var id, method, clientIP, url, backend, agent string

	duration := pr.FinishTime.Sub(pr.StartTime)

	if pr.Request != nil {
		id = pr.Request.Header.Get("X-Request-Id")
		method = pr.Request.Method
		clientIP = pr.Request.RemoteAddr
		url = pr.Request.Host + pr.Request.RequestURI
		agent = pr.Request.UserAgent()
	}

	if pr.Response != nil && pr.Response.Request != nil && pr.Response.Request.URL != nil {
		backend = pr.Response.Request.URL.Host
	}

	err := fmt.Sprintf("%v", pr.ProxyError)

	fmtStr := "id=%s method=%s clientIp=%s url=%s backend=%s duration=%s agent=%s, err=%s"

	log.Printf(fmtStr, id, method, clientIP, url, backend, duration, agent, err)
	return true
}
