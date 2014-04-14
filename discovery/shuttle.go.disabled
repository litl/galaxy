package main

import (
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/coreos/go-etcd/etcd"
	"github.com/litl/galaxy/registry"
)

var (
	env      string
	etcdHost string
	etcdPort string
	publicIP string
)

func init() {
	flag.StringVar(&env, "env", "dev", "environment {dev|stage|prod}")
	flag.StringVar(&etcdHost, "etcd", "127.0.0.1", "etcd hostname")
	flag.StringVar(&etcdPort, "etcd-port", "4001", "etcd port")
	flag.StringVar(&publicIP, "public-ip", "127.0.0.1", "advertised public IP for this host")
	flag.StringVar(&backendCfg, "backends", "backend.cfg", "haproxy backend config file")
	flag.StringVar(&globalCfg, "global", "haproxy.cfg", "haproxy global config file")
	flag.StringVar(&haproxyPid, "pid", "haproxy.pid", "haproxy pid file")
	flag.StringVar(&haproxy, "haproxy", "haproxy", "haproxy binary")
	flag.Parse()

	haproxyOpts = []string{"-f", globalCfg, "-f", backendCfg, "-p", haproxyPid}
}

// struct for unmarshaling Service (for PORT) and Server locations
type ServiceCfg struct {
	Port string `json:"PORT"`
	registry.ServiceRegistration
}

// detect Services and Servers by their configuration key names
// return "server" or "service"
func valueType(parts []string) string {
	if len(parts) == 4 && parts[3] == "environment" {
		return "service"
	}
	if len(parts) == 6 && parts[5] == "location" {
		return "server"
	}
	return ""
}

// Walk the entire tree once to get the current config.
// Further updates will come in through a Watch.
func walkTree(n etcd.Node, config *HaproxyCfg) {
	// strip the leading "/" while we split
	parts := strings.Split(strings.TrimLeft(n.Key, "/"), "/")

	switch valueType(parts) {
	case "service":
		config.UpdateListener(n.Key, n.Value)
	case "server":
		config.UpdateServer(n.Key, n.Value)
	}

	for _, next := range n.Nodes {
		walkTree(next, config)
	}
}

func updateConfig(config *HaproxyCfg, resp *etcd.Response) (changed bool) {
	if resp == nil || resp.Node == nil {
		return
	}

	parts := strings.Split(strings.TrimLeft(resp.Node.Key, "/"), "/")
	switch valueType(parts) {
	case "service":
		changed = config.UpdateListener(resp.Node.Key, resp.Node.Value)
	case "server":
		changed = config.UpdateServer(resp.Node.Key, resp.Node.Value)
	}
	return changed
}

func buildConfig(n etcd.Node) *HaproxyCfg {
	config := NewHaproxyCfg()
	walkTree(n, config)
	return config
}

func watchConfig() {
	respCh := make(chan *etcd.Response)
	updateCh := make(chan []byte)
	go haproxyUpdater(updateCh)

	client := etcd.NewClient([]string{fmt.Sprintf("http://%s:%s", etcdHost, etcdPort)})
	client.SyncCluster()

	// drop back here on error, building a new config in case there was a
	// number of changes piled up.
REDO:
	var resp *etcd.Response
	var err error
	for {
		// loop here until we can get a response from the cluster
		resp, err = client.Get(env, false, true)
		if err != nil {
			log.Println(err)
			time.Sleep(10 * time.Second)
		} else {
			break
		}
	}

	lastIndex := resp.EtcdIndex

	config := buildConfig(*resp.Node)
	updateCh <- config.Bytes()

	stopCh := make(chan bool)
	errCh := make(chan error, 1)
	go func() {
		_, err := client.Watch(env, lastIndex+1, true, respCh, stopCh)
		errCh <- err
	}()

	// restart the watch every so often in case our socket is broken
	// pollTicker := time.NewTicker(time.Hour)

	for {
		select {
		case resp := <-respCh:
			lastIndex = resp.EtcdIndex
			if updateConfig(config, resp) {
				log.Println("config changed")
				updateCh <- config.Bytes()
			}
		case err := <-errCh:
			log.Println(err)
			goto REDO
		}
	}
}

func main() {
	if !haproxyRunning() {
		restartHaproxy()
	}

	watchConfig()
}
