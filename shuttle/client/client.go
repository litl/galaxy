package client

import (
	"encoding/json"
	"reflect"
)

// The subset of fields we load and serialize for config.
type BackendConfig struct {
	Name      string `json:"name"`
	Addr      string `json:"address"`
	CheckAddr string `json:"check_address"`
	Weight    int    `json:"weight"`
}

func (b BackendConfig) Equal(other BackendConfig) bool {
	if other.Weight == 0 {
		other.Weight = 1
	}
	return b == other
}

func (b *BackendConfig) Marshal() []byte {
	js, _ := json.Marshal(&b)
	return js
}

func (b *BackendConfig) String() string {
	return string(b.Marshal())
}

// Subset of service fields needed for configuration.
type ServiceConfig struct {
	Name          string           `json:"name"`
	Addr          string           `json:"address"`
	VirtualHosts  []string         `json:"virtual_hosts,omitempty"`
	Backends      []BackendConfig  `json:"backends,omitempty"`
	Balance       string           `json:"balance,omitempty"`
	CheckInterval int              `json:"check_interval"`
	Fall          int              `json:"fall"`
	Rise          int              `json:"rise"`
	ClientTimeout int              `json:"client_timeout"`
	ServerTimeout int              `json:"server_timeout"`
	DialTimeout   int              `json:"connect_timeout"`
	ErrorPages    map[string][]int `json:"error_pages"`
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

func (b *ServiceConfig) Marshal() []byte {
	js, _ := json.Marshal(&b)
	return js
}

func (b *ServiceConfig) String() string {
	return string(b.Marshal())
}
