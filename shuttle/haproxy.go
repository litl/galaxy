package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"text/template"
)

var (
	globalCfg   string
	backendCfg  string
	haproxyPid  string
	haproxyOpts []string
	haproxy     string
)

var listenTmpl = `{{range .}}listen {{.Name}}
    bind {{.Addr}}
    {{range .Servers}}server {{.Name}} {{.Addr}} check
    {{end}}
{{end}}`

// The listeners for the haproxy config
// we'll keep the global defaults in a separate file
type Listener struct {
	Name    string
	Addr    string
	Servers map[string]*Server
}

type Server struct {
	Name string
	Addr string
}

type HaproxyCfg struct {
	ConfigTemplate *template.Template
	Listeners      map[string]*Listener
	buff           *bytes.Buffer
}

func NewHaproxyCfg() *HaproxyCfg {
	cfg := &HaproxyCfg{
		Listeners: make(map[string]*Listener),
		buff:      new(bytes.Buffer),
	}
	var err error
	cfg.ConfigTemplate, err = template.New("haproxy template").Parse(listenTmpl)
	if err != nil {
		panic(err)
	}
	return cfg
}

func (c *HaproxyCfg) UpdateListener(key, value string) (changed bool) {
	parts := strings.Split(strings.TrimLeft(key, "/"), "/")
	name := parts[2]

	if value == "" {
		// Don't remove listeners yet
	}

	env := ServiceCfg{}
	err := json.Unmarshal([]byte(value), &env)
	if err != nil {
		log.Println("error unmarshaling service/environment:", err)
		return false
	}
	if env.Port == "" {
		log.Println("no port for service", name)
		return true
	}

	listener, ok := c.Listeners[name]
	if !ok {
		listener = &Listener{Servers: make(map[string]*Server)}
		c.Listeners[name] = listener
	}

	addr := "127.0.0.1:" + env.Port
	if listener.Name != name || listener.Addr != addr {
		changed = true
		listener.Name = name
		listener.Addr = addr
		log.Println("Updating listener", name)
	}
	return changed
}

func (c *HaproxyCfg) UpdateServer(key, value string) (changed bool) {
	parts := strings.Split(strings.TrimLeft(key, "/"), "/")
	name := parts[3]
	service := parts[4]

	if value == "" {
		// we're deleting this server
		if l, ok := c.Listeners[service]; ok {
			// todo notify of changes
			log.Println("removing server", name, "from", service)
			delete(l.Servers, name)
			return true
		}
		return false
	}

	location := ServiceCfg{}
	err := json.Unmarshal([]byte(value), &location)
	if err != nil {
		log.Println("error unmarshaling server location:", err)
		return false
	}

	if location.ExternalIP == "" || location.ExternalPort == "" {
		log.Println("invalid location for server", name)
		return false
	}

	listener, ok := c.Listeners[service]
	if !ok {
		listener = &Listener{
			Servers: make(map[string]*Server),
		}
		changed = true
	}

	server, ok := listener.Servers[name]
	if !ok {
		server = &Server{}
	}

	addr := location.ExternalIP + ":" + location.ExternalPort

	if server.Name != name || server.Addr != addr {
		changed = true
	}

	if changed {
		server.Name = name
		server.Addr = addr
		listener.Servers[name] = server
		log.Println("Adding server to", service, *server)
	}

	return changed
}

// Execute the template and return the bytes for the config file
func (c *HaproxyCfg) Bytes() []byte {
	c.buff.Reset()
	err := c.ConfigTemplate.Execute(c.buff, c.Listeners)
	if err != nil {
		log.Println("error executing haproxy template:", err)
	}
	return c.buff.Bytes()
}

// collect config changes through the updates channel
func haproxyUpdater(updates chan []byte) {
	for {
		update := <-updates
		updateHaproxyCfg(update)
	}
}

func updateHaproxyCfg(config []byte) {
	if len(config) == 0 {
		log.Println("no config to write")
		return
	}

	currCfg, _ := ioutil.ReadFile(backendCfg)
	if bytes.Equal(currCfg, config) {
		log.Println("no change to config")
		return
	}

	tmpCfg, err := os.Create(backendCfg + "~next")
	if err != nil {
		log.Println("cannot create temp config:", err)
		return
	}
	defer os.Remove(tmpCfg.Name())
	defer tmpCfg.Close()

	if _, err := tmpCfg.Write(config); err != nil {
		log.Println("error writing temp config:", err)
		return
	}

	oldCfg, err := os.Create(backendCfg + "~prev")
	if err != nil {
		log.Println("cannot create backup config:", err)
		return
	}
	defer oldCfg.Close()

	_, err = oldCfg.Write(currCfg)
	if err != nil {
		log.Println("error writing backup config:", err)
		return
	}

	if err := os.Rename(tmpCfg.Name(), backendCfg); err != nil {
		log.Println("error creating new config:", err)
		return
	}

	if restartHaproxy() != nil {
		log.Println("rolling back config")
		os.Rename(oldCfg.Name(), backendCfg)
	}
}

// Start or Restart HAProxy as needed
func restartHaproxy() error {
	if _, err := os.Stat(backendCfg); os.IsNotExist(err) {
		log.Println("no backend config, not starting haproxy")
		return fmt.Errorf("missing haproxy config")
	}

	// although haproxy can use multiple pids, we are only ever using a single
	// process.
	pid, err := ioutil.ReadFile(haproxyPid)
	if pe, ok := err.(*os.PathError); err != nil && !ok {
		log.Println("error checking pid file:", pe)
	}

	opts := make([]string, len(haproxyOpts))
	copy(opts, haproxyOpts)

	running := string(bytes.Trim(pid, " \n"))
	if running != "" {
		opts = append(opts, []string{"-sf", running}...)
	}

	cmd := exec.Command(haproxy, opts...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Println("error starting haproxy:", err)
		log.Println(string(output))
		return err
	}

	if len(output) > 0 {
		log.Println(string(output))
	}
	log.Println("haproxy started")
	return nil
}
