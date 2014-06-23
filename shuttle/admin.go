package main

import (
	"encoding/json"
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

	if Registry.GetService(svcCfg.Name) == nil {
		if e := Registry.AddService(svcCfg); e != nil {
			http.Error(w, e.Error(), http.StatusInternalServerError)
			return
		}
	}

	if e := Registry.UpdateService(svcCfg); e != nil {
		log.Errorln("Unable to update service %s", svcCfg.Name)
		http.Error(w, e.Error(), http.StatusInternalServerError)
		return
	}

	vhosts := make(map[string][]string)
	for _, svcCfg := range Registry.Config() {
		for _, vhost := range svcCfg.VirtualHosts {
			for _, backend := range svcCfg.Backends {
				if backend.Addr == "" {
					log.Warnf("No address specifed for %s for %s. Skipping.", backend.Name, svcCfg.Name)
					continue
				}
				addr := "http://" + backend.Addr
				httpRouter.AddBackend(svcCfg.Name, vhost, addr)
				vhosts[vhost] = append(vhosts[vhost], addr)
			}

		}
	}
	for vhost, addrs := range vhosts {
		httpRouter.RemoveBackends(vhost, addrs)
	}

	go writeStateConfig()
	w.Write(marshal(Registry.Config()))
}

func deleteService(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	service := Registry.GetService(vars["service"])
	if service != nil {
		for _, vhost := range service.VirtualHosts {
			httpRouter.RemoveRouter(vhost)
		}
	}

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

	service := Registry.GetService(vars["service"])
	if service != nil {
		for _, vhost := range service.VirtualHosts {
			httpRouter.AddBackend(backendCfg.Name, vhost, "http://"+backendCfg.Addr)
		}
	}

	go writeStateConfig()
	w.Write(marshal(Registry.Config()))
}

func deleteBackend(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	serviceName := vars["service"]
	backendName := vars["backend"]

	service := Registry.GetService(vars["service"])
	if service != nil {
		for _, vhost := range service.VirtualHosts {
			backend := service.get(backendName)
			httpRouter.RemoveBackend(vhost, "http://"+backend.Addr)
		}
	}

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
