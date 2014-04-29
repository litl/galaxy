package main

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

	// return the backend with the least connections, favoring the newer backends.
	least := int64(65536)
	var balanced []*Backend
	var backend *Backend
	for i, b := range s.Backends {
		if b.Up() && b.Active <= least {
			least = b.Active
			backend = b
			// keep track of this just in case
			s.lastBackend = (i + 1) % count
		}
	}

	if backend == nil {
		return nil
	}

	balanced = append(balanced, backend)

	// Now add the rest of the available backends in order, in case the first
	// connect fails.

	// FIXME: a broken backend will cause the next one in the list to get
	// hammered. We need to sort them based on Active!
	lastBackend := s.lastBackend
	for i := 0; i < count-1; i++ {
		backend := s.Backends[lastBackend]
		if backend.Up() {
			balanced = append(balanced, backend)
		}
		lastBackend = (lastBackend + 1) % count
	}

	return balanced
}
