package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"
	"time"
)

var (
	// slice these up to get ranges of codes for error pages
	Status400s = []int{400, 401, 402, 403, 404, 405, 406, 407, 408, 409, 410, 411, 412, 413, 414, 415, 416, 417, 418}
	Status500s = []int{500, 501, 502, 503, 504, 505}
)

type Client struct {
	httpClient  *http.Client
	shuttleAddr string
}

// Global config which applies to all Services
type Config struct {
	Balance       string          `json:"balance,omitempty"`
	CheckInterval int             `json:"check_interval"`
	Fall          int             `json:"fall"`
	Rise          int             `json:"rise"`
	ClientTimeout int             `json:"client_timeout"`
	ServerTimeout int             `json:"server_timeout"`
	DialTimeout   int             `json:"connect_timeout"`
	Services      []ServiceConfig `json:"services"`
}

func (c *Config) Marshal() []byte {
	js, _ := json.Marshal(c)
	return js
}

func (c *Config) String() string {
	return string(c.Marshal())
}

// The subset of fields we load and serialize for config.
type BackendConfig struct {
	Name      string `json:"name"`
	Addr      string `json:"address"`
	CheckAddr string `json:"check_address"`
	Weight    int    `json:"weight"`
	Network   string `json:"network,omitempty"`
}

func (b BackendConfig) Equal(other BackendConfig) bool {
	if other.Weight == 0 {
		other.Weight = 1
	}

	if b.Weight == 0 {
		b.Weight = 1
	}

	if b.Network == "" {
		b.Network = "tcp"
	}

	if other.Network == "" {
		other.Network = "tcp"
	}

	return b == other
}

func (b *BackendConfig) Marshal() []byte {
	js, _ := json.Marshal(b)
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
	ErrorPages    map[string][]int `json:"error_pages,omitempty"`
	Network       string           `json:"network,omitempty"`
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
	if other.CheckInterval == 0 {
		other.CheckInterval = 2000
	}
	if s.Rise == 0 {
		s.Rise = 2
	}
	if other.Rise == 0 {
		other.Rise = 2
	}
	if s.Fall == 0 {
		s.Fall = 2
	}
	if other.Fall == 0 {
		other.Fall = 2
	}

	if s.Network == "" {
		s.Network = "tcp"
	}

	// We handle backends separately
	s.Backends = nil
	other.Backends = nil

	return reflect.DeepEqual(s, other)
}

func (b *ServiceConfig) Marshal() []byte {
	js, _ := json.Marshal(b)
	return js
}

func (b *ServiceConfig) String() string {
	return string(b.Marshal())
}

// Check for equality including backends
func (s ServiceConfig) DeepEqual(other ServiceConfig) bool {
	if len(s.Backends) != len(other.Backends) {
		return false
	}

	if !s.Equal(other) {
		return false
	}

	// O(n^2), but we shouldn't have many backends
	for _, a := range s.Backends {
	NEXT:
		for _, b := range other.Backends {
			if a.Name == b.Name {
				if a.Equal(b) {
					break NEXT
				} else {
					return false
				}
			}
		}
	}

	return true
}

func NewClient(addr string) *Client {
	transport := &http.Transport{ResponseHeaderTimeout: 2 * time.Second}
	httpClient := &http.Client{Transport: transport}
	return &Client{
		httpClient:  httpClient,
		shuttleAddr: addr,
	}
}

func (c *Client) GetConfig() (*Config, error) {

	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/_config", c.shuttleAddr), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	config := &Config{}
	err = json.Unmarshal(body, config)
	if err != nil {
		return nil, err
	}

	return config, nil
}

func (c *Client) UpdateService(name string, service *ServiceConfig) error {

	js, err := json.Marshal(service)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Post(fmt.Sprintf("http://%s/%s", c.shuttleAddr, name), "application/json",
		bytes.NewBuffer(js))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to register service with shuttle: %s", resp.Status)
	}
	return nil
}

func (c *Client) UnregisterService(service *ServiceConfig) error {
	js, err := json.Marshal(service)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("DELETE", fmt.Sprintf("http://%s/%s", c.shuttleAddr, service.Name), bytes.NewBuffer(js))
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New(fmt.Sprintf("failed to unregister service: %s", resp.Status))
	}
	return nil
}

func (c *Client) UnregisterBackend(service, backend string) error {
	req, err := http.NewRequest("DELETE", fmt.Sprintf("http://%s/%s/%s", c.shuttleAddr, service, backend), nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New(fmt.Sprintf("failed to unregister backend: %s", resp.Status))
	}
	return nil
}
