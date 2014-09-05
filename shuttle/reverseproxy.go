// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package main

import (
	"bytes"
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

type RequestCallback func(http.ResponseWriter, *http.Request) bool
type ResponseCallback func(http.ResponseWriter, *http.Response, error) bool

// TODO: Create a ProxyRequest type that contains the http.Response, error,
// http.ResponseWriter, and any statistics we may want to track.

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
	OnRequest []RequestCallback

	// These are called in order after the response is obtained from the remote
	// server. The http.Response will be valid even on error. Callbacks may
	// write directly to the client, or modify the response which will be
	// written to the client if all callbacks complete with True. If any
	// callback returns false to stop the chain, the response is discarded.
	OnResponse []ResponseCallback
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

func (p *ReverseProxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {

	for _, f := range p.OnRequest {
		cont := f(rw, req)
		if !cont {
			return
		}
	}

	res, err := p.doRequest(req)

	if err != nil {
		log.Printf("http: proxy error: %v", err)

		// TODO: create a more-filled out response

		// We want to ensure that we have a non-nil response even on error for
		// the OnResponse callbacks.
		res = &http.Response{
			Header:     make(map[string][]string),
			StatusCode: http.StatusBadGateway,
			Status:     http.StatusText(http.StatusBadGateway),
			Body:       ioutil.NopCloser(bytes.NewReader(nil)),
		}
	}

	// make sure this is set correctly
	res.Request = req

	for _, f := range p.OnResponse {
		cont := f(rw, res, err)
		if !cont {
			return
		}
	}

	if err != nil {
		return
	}

	defer res.Body.Close()

	for _, h := range hopHeaders {
		res.Header.Del(h)
	}

	copyHeader(rw.Header(), res.Header)

	rw.WriteHeader(res.StatusCode)
	p.copyResponse(rw, res.Body)
}

func (p *ReverseProxy) doRequest(req *http.Request) (*http.Response, error) {
	transport := p.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	outreq := new(http.Request)
	*outreq = *req // includes shallow copies of maps, but okay

	// Our Dialer will find the correct host, but we need a host for the URL
	outreq.URL.Host = outreq.Host

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
				copyHeader(outreq.Header, req.Header)
				copiedHeaders = true
			}
			outreq.Header.Del(h)
		}
	}

	if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		// If we aren't the first proxy retain prior
		// X-Forwarded-For information as a comma+space
		// separated list and fold multiple headers into one.
		if prior, ok := outreq.Header["X-Forwarded-For"]; ok {
			clientIP = strings.Join(prior, ", ") + ", " + clientIP
		}
		outreq.Header.Set("X-Forwarded-For", clientIP)
	}

	return transport.RoundTrip(outreq)
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
