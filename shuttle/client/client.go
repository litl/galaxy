package client

import "reflect"

// The subset of fields we load and serialize for config.
type BackendConfig struct {
	Name      string `json:"name"`
	Addr      string `json:"address"`
	CheckAddr string `json:"check_address"`
	Weight    int    `json:"weight"`
}

// Subset of service fields needed for configuration.
type ServiceConfig struct {
	Name          string          `json:"name"`
	Addr          string          `json:"address"`
	VirtualHosts  []string        `json:"virtual_hosts"`
	Backends      []BackendConfig `json:"backends"`
	Balance       string          `json:"balance"`
	CheckInterval int             `json:"check_interval"`
	Fall          int             `json:"fall"`
	Rise          int             `json:"rise"`
	ClientTimeout int             `json:"client_timeout"`
	ServerTimeout int             `json:"server_timeout"`
	DialTimeout   int             `json:"connect_timeout"`
}

func (b BackendConfig) Equal(other BackendConfig) bool {
	if other.Weight == 0 {
		other.Weight = 1
	}
	return b == other
}

// Compare a service's settings, ignoring individual backends.
func (s ServiceConfig) Equal(other ServiceConfig) bool {
	// just remove the backends and compare the rest
	s.Backends = nil
	other.Backends = nil

	// FIXME: Normalize default in one place!

	if s.Balance != other.Balance {
		if s.Balance == "" && other.Balance == "RR" {
			other.Balance = ""
		} else if s.Balance == "RR" && other.Balance == "" {
			other.Balance = "RR"
		}
	}

	if s.CheckInterval == 0 {
		s.CheckInterval = 2000
	}
	if s.Rise == 0 {
		s.Rise = 2
	}
	if s.Fall == 0 {
		s.Fall = 2
	}

	// We handle backends separately
	s.Backends = nil
	other.Backends = nil

	return reflect.DeepEqual(s, other)
}
