package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/shuttle/client"

	"github.com/gorilla/mux"
)

func getConfig(w http.ResponseWriter, r *http.Request) {
	w.Write(marshal(Registry.Config()))
}

func getStats(w http.ResponseWriter, r *http.Request) {
	if len(Registry.Config()) == 0 {
		w.WriteHeader(503)
	}
	w.Write(marshal(Registry.Stats()))
}

func getService(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	serviceStats, err := Registry.ServiceStats(vars["service"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Write(marshal(serviceStats))
}

// TODO: This is no longer called
//       Verify we didn't miss any logic
/*
func updateVhosts(svcCfg client.ServiceConfig) {
	// This creates map of vhosts we are expecting to be
	// able to route across all the backends in the registry.  We
	// create this because multiple services can support multiple
	// vhost entries.  This creates our expected state.
	vhosts := make(map[string][]string)
	for _, svcCfg := range Registry.Config() {
		for _, vhost := range svcCfg.VirtualHosts {
			if vhost == "" {
				continue
			}
			for _, backend := range svcCfg.Backends {
				if backend.Addr == "" {
					log.Warnf("No address specifed for %s for %s. Skipping.", backend.Name, svcCfg.Name)
					continue
				}
				addr := "http://" + backend.Addr
				vhosts[vhost] = append(vhosts[vhost], addr)
			}
		}
	}

	// Add all the new vhost backends while keeping our expected state
	// in sync.  adding a backend to a router is idempotent so we
	// can add these unconditionally
	for _, vhost := range svcCfg.VirtualHosts {
		if vhost == "" {
			continue
		}

		for _, newBackend := range svcCfg.Backends {
			if newBackend.Addr == "" {
				log.Warnf("No address specifed for %s for %s. Skipping.", newBackend.Name, svcCfg.Name)
				continue
			}
			addr := "http://" + newBackend.Addr
			httpRouter.AddBackend(svcCfg.Name, vhost, addr)
			if !utils.StringInSlice(addr, vhosts[vhost]) {
				vhosts[vhost] = append(vhosts[vhost], addr)
			}
		}
	}

	// We need to remove any backends setup in the router
	// that are not supposed to be there according to our expected
	// state.  Removing all the backends from a router also
	// removes the router.
	for vhost, expectedBackends := range vhosts {
		if vhost == "" {
			continue
		}
		existingBackends := httpRouter.GetBackends(vhost)
		for _, existing := range existingBackends {
			if !utils.StringInSlice(existing, expectedBackends) {
				httpRouter.RemoveBackend(vhost, existing)
			}
		}
	}

	// Last, if a vhost was completely removed then it would
	// not be in our expected state so iterate over our actual
	// state and remove any routers that should not be there
	for _, vhost := range Registry.GetVhosts() {
		if _, ok := vhosts[vhost]; !ok {
			httpRouter.RemoveRouter(vhost)
		}
	}
}
*/

// Update a service and/or backends.
// Adding a `backends_only` query parameter will prevent the service from being
// shutdown and replaced if the ServiceConfig is not identical..
func postService(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Errorln(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	svcCfg := client.ServiceConfig{Name: vars["service"]}
	err = json.Unmarshal(body, &svcCfg)
	if err != nil {
		log.Errorln(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	invalidPorts := []string{
		listenAddr[strings.Index(listenAddr, ":")+1:],
		adminListenAddr[strings.Index(adminListenAddr, ":")+1:],
	}

	for _, port := range invalidPorts {
		if strings.HasSuffix(svcCfg.Addr, port) {
			log.Errorf("Cannot use shuttle port: %s for %s service listener. Shuttle is using it.", port, svcCfg.Name)
			http.Error(w, fmt.Sprintf("cannot use %s for listener port", port), http.StatusBadRequest)
			return
		}
	}

	if Registry.GetService(svcCfg.Name) == nil {
		if e := Registry.AddService(svcCfg); e != nil {
			log.Errorln(err)
			http.Error(w, e.Error(), http.StatusInternalServerError)
			return
		}
	}

	if e := Registry.UpdateService(svcCfg); e != nil {
		log.Errorln("Unable to update service %s", svcCfg.Name)
		http.Error(w, e.Error(), http.StatusInternalServerError)
		return
	}

	go writeStateConfig()
	w.Write(marshal(Registry.Config()))
}

func deleteService(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	err := Registry.RemoveService(vars["service"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	go writeStateConfig()
	w.Write(marshal(Registry.Config()))
}

func getBackend(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	serviceName := vars["service"]
	backendName := vars["backend"]

	backend, err := Registry.BackendStats(serviceName, backendName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Write(marshal(backend))
}

func postBackend(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Errorln(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	backendName := vars["backend"]
	serviceName := vars["service"]

	backendCfg := client.BackendConfig{Name: backendName}
	err = json.Unmarshal(body, &backendCfg)
	if err != nil {
		log.Errorln(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := Registry.AddBackend(serviceName, backendCfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	go writeStateConfig()
	w.Write(marshal(Registry.Config()))
}

func deleteBackend(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	serviceName := vars["service"]
	backendName := vars["backend"]

	if err := Registry.RemoveBackend(serviceName, backendName); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	go writeStateConfig()
	w.Write(marshal(Registry.Config()))
}

func addHandlers() {
	r := mux.NewRouter()
	r.HandleFunc("/", getStats).Methods("GET")
	r.HandleFunc("/_config", getConfig).Methods("GET")
	r.HandleFunc("/{service}", getService).Methods("GET")
	r.HandleFunc("/{service}", postService).Methods("PUT", "POST")
	r.HandleFunc("/{service}", deleteService).Methods("DELETE")
	r.HandleFunc("/{service}/{backend}", getBackend).Methods("GET")
	r.HandleFunc("/{service}/{backend}", postBackend).Methods("PUT", "POST")
	r.HandleFunc("/{service}/{backend}", deleteBackend).Methods("DELETE")
	http.Handle("/", r)
}

func startAdminHTTPServer() {
	defer wg.Done()
	addHandlers()
	log.Println("Admin server listening on", adminListenAddr)

	netw := "tcp"

	if strings.HasPrefix(adminListenAddr, "/") {
		netw = "unix"

		// remove our old socket if we left it lying around
		if stats, err := os.Stat(adminListenAddr); err == nil {
			if stats.Mode()&os.ModeSocket != 0 {
				os.Remove(adminListenAddr)
			}
		}

		defer os.Remove(adminListenAddr)
	}

	listener, err := net.Listen(netw, adminListenAddr)
	if err != nil {
		log.Fatalln(err)
	}

	http.Serve(listener, nil)
}
