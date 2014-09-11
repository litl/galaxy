// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// onExitFlushLoop is a callback set by tests to detect the state of the
// flushLoop() goroutine.
var onExitFlushLoop func()

type ProxyCallback func(*ProxyRequest) bool

// A Dialer can return an error wrapped in DialError to notify the ReverseProxy
// that an error occured during the initial TCP connection, and it's safe to
// try again.
type DialError struct {
	error
}

// ReverseProxy is an HTTP Handler that takes an incoming request and
// sends it to another server, proxying the response back to the
// client.
type ReverseProxy struct {
	// we need to protect our ErrorPage cache
	sync.Mutex

	// Director must be a function which modifies
	// the request into a new request to be sent
	// using Transport. Its response is then copied
	// back to the original client unmodified.
	Director func(*http.Request)

	// The transport used to perform proxy requests.
	// If nil, http.DefaultTransport is used.
	Transport http.RoundTripper

	// FlushInterval specifies the flush interval
	// to flush to the client while copying the
	// response body.
	// If zero, no periodic flushing is done.
	FlushInterval time.Duration

	// These are called in order on before any request is made to the backend server.
	// Each Callback must return true to continue processing.
	OnRequest []ProxyCallback

	// These are called in order after the response is obtained from the remote
	// server. The http.Response will be valid even on error. Callbacks may
	// write directly to the client, or modify the response which will be
	// written to the client if all callbacks complete with True. If any
	// callback returns false to stop the chain, the response is discarded.
	OnResponse []ProxyCallback
}

// Create a new ReverseProxy
// This will still need to have a Director and Transport assigned.
func NewReverseProxy() *ReverseProxy {
	p := &ReverseProxy{}
	return p
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// Hop-by-hop headers. These are removed when sent to the backend.
// http://www.w3.org/Protocols/rfc2616/rfc2616-sec13.html
var hopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te", // canonicalized version of "TE"
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
}

// This probably shouldn't be called ServeHTTP anymore
func (p *ReverseProxy) ServeHTTP(rw http.ResponseWriter, req *http.Request, addrs []string) {

	pr := &ProxyRequest{
		ResponseWriter: rw,
		Request:        req,
		Backends:       addrs,
	}

	for _, f := range p.OnRequest {
		cont := f(pr)
		if !cont {
			return
		}
	}

	pr.StartTime = time.Now()
	res, err := p.doRequest(pr)

	pr.Response = res
	pr.ProxyError = err
	pr.FinishTime = time.Now()

	if err != nil {
		log.Printf("http: proxy error: %v", err)

		// We want to ensure that we have a non-nil response even on error for
		// the OnResponse callbacks. If the Callback chain completes, this will
		// be written to the client.
		res = &http.Response{
			Header:     make(map[string][]string),
			StatusCode: http.StatusBadGateway,
			Status:     http.StatusText(http.StatusBadGateway),
			// this ensures Body isn't nil
			Body: ioutil.NopCloser(bytes.NewReader(nil)),
		}
		pr.Response = res
	}

	for _, h := range hopHeaders {
		res.Header.Del(h)
	}

	copyHeader(rw.Header(), res.Header)

	for _, f := range p.OnResponse {
		cont := f(pr)
		if !cont {
			return
		}
	}

	// calls all completed with true, write the Response back to the client.
	defer res.Body.Close()
	rw.WriteHeader(res.StatusCode)
	p.copyResponse(rw, res.Body)
}

func (p *ReverseProxy) doRequest(pr *ProxyRequest) (*http.Response, error) {
	transport := p.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	outreq := new(http.Request)
	*outreq = *pr.Request // includes shallow copies of maps, but okay

	p.Director(outreq)
	outreq.Proto = "HTTP/1.1"
	outreq.ProtoMajor = 1
	outreq.ProtoMinor = 1
	outreq.Close = false

	// Remove hop-by-hop headers to the backend.  Especially
	// important is "Connection" because we want a persistent
	// connection, regardless of what the client sent to us.  This
	// is modifying the same underlying map from req (shallow
	// copied above) so we only copy it if necessary.
	copiedHeaders := false
	for _, h := range hopHeaders {
		if outreq.Header.Get(h) != "" {
			if !copiedHeaders {
				outreq.Header = make(http.Header)
				copyHeader(outreq.Header, pr.Request.Header)
				copiedHeaders = true
			}
			outreq.Header.Del(h)
		}
	}

	if clientIP, _, err := net.SplitHostPort(pr.Request.RemoteAddr); err == nil {
		// If we aren't the first proxy retain prior
		// X-Forwarded-For information as a comma+space
		// separated list and fold multiple headers into one.
		if prior, ok := outreq.Header["X-Forwarded-For"]; ok {
			clientIP = strings.Join(prior, ", ") + ", " + clientIP
		}
		outreq.Header.Set("X-Forwarded-For", clientIP)
	}

	var err error
	var resp *http.Response

	for _, addr := range pr.Backends {
		outreq.URL.Host = addr
		resp, err = transport.RoundTrip(outreq)
		if err == nil {
			return resp, nil
		}

		if _, ok := err.(DialError); ok {
			// only Dial failed, so we can try again
			continue
		}

		// not a DialError, so make this terminal.
		return nil, err
	}

	// In this case, our last backend returned a DialError
	if err != nil {
		return nil, err
	}

	// probably shouldn't get here
	return nil, fmt.Errorf("no http backends available")
}

func (p *ReverseProxy) copyResponse(dst io.Writer, src io.Reader) {
	if p.FlushInterval != 0 {
		if wf, ok := dst.(writeFlusher); ok {
			mlw := &maxLatencyWriter{
				dst:     wf,
				latency: p.FlushInterval,
				done:    make(chan bool),
			}
			go mlw.flushLoop()
			defer mlw.stop()
			dst = mlw
		}
	}

	io.Copy(dst, src)
}

type writeFlusher interface {
	io.Writer
	http.Flusher
}

type maxLatencyWriter struct {
	dst     writeFlusher
	latency time.Duration

	lk   sync.Mutex // protects Write + Flush
	done chan bool
}

func (m *maxLatencyWriter) Write(p []byte) (int, error) {
	m.lk.Lock()
	defer m.lk.Unlock()
	return m.dst.Write(p)
}

func (m *maxLatencyWriter) flushLoop() {
	t := time.NewTicker(m.latency)
	defer t.Stop()
	for {
		select {
		case <-m.done:
			if onExitFlushLoop != nil {
				onExitFlushLoop()
			}
			return
		case <-t.C:
			m.lk.Lock()
			m.dst.Flush()
			m.lk.Unlock()
		}
	}
}

func (m *maxLatencyWriter) stop() { m.done <- true }

// Proxy Request stores a client request, backend response, error, and any
// stats needed to complete a round trip.
type ProxyRequest struct {
	// The incoming request from the client
	Request *http.Request

	// The Client's ResponseWriter
	ResponseWriter http.ResponseWriter

	// The response, if any, from the backend server
	Response *http.Response

	// The error, if any, from the http request to the backend server
	ProxyError error

	// backend hosts we can use
	Backends []string

	// Duration of the backend request
	StartTime  time.Time
	FinishTime time.Time
}
