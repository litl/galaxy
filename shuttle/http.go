package main

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/litl/galaxy/log"
)

var (
	httpRouter *HostRouter
)

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

	// automatically redirect to https
	SSLOnly bool

	// HTTP/HTTPS
	Scheme string

	// track our listener so we can kill the server
	listener net.Listener
}

func NewHostRouter(httpServer *http.Server) *HostRouter {
	r := &HostRouter{
		Scheme: "http",
	}
	httpServer.Handler = r
	r.server = httpServer
	return r
}

func (r *HostRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	reqId := genId()
	req.Header.Set("X-Request-Id", reqId)
	w.Header().Add("X-Request-Id", reqId)

	if r.SSLOnly {
		if req.TLS != nil || req.Header.Get("X-Forwarded-Proto") != "https" {
			//TODO: verify RequestURI
			redirLoc := "https://" + req.Host + req.RequestURI
			http.Redirect(w, req, redirLoc, http.StatusMovedPermanently)
			return
		}
	}

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

	r.noHostHandler(w, req)
}

func (r *HostRouter) noHostHandler(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintln(w, "Not found")
}

// TODO: collect more stats?

// Start the HTTP Router frontend.
// Takes a channel to notify when the listener is started
// to safely synchronize tests.
func (r *HostRouter) Start(ready chan bool) {
	//FIXME: poor locking strategy
	r.Lock()
	var err error
	r.listener, err = newTimeoutListener("tcp", r.server.Addr, 300*time.Second)
	if err != nil {
		log.Errorf("%s", err)
		r.Unlock()
		return
	}

	listener := r.listener
	if r.Scheme == "https" {
		listener = tls.NewListener(listener, r.server.TLSConfig)
	}

	r.Unlock()

	log.Printf("%s server listening at %s", strings.ToUpper(r.Scheme), r.server.Addr)
	if ready != nil {
		close(ready)
	}

	// This will log a closed connection error every time we Stop
	// but that's mostly a testing issue.
	log.Errorf("%s", r.server.Serve(listener))
}

func (r *HostRouter) Stop() {
	r.listener.Close()
}

func startHTTPServer(wg *sync.WaitGroup) {
	defer wg.Done()

	//TODO: configure these timeouts somewhere
	httpServer := &http.Server{
		Addr:           httpAddr,
		ReadTimeout:    10 * time.Minute,
		WriteTimeout:   10 * time.Minute,
		MaxHeaderBytes: 1 << 20,
	}

	httpRouter = NewHostRouter(httpServer)
	httpRouter.SSLOnly = sslOnly

	httpRouter.Start(nil)
}

// find certs in and is the named directory, and match them up by their base
// name using '.pem' and '.key' as extensions.
func loadCerts(certDir string) (*tls.Config, error) {
	abs, err := filepath.Abs(certDir)
	if err != nil {
		return nil, err
	}

	dir, err := ioutil.ReadDir(abs)
	if err != nil {
		return nil, err
	}

	// [cert, key] pairs
	pairs := make(map[string][2]string)

	for _, f := range dir {
		name := f.Name()
		if strings.HasSuffix(name, ".pem") {
			p := pairs[name[:len(name)-4]]
			p[0] = filepath.Join(abs, name)
			pairs[name[:len(name)-4]] = p
		}
		if strings.HasSuffix(name, ".key") {
			p := pairs[name[:len(name)-4]]
			p[1] = filepath.Join(abs, name)
			pairs[name[:len(name)-4]] = p
		}
	}

	tlsCfg := &tls.Config{
		NextProtos: []string{"http/1.1"},
	}

	for key, pair := range pairs {
		if pair[0] == "" {
			log.Errorf("missing cert for key: %s", pair[1])
			continue
		}
		if pair[1] == "" {
			log.Errorf("missing key for cert: %s", pair[0])
			continue
		}

		cert, err := tls.LoadX509KeyPair(pair[0], pair[1])
		if err != nil {
			log.Error(err)
			continue
		}
		tlsCfg.Certificates = append(tlsCfg.Certificates, cert)
		log.Debugf("loaded X509KeyPair for %s", key)
	}

	if len(tlsCfg.Certificates) == 0 {
		return nil, fmt.Errorf("no tls certificates loaded")
	}

	tlsCfg.BuildNameToCertificate()

	return tlsCfg, nil
}

func startHTTPSServer(wg *sync.WaitGroup) {
	defer wg.Done()

	tlsCfg, err := loadCerts(certDir)
	if err != nil {
		log.Error(err)
		return
	}

	//TODO: configure these timeouts somewhere
	httpsServer := &http.Server{
		Addr:           httpsAddr,
		ReadTimeout:    10 * time.Minute,
		WriteTimeout:   10 * time.Minute,
		MaxHeaderBytes: 1 << 20,
		TLSConfig:      tlsCfg,
	}

	httpRouter = NewHostRouter(httpsServer)
	httpRouter.Scheme = "https"
	httpRouter.SSLOnly = sslOnly

	httpRouter.Start(nil)
}

type ErrorPage struct {
	// The Mutex protects access to the body slice, and headers
	// Everything else should be static once the ErrorPage is created.
	sync.Mutex

	Location    string
	StatusCodes []int

	// body contains the cached error page
	body []byte
	// important headers
	header http.Header
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

func (e *ErrorPage) Header() http.Header {
	e.Lock()
	defer e.Unlock()
	return e.header
}

func (e *ErrorPage) SetHeader(h http.Header) {
	e.Lock()
	defer e.Unlock()
	e.header = h
}

// List of headers we want to cache for ErrorPages
var ErrorHeaders = []string{
	"Content-Type",
	"Content-Encoding",
	"Cache-Control",
	"Last-Modified",
	"Retry-After",
	"Set-Cookie",
}

// ErrorResponse provides a ReverProxy callback to process a response and
// insert custom error pages for a virtual host.
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

	if pages != nil {
		errors.Update(pages)
	}
	return errors
}

// Get the ErrorPage, returning nil if the page was incomplete.
// We permanently cache error pages and headers once we've seen them.
func (e *ErrorResponse) Get(code int) *ErrorPage {
	e.Lock()
	page, ok := e.pages[code]
	e.Unlock()

	if !ok {
		// this is a code we don't handle
		return nil
	}

	body := page.Body()
	if body != nil {
		return page
	}

	// we haven't successfully fetched this error
	e.fetch(page)
	return page
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
			resp.StatusCode = http.StatusOK
			break
		}
	}

	if resp.StatusCode != http.StatusOK {
		log.Warnf("Server returned %d when fetching %s", resp.StatusCode, page.Location)
		return
	}

	header := make(map[string][]string)
	for _, key := range ErrorHeaders {
		if hdr, ok := resp.Header[key]; ok {
			header[key] = hdr
		}
	}
	// set the headers along with the body below

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Warnf("Error reading response from %s: %s", page.Location, err.Error())
		return
	}

	if len(body) > 0 {
		page.SetHeader(header)
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
		go e.fetch(page)
	}
}

func (e *ErrorResponse) CheckResponse(pr *ProxyRequest) bool {

	errPage := e.Get(pr.Response.StatusCode)
	if errPage != nil {
		// load the cached headers
		header := pr.ResponseWriter.Header()
		for key, val := range errPage.Header() {
			header[key] = val
		}

		pr.ResponseWriter.WriteHeader(pr.Response.StatusCode)
		pr.ResponseWriter.Write(errPage.Body())
		return false
	}

	return true
}

func logProxyRequest(pr *ProxyRequest) bool {
	// TODO: we may to be able to switch this off
	if pr == nil || pr.Request == nil {
		return true
	}

	var id, method, clientIP, url, backend, agent string
	var status int

	duration := pr.FinishTime.Sub(pr.StartTime)

	id = pr.Request.Header.Get("X-Request-Id")
	method = pr.Request.Method
	url = pr.Request.Host + pr.Request.RequestURI
	agent = pr.Request.UserAgent()
	status = pr.Response.StatusCode

	clientIP = pr.Request.Header.Get("X-Forwarded-For")
	if clientIP == "" {
		clientIP = pr.Request.RemoteAddr
	}

	if pr.Response != nil && pr.Response.Request != nil && pr.Response.Request.URL != nil {
		backend = pr.Response.Request.URL.Host
	}

	err := fmt.Sprintf("%v", pr.ProxyError)

	fmtStr := "id=%s method=%s client-ip=%s url=%s backend=%s status=%d duration=%s agent=%s, err=%s"

	log.Printf(fmtStr, id, method, clientIP, url, backend, status, duration, agent, err)
	return true
}
