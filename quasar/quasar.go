package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/utils"
	log "github.com/mailgun/gotools-log"
	"github.com/mailgun/vulcan"
	"github.com/mailgun/vulcan/endpoint"
	"github.com/mailgun/vulcan/loadbalance/roundrobin"
	"github.com/mailgun/vulcan/location/httploc"
	"github.com/mailgun/vulcan/request"
	"github.com/mailgun/vulcan/route"
	"github.com/mailgun/vulcan/route/hostroute"
)

var (
	serviceRegistry *registry.ServiceRegistry
	router          *hostroute.HostRouter
	env             string
	version         bool
	buildVersion    string
	listenAddr      string
	redisHost       string
)

type RequestLogger struct{}

func (r *RequestLogger) ObserveRequest(req request.Request) {
	log.Infof("%s %s", req.GetHttpRequest().Host, req)
}

func (r *RequestLogger) ObserveResponse(req request.Request, a request.Attempt) {}

var balancers map[string]*roundrobin.RoundRobin

func addBackends(registrations []registry.ServiceRegistration) map[string][]string {

	liveVhosts := make(map[string][]string)
	for _, r := range registrations {

		serviceConfig, err := serviceRegistry.GetServiceConfig(r.Name)
		if err != nil {
			log.Errorf("Error: %s", err)
			continue
		}
		vhosts := serviceConfig.Env()["VIRTUAL_HOST"]

		for _, vhost := range strings.Split(vhosts, ",") {
			addr := fmt.Sprint(r.ExternalIP, ":", r.ExternalPort)
			url := "http://" + addr
			liveVhosts[vhost] = append(liveVhosts[vhost], url)

			balancer := balancers[vhost]
			if balancer == nil {
				// Create a round robin load balancer with some endpoints
				balancer, err = roundrobin.NewRoundRobin()
				if err != nil {
					log.Errorf("Error: %s", err)
					continue
				}

				// Create a http location with the load balancer we've just added
				loc, err := httploc.NewLocation(r.Name, balancer)
				if err != nil {
					log.Errorf("Error: %s", err)
					continue
				}
				loc.GetObserverChain().Add("logger", &RequestLogger{})

				router.SetRouter(vhost, &route.ConstRouter{Location: loc})

				balancers[vhost] = balancer
			}

			// Already registered?
			if balancer.FindEndpointByUrl(url) != nil {
				continue
			}
			endpoint := endpoint.MustParseUrl(url)
			log.Infof("Adding %s endpoint %s", r.Name, endpoint.GetUrl())
			err := balancer.AddEndpoint(endpoint)
			if err != nil {
				log.Warningf("%s", err)
			}
		}
	}
	return liveVhosts
}

func removeBackends(liveVhosts map[string][]string) {
	// Remove backends that are no longer registered
	for k, _ := range balancers {

		if k == "" {
			continue
		}
		balancer := balancers[k]
		endpoints := balancer.GetEndpoints()
		for _, endpoint := range endpoints {
			vhosts := liveVhosts[k]
			exists := false
			for _, url := range vhosts {
				if endpoint.GetUrl().String() == url {
					exists = true
					break
				}
			}
			if !exists {
				log.Infof("Removing endpoint %s", endpoint.GetUrl())
				balancer.RemoveEndpoint(endpoint)
			}
		}

		endpoints = balancer.GetEndpoints()
		if len(endpoints) == 0 {
			delete(balancers, k)
			continue
		}

	}
}

func updateRoutes() {
	for {
		registrations, err := serviceRegistry.ListRegistrations()
		if err != nil {
			log.Errorf("%s", err)
			return
		}

		liveVhosts := addBackends(registrations)
		removeBackends(liveVhosts)
		time.Sleep(10 * time.Second)
	}
}

func adminHandler(w http.ResponseWriter, r *http.Request) {
	if len(balancers) == 0 {
		w.WriteHeader(503)
		return
	}
	for k, _ := range balancers {
		balancer := balancers[k]
		endpoints := balancer.GetEndpoints()
		fmt.Fprintf(w, "%s\n", k)
		for _, endpoint := range endpoints {
			fmt.Fprintf(w, "  %s\t%d\t%d\t%0.2f\n", endpoint.GetUrl(), endpoint.GetOriginalWeight(), endpoint.GetEffectiveWeight(), endpoint.GetMeter().GetRate())
		}
	}
}

func statusHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			log.Warningf("%s", err)
			h.ServeHTTP(w, r)
			return
		}

		if _, exists := balancers[host]; !exists {
			adminHandler(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func main() {

	flag.StringVar(&listenAddr, "http", "127.0.0.1:8080", "http server address")
	flag.StringVar(&env, "env", utils.GetEnv("GALAXY_ENV", ""), "Environment namespace")
	flag.StringVar(&redisHost, "redis", utils.GetEnv("GALAXY_REDIS_HOST", "127.0.0.1:6379"), "redis host")
	flag.BoolVar(&version, "v", false, "display version info")

	flag.Parse()

	if version {
		fmt.Println(buildVersion)
		return
	}

	if env == "" {
		fmt.Println("Need an env")
		flag.PrintDefaults()
		os.Exit(1)
	}

	balancers = make(map[string]*roundrobin.RoundRobin)
	serviceRegistry = registry.NewServiceRegistry(
		env,
		"",
		"",
		30,
		"",
	)

	serviceRegistry.Connect(redisHost)

	outputBuffer := &utils.OutputBuffer{}
	serviceRegistry.OutputBuffer = outputBuffer

	// init the logging package
	log.Init([]*log.LogConfig{
		&log.LogConfig{Name: "console"},
	})

	log.Infof("Starting quasar for env %s", env)
	log.Infof("Listening at %s", listenAddr)

	router = hostroute.NewHostRouter()
	go updateRoutes()

	proxy, err := vulcan.NewProxy(router)
	if err != nil {
		log.Fatalf("Error: %s", err)
	}

	// Proxy acts as http handler:
	server := &http.Server{
		Addr:           listenAddr,
		Handler:        statusHandler(proxy),
		ReadTimeout:    60 * time.Second,
		WriteTimeout:   60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	log.Errorf("%s", server.ListenAndServe())
}
