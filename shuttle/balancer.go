package main

import (
	"sort"
	"sync/atomic"
)

// Balancing functions return a slice of all known available backends, in
// priority order.  This way the service can cycle through backends if the
// initial connections fails.

// RR is always weighted.
// we don't reduce the weight, we just distrubute exactly "Weight" calls in
// a row
func (s *Service) roundRobin() []*Backend {
	s.Lock()
	defer s.Unlock()

	count := len(s.Backends)
	switch count {
	case 0:
		return nil
	case 1:
		// fast track for the single backend case
		return s.Backends[0:1]
	}

	// we may be out of range if we lost a backend since last connections
	if s.lastBackend >= count {
		s.lastBackend = 0
		s.lastCount = 0
	}

	var balanced []*Backend
	// Find the next Up backend to call
	for i := 0; i < count; i++ {
		backend := s.Backends[s.lastBackend]
		if backend.Up() && s.lastCount < int(backend.Weight) {
			s.lastCount++
			balanced = append(balanced, backend)

			break
		}

		s.lastBackend = (s.lastBackend + 1) % count
		s.lastCount = 0
	}

	if len(balanced) == 0 {
		return nil
	}

	// Now add the rest of the available backends in order, in case the first
	// connect fails
	lastBackend := s.lastBackend
	for i := 0; i < count-1; i++ {
		lastBackend = (lastBackend + 1) % count
		backend := s.Backends[lastBackend]
		if backend.Up() {
			balanced = append(balanced, backend)
		}
	}

	return balanced
}

// LC returns the backend with the least number of active connections
func (s *Service) leastConn() []*Backend {
	s.Lock()
	defer s.Unlock()

	count := len(s.Backends)
	switch count {
	case 0:
		return nil
	case 1:
		// fast track for the single backend case
		return s.Backends[0:1]
	}

	// return the backends in the order of least connections
	var balanced []*Backend

	// Accumulate all backends that are currently Up
	for _, b := range s.Backends {
		if b.Up() {
			balanced = append(balanced, b)
		}
	}

	if len(balanced) == 0 {
		return nil
	}

	sort.Sort(ByActive(balanced))

	return balanced
}

type ByActive []*Backend

func (s ByActive) Len() int      { return len(s) }
func (s ByActive) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s ByActive) Less(i, j int) bool {
	iActive := atomic.LoadInt64(&(s[i].Active))
	jActive := atomic.LoadInt64(&(s[j].Active))
	return iActive < jActive
}
