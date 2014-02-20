package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/coreos/go-etcd/etcd"
	"github.com/jwilder/go-dockerclient"
	"github.com/litl/galaxy/commander/auth"
	"github.com/ryanuber/columnize"
	"os"
	"os/user"
	"strings"
)

var (
	client         *docker.Client
	ectdClient     *etcd.Client
	etcdHosts      = flag.String("etcd", "http://127.0.0.1:4001", "Comma-separated list of etcd hosts")
	env            = flag.String("env", "dev", "Environment namespace")
	pool           = flag.String("pool", "web", "Pool namespace")
	hostIp         = flag.String("hostIp", "127.0.0.1", "Hosts external IP")
	authConfig     *auth.ConfigFile
	serviceConfigs []*ServiceConfig
	hostname       string
	output         []string
)

type ServiceConfig struct {
	Name    string
	Version string
	Env     map[string]string
}

type ServiceRegistration struct {
	ExternalIp   string `json:"EXTERNAL_IP"`
	ExternalPort string `json:"EXTERNAL_PORT"`
	InternalIp   string `json:"INTERNAL_IP"`
	InternalPort string `json:"INTERNAL_PORT"`
}

func splitDockerImage(img string) (string, string, string) {
	if !strings.Contains(img, "/") {
		return "", img, ""
	}
	parts := strings.Split(img, "/")

	if !strings.Contains(parts[1], ":") {
		return parts[0], parts[1], ""
	}

	imageParts := strings.Split(parts[1], ":")
	// registry, repository, tag
	return parts[0], imageParts[0], imageParts[1]
}

func isRunning(img string) (string, error) {

	image, err := client.InspectImage(img)
	if err != nil {
		return "", err
	}

	containers, err := client.ListContainers(docker.ListContainersOptions{
		All: false,
	})
	if err != nil {
		return "", err
	}

	for _, container := range containers {
		dockerContainer, err := client.InspectContainer(container.ID)
		if err != nil {
			return "", err
		}

		if image.ID == dockerContainer.Image {
			return container.ID, nil
		}
	}
	return "", nil
}

func getImageByName(img string) (*docker.APIImages, error) {
	imgs, err := client.ListImages(true)
	if err != nil {
		panic(err)
	}

	for _, image := range imgs {
		if stringInSlice(img, image.RepoTags) {
			return &image, nil
		}
	}
	return nil, nil

}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func setHostValue(service string, key string, value string) error {
	_, err := ectdClient.Set("/"+*env+"/"+*pool+"/hosts/"+hostname+"/"+
		service+"/"+key, value, 0)
	return err
}

func registerService(container *docker.Container, serviceConfig *ServiceConfig) error {
	_, err := ectdClient.CreateDir("/"+*env+"/"+*pool+"/hosts", 0)
	if err != nil && err.(*etcd.EtcdError).ErrorCode != 105 {
		return err
	}

	registrationPath := "/" + *env + "/" + *pool + "/hosts/" + hostname + "/" + serviceConfig.Name
	registration, err := ectdClient.CreateDir(registrationPath, 60)
	if err != nil && err.(*etcd.EtcdError).ErrorCode != 105 {
		return err
	}

	//FIXME: We're using the first found port and assuming it's tcp.
	//How should we handle a service that exposes multiple ports
	//as well as tcp vs udp ports.
	var externalPort, internalPort string
	for k, _ := range container.NetworkSettings.Ports {
		externalPort = k.Port()
		internalPort = externalPort
		break
	}

	serviceRegistration := ServiceRegistration{
		ExternalIp:   *hostIp,
		ExternalPort: externalPort,
		InternalIp:   container.NetworkSettings.IPAddress,
		InternalPort: internalPort,
	}

	jsonReg, err := json.Marshal(serviceRegistration)
	if err != nil {
		return err
	}

	err = setHostValue(serviceConfig.Name, "location", string(jsonReg))
	if err != nil {
		return err
	}

	jsonReg, err = json.Marshal(serviceConfig.Env)
	if err != nil {
		return err
	}

	err = setHostValue(serviceConfig.Name, "environment", string(jsonReg))
	if err != nil {
		return err
	}

	statusLine := strings.Join([]string{registrationPath,
		container.Config.Image,
		serviceRegistration.ExternalIp + ":" + serviceRegistration.ExternalPort,
		serviceRegistration.InternalIp + ":" + serviceRegistration.InternalPort,
		registration.Node.Expiration.String(),
	}, " | ")

	output = append(output, statusLine)

	return nil
}

func initOrDie() {
	var err error
	endpoint := "unix:///var/run/docker.sock"
	client, err = docker.NewClient(endpoint)

	if err != nil {
		panic(err)
	}

	currentUser, err := user.Current()
	if err != nil {
		panic(err)
	}

	hostname, err = os.Hostname()
	if err != nil {
		panic(err)
	}

	// use ~/.dockercfg
	authConfig, err = auth.LoadConfig(currentUser.HomeDir)
	if err != nil {
		panic(err)
	}
}

func main() {
	flag.Parse()

	if *env == "" {
		fmt.Println("Need an env")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if *pool == "" {
		fmt.Println("Need a pool")
		flag.PrintDefaults()
		os.Exit(1)
	}

	initOrDie()

	if *etcdHosts != "" {
		machines := strings.Split(*etcdHosts, ",")
		ectdClient = etcd.NewClient(machines)
	}

	containers, err := client.ListContainers(docker.ListContainersOptions{
		All: false,
	})
	if err != nil {
		panic(err)
	}

	output = append(output, strings.Join([]string{
		"Registration", "Service", "External Addr", "Internal Addr", "Expires",
	}, " | "))
	for _, container := range containers {
		dockerContainer, err := client.InspectContainer(container.ID)
		if err != nil {
			fmt.Printf("ERROR: Unable to inspect container %s: %s. Skipping.\n", container.ID, err)
			continue
		}

		_, repository, tag := splitDockerImage(dockerContainer.Config.Image)

		env := make(map[string]string)
		for _, entry := range dockerContainer.Config.Env {
			firstSeparator := strings.Index(entry, "=")
			key := entry[0:firstSeparator]
			value := entry[firstSeparator+1:]
			env[key] = value
		}

		serviceConfig := &ServiceConfig{
			Name:    repository,
			Env:     env,
			Version: tag,
		}

		if *etcdHosts != "" {
			err := registerService(dockerContainer, serviceConfig)
			if err != nil {
				fmt.Printf("ERROR: Could not register service %s is running: %s\n",
					serviceConfig.Version, err)
				os.Exit(1)
			}
		}
	}

	result, _ := columnize.SimpleFormat(output)
	fmt.Println(result)

}
