package main

// RR is always weighted.
// we don't reduce the weight, we just distrubute exactly "Weight" calls in
// a row
func (s *Service) roundRobin() *Backend {
	s.Lock()
	defer s.Unlock()

	count := len(s.Backends)
	switch count {
	case 0:
		return nil
	case 1:
		// fast track for the single backend case
		return s.Backends[0]
	}

	// we may be out of range if we lost a backend since last connections
	if s.lastBackend >= count {
		s.lastBackend = 0
		s.lastCount = 0
	}

	// if some of the backends are down, we need to cycle through them all
	for i := 0; i < count; i++ {
		backend := s.Backends[s.lastBackend]
		if backend.Up() && s.lastCount < int(backend.Weight) {
			s.lastCount++
			return s.Backends[s.lastBackend]
		}

		s.lastBackend = (s.lastBackend + 1) % count
		s.lastCount = 0
	}
	return nil
}

// LC returns the backend with the least number of active connections
func (s *Service) leastConn() *Backend {
	s.Lock()
	defer s.Unlock()

	count := uint64(len(s.Backends))
	switch count {
	case 0:
		return nil
	case 1:
		// fast track for the single backend case
		return s.Backends[0]
	}

	// return the backend with the least connections, favoring the newer backends.
	least := int64(65536)
	var backend *Backend
	for i, b := range s.Backends {
		if b.Up() && b.Active <= least {
			least = b.Active
			backend = b
			// keep track of this just in case
			s.lastBackend = i
		}
	}

	return backend
}
