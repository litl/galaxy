package main

import (
	"encoding/json"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/shuttle/client"

	"github.com/gorilla/mux"
)

func getConfig(w http.ResponseWriter, r *http.Request) {
	w.Write(marshal(Registry.Config()))
}

func getStats(w http.ResponseWriter, r *http.Request) {
	if len(Registry.Config().Services) == 0 {
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

// Update the global config
func postConfig(w http.ResponseWriter, r *http.Request) {
	cfg := client.Config{}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Errorln(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	err = json.Unmarshal(body, &cfg)
	if err != nil {
		log.Errorln(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := Registry.UpdateConfig(cfg); err != nil {
		log.Errorln(err)
		// TODO: differentiate between ServerError and BadRequest
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
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

	// don't let someone update the wrong service
	if svcCfg.Name != vars["service"] {
		errMsg := "Mismatched service name in API call"
		log.Error(errMsg)
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	cfg := client.Config{
		Services: []client.ServiceConfig{svcCfg},
	}

	err = Registry.UpdateConfig(cfg)
	//FIXME: this doesn't return an error for an empty or broken service
	if err != nil {
		log.Error(err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

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
	r.HandleFunc("/", postConfig).Methods("PUT", "POST")
	r.HandleFunc("/_config", getConfig).Methods("GET")
	r.HandleFunc("/_config", postConfig).Methods("PUT", "POST")
	r.HandleFunc("/_stats", getStats).Methods("GET")
	r.HandleFunc("/{service}", getService).Methods("GET")
	r.HandleFunc("/{service}", postService).Methods("PUT", "POST")
	r.HandleFunc("/{service}", deleteService).Methods("DELETE")
	r.HandleFunc("/{service}/{backend}", getBackend).Methods("GET")
	r.HandleFunc("/{service}/{backend}", postBackend).Methods("PUT", "POST")
	r.HandleFunc("/{service}/{backend}", deleteBackend).Methods("DELETE")
	http.Handle("/", r)
}

func startAdminHTTPServer(wg *sync.WaitGroup) {
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
