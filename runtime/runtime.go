package runtime

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"time"

	auth "github.com/dotcloud/docker/registry"
	docker "github.com/fsouza/go-dockerclient"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/utils"
)

var blacklistedContainerId = make(map[string]bool)

type ServiceRuntime struct {
	dockerClient    *docker.Client
	authConfig      *auth.ConfigFile
	shuttleHost     string
	statsdHost      string
	serviceRegistry *registry.ServiceRegistry
}

func NewServiceRuntime(shuttleHost, statsdHost, env, pool, redisHost string) *ServiceRuntime {
	dockerZero, err := dockerBridgeIp()
	if err != nil {
		log.Fatalf("ERROR: Unable to find docker0 bridge: %s", err)
	}
	if shuttleHost == "" {
		shuttleHost = dockerZero
	}

	if statsdHost == "" {
		statsdHost = dockerZero + ":8125"
	}

	statsdHost = utils.GetEnv("GALAXY_STATSD_HOST", statsdHost)

	serviceRegistry := registry.NewServiceRegistry(
		env,
		pool,
		"",
		600,
		"",
	)
	serviceRegistry.Connect(redisHost)

	return &ServiceRuntime{
		shuttleHost:     shuttleHost,
		statsdHost:      statsdHost,
		serviceRegistry: serviceRegistry,
	}

}

func GetEndpoint() string {
	defaultEndpoint := "unix:///var/run/docker.sock"
	if os.Getenv("DOCKER_HOST") != "" {
		defaultEndpoint = os.Getenv("DOCKER_HOST")
	}

	return defaultEndpoint

}

// based off of https://github.com/dotcloud/docker/blob/2a711d16e05b69328f2636f88f8eac035477f7e4/utils/utils.go
func parseHost(addr string) (string, string, error) {
	var (
		proto string
		host  string
		port  int
	)
	addr = strings.TrimSpace(addr)
	switch {
	case addr == "tcp://":
		return "", "", fmt.Errorf("Invalid bind address format: %s", addr)
	case strings.HasPrefix(addr, "unix://"):
		proto = "unix"
		addr = strings.TrimPrefix(addr, "unix://")
		if addr == "" {
			addr = "/var/run/docker.sock"
		}
	case strings.HasPrefix(addr, "tcp://"):
		proto = "tcp"
		addr = strings.TrimPrefix(addr, "tcp://")
	case strings.HasPrefix(addr, "fd://"):
		return "fd", addr, nil
	case addr == "":
		proto = "unix"
		addr = "/var/run/docker.sock"
	default:
		if strings.Contains(addr, "://") {
			return "", "", fmt.Errorf("Invalid bind address protocol: %s", addr)
		}
		proto = "tcp"
	}

	if proto != "unix" && strings.Contains(addr, ":") {
		hostParts := strings.Split(addr, ":")
		if len(hostParts) != 2 {
			return "", "", fmt.Errorf("Invalid bind address format: %s", addr)
		}
		if hostParts[0] != "" {
			host = hostParts[0]
		} else {
			host = "127.0.0.1"
		}

		if p, err := strconv.Atoi(hostParts[1]); err == nil && p != 0 {
			port = p
		} else {
			return "", "", fmt.Errorf("Invalid bind address format: %s", addr)
		}

	} else if proto == "tcp" && !strings.Contains(addr, ":") {
		return "", "", fmt.Errorf("Invalid bind address format: %s", addr)
	} else {
		host = addr
	}
	if proto == "unix" {
		return proto, host, nil

	}
	return proto, fmt.Sprintf("%s:%d", host, port), nil
}

func dockerBridgeIp() (string, error) {
	dh := os.Getenv("DOCKER_HOST")
	if dh != "" && strings.HasPrefix(dh, "tcp") {
		_, hostPort, err := parseHost(dh)
		return strings.Split(hostPort, ":")[0], err
	}

	dockerZero, err := net.InterfaceByName("docker0")
	if err != nil {
		return "", err
	}
	addrs, _ := dockerZero.Addrs()
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil {
			return "", err
		}
		if ip.DefaultMask() != nil {
			return ip.String(), nil
		}
	}
	return "", errors.New("unable to find docker0 interface")
}

func (s *ServiceRuntime) ensureDockerClient() *docker.Client {
	if s.dockerClient == nil {
		endpoint := GetEndpoint()
		client, err := docker.NewClient(endpoint)
		if err != nil {
			log.Fatalf("ERROR: Unable to connect to docker: %s: %s", err, endpoint)
		}
		s.dockerClient = client

	}
	return s.dockerClient
}

func (s *ServiceRuntime) InspectImage(image string) (*docker.Image, error) {
	return s.ensureDockerClient().InspectImage(image)
}

func (s *ServiceRuntime) Stop(serviceConfig *registry.ServiceConfig) error {
	latestName := serviceConfig.ContainerName()
	latestContainer, err := s.ensureDockerClient().InspectContainer(latestName)
	_, ok := err.(*docker.NoSuchContainer)
	// Expected container is not actually running. Skip it and leave old ones.
	if err != nil && ok {
		return nil
	}

	return s.stopContainer(latestContainer)
}

func (s *ServiceRuntime) stopContainer(container *docker.Container) error {
	if _, ok := blacklistedContainerId[container.ID]; ok {
		log.Printf("Container %s blacklisted. Won't try to stop.\n", container.ID)
		return nil
	}

	log.Printf("Stopping %s container %s\n", strings.TrimPrefix(container.Name, "/"), container.ID[0:12])
	c := make(chan error, 1)
	go func() { c <- s.ensureDockerClient().StopContainer(container.ID, 10) }()
	select {
	case err := <-c:
		if err != nil {
			log.Printf("ERROR: Unable to stop container: %s\n", container.ID)
			return err
		}
	case <-time.After(20 * time.Second):
		blacklistedContainerId[container.ID] = true
		log.Printf("ERROR: Timed out trying to stop container. Zombie?. Blacklisting: %s\n", container.ID)
		return nil
	}
	log.Printf("Stopped %s container %s\n", strings.TrimPrefix(container.Name, "/"), container.ID[0:12])

	return s.ensureDockerClient().RemoveContainer(docker.RemoveContainerOptions{
		ID:            container.ID,
		RemoveVolumes: true,
	})
}

func (s *ServiceRuntime) StopAllButLatestService(serviceConfig *registry.ServiceConfig, stopCutoff int64) error {
	latestName := serviceConfig.ContainerName()

	containers, err := s.ensureDockerClient().ListContainers(docker.ListContainersOptions{
		All: false,
	})
	if err != nil {
		return err
	}

	latestContainer, err := s.ensureDockerClient().InspectContainer(latestName)
	_, ok := err.(*docker.NoSuchContainer)
	// Expected container is not actually running. Skip it and leave old ones.
	if err != nil && ok {
		return nil
	}

	for _, container := range containers {

		// We name all galaxy managed containers
		if len(container.Names) == 0 {
			continue
		}

		// Container name does match one that would be started w/ this service config
		if !serviceConfig.IsContainerVersion(strings.TrimPrefix(container.Names[0], "/")) {
			continue
		}

		if container.ID != latestContainer.ID &&
			container.Created < (time.Now().Unix()-stopCutoff) {
			dockerContainer, err := s.ensureDockerClient().InspectContainer(container.ID)
			if err != nil {
				log.Printf("ERROR: Unable to stop container: %s\n", container.ID)
				continue
			}
			s.stopContainer(dockerContainer)
		}
	}
	return nil
}

func (s *ServiceRuntime) StopAllButLatest(stopCutoff int64) error {

	serviceConfigs, err := s.serviceRegistry.ListApps("")
	if err != nil {
		return err
	}

	for _, serviceConfig := range serviceConfigs {
		s.StopAllButLatestService(&serviceConfig, stopCutoff)
	}

	return nil

}

func (s *ServiceRuntime) GetImageByName(img string) (*docker.APIImages, error) {
	imgs, err := s.ensureDockerClient().ListImages(true)
	if err != nil {
		panic(err)
	}

	for _, image := range imgs {
		if utils.StringInSlice(img, image.RepoTags) {
			return &image, nil
		}
	}
	return nil, nil

}

func (s *ServiceRuntime) RunCommand(serviceConfig *registry.ServiceConfig, cmd []string) (*docker.Container, error) {

	// see if we have the image locally
	fmt.Fprintf(os.Stderr, "Pulling latest image for %s\n", serviceConfig.Version())
	_, err := s.PullImage(serviceConfig.Version(), true)
	if err != nil {
		return nil, err
	}

	envVars := []string{}
	for key, value := range serviceConfig.Env() {
		envVars = append(envVars, strings.ToUpper(key)+"="+value)
	}

	runCmd := []string{"/bin/bash", "-c", strings.Join(cmd, " ")}

	container, err := s.ensureDockerClient().CreateContainer(docker.CreateContainerOptions{
		Config: &docker.Config{
			Image:        serviceConfig.Version(),
			Env:          envVars,
			AttachStdout: true,
			AttachStderr: true,
			Cmd:          runCmd,
			OpenStdin:    false,
		},
	})

	if err != nil {
		return nil, err
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)
	go func(s *ServiceRuntime, containerId string) {
		<-c
		log.Println("Stopping container...")
		err := s.ensureDockerClient().StopContainer(containerId, 3)
		if err != nil {
			log.Printf("ERROR: Unable to stop container: %s", err)
		}
		err = s.ensureDockerClient().RemoveContainer(docker.RemoveContainerOptions{
			ID: containerId,
		})
		if err != nil {
			log.Printf("ERROR: Unable to stop container: %s", err)
		}

	}(s, container.ID)

	defer s.ensureDockerClient().RemoveContainer(docker.RemoveContainerOptions{
		ID: container.ID,
	})
	err = s.ensureDockerClient().StartContainer(container.ID,
		&docker.HostConfig{
			Dns: []string{s.shuttleHost},
		})

	if err != nil {
		return container, err
	}

	// FIXME: Hack to work around the race of attaching to a container before it's
	// actually running.  Tried polling the container and then attaching but the
	// output gets lost sometimes if the command executes very quickly. Not sure
	// what's going on.
	time.Sleep(1 * time.Second)

	err = s.ensureDockerClient().AttachToContainer(docker.AttachToContainerOptions{
		Container:    container.ID,
		OutputStream: os.Stdout,
		ErrorStream:  os.Stderr,
		Logs:         true,
		Stream:       false,
		Stdout:       true,
		Stderr:       true,
	})

	if err != nil {
		log.Printf("ERROR: Unable to attach to running container: %s", err.Error())
	}

	s.ensureDockerClient().WaitContainer(container.ID)

	return container, err
}

func (s *ServiceRuntime) StartInteractive(serviceConfig *registry.ServiceConfig) error {

	// see if we have the image locally
	fmt.Fprintf(os.Stderr, "Pulling latest image for %s\n", serviceConfig.Version())
	_, err := s.PullImage(serviceConfig.Version(), true)
	if err != nil {
		return err
	}

	args := []string{
		"run", "--rm", "-i",
	}
	for key, value := range serviceConfig.Env() {
		if key == "ENV" {
			args = append(args, "-e")
			args = append(args, strings.ToUpper(key)+"="+s.serviceRegistry.Env)
			continue
		}

		args = append(args, "-e")
		args = append(args, strings.ToUpper(key)+"="+value)
	}

	args = append(args, "-e")
	args = append(args, fmt.Sprintf("HOST_IP=%s", s.shuttleHost))
	args = append(args, "-e")
	args = append(args, fmt.Sprintf("STATSD_ADDR=%s", s.statsdHost))
	args = append(args, "--dns")
	args = append(args, s.shuttleHost)

	serviceConfigs, err := s.serviceRegistry.ListApps("")
	if err != nil {
		return err
	}

	for _, config := range serviceConfigs {
		port := config.Env()["GALAXY_PORT"]
		if port == "" {
			continue
		}

		args = append(args, "-e")
		args = append(args, strings.ToUpper(config.Name)+"_ADDR="+s.shuttleHost+":"+port)
	}

	publicDns, err := ec2PublicHostname()
	if err != nil {
		log.Warnf("Unable to determine public hostname. Not on AWS? %s", err)
		publicDns = "127.0.0.1"
	}

	args = append(args, "-e")
	args = append(args, fmt.Sprintf("PUBLIC_HOSTNAME=%s", publicDns))

	args = append(args, []string{"-t", serviceConfig.Version(), "/bin/bash"}...)
	// shell out to docker run to get signal forwarded and terminal setup correctly
	//cmd := exec.Command("docker", "run", "-rm", "-i", "-t", serviceConfig.Version(), "/bin/bash")
	cmd := exec.Command("docker", args...)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Start()
	if err != nil {
		log.Fatal(err)
	}

	err = cmd.Wait()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Command finished with error: %v\n", err)
	}

	return err
}

func (s *ServiceRuntime) Start(serviceConfig *registry.ServiceConfig) (*docker.Container, error) {
	img := serviceConfig.Version()
	// see if we have the image locally
	image, err := s.PullImage(img, false)
	if err != nil {
		return nil, err
	}

	// setup env vars from etcd
	var envVars []string
	for key, value := range serviceConfig.Env() {
		if key == "ENV" {
			envVars = append(envVars, strings.ToUpper(key)+"="+s.serviceRegistry.Env)
			continue
		}
		envVars = append(envVars, strings.ToUpper(key)+"="+value)
	}

	serviceConfigs, err := s.serviceRegistry.ListApps("")
	if err != nil {
		return nil, err
	}

	for _, config := range serviceConfigs {
		port := config.Env()["GALAXY_PORT"]

		if port == "" {
			continue
		}

		envVars = append(envVars, strings.ToUpper(config.Name)+"_ADDR="+s.shuttleHost+":"+port)

	}

	envVars = append(envVars, fmt.Sprintf("HOST_IP=%s", s.shuttleHost))
	envVars = append(envVars, fmt.Sprintf("STATSD_ADDR=%s", s.statsdHost))
	publicDns, err := ec2PublicHostname()
	if err != nil {
		log.Warnf("Unable to determine public hostname. Not on AWS? %s", err)
		publicDns = "127.0.0.1"
	}
	envVars = append(envVars, fmt.Sprintf("PUBLIC_HOSTNAME=%s", publicDns))

	containerName := serviceConfig.ContainerName()
	container, err := s.ensureDockerClient().InspectContainer(containerName)
	_, ok := err.(*docker.NoSuchContainer)
	if err != nil && !ok {
		return nil, err
	}

	// Existing container is running or stopped.  If the image has changed, stop
	// and re-create it.
	if container != nil && container.Image != image.ID {
		if container.State.Running {
			log.Printf("Stopping %s version %s running as %s", serviceConfig.Name, serviceConfig.Version(), container.ID[0:12])
			err := s.ensureDockerClient().StopContainer(container.ID, 10)
			if err != nil {
				return nil, err
			}
		}

		log.Printf("Removing %s version %s running as %s", serviceConfig.Name, serviceConfig.Version(), container.ID[0:12])
		err = s.ensureDockerClient().RemoveContainer(docker.RemoveContainerOptions{
			ID: container.ID,
		})
		if err != nil {
			return nil, err
		}
		container = nil
	}

	if container == nil {
		log.Printf("Creating %s version %s", serviceConfig.Name, serviceConfig.Version())
		container, err = s.ensureDockerClient().CreateContainer(docker.CreateContainerOptions{
			Name: containerName,
			Config: &docker.Config{
				Image: img,
				Env:   envVars,
			},
		})
		if err != nil {
			return nil, err
		}
	}

	log.Printf("Starting %s version %s running as %s", serviceConfig.Name, serviceConfig.Version(), container.ID[0:12])
	err = s.ensureDockerClient().StartContainer(container.ID,
		&docker.HostConfig{
			Dns:             []string{s.shuttleHost},
			PublishAllPorts: true,
		})

	if err != nil {
		return container, err
	}

	startedContainer, err := s.ensureDockerClient().InspectContainer(container.ID)
	for i := 0; i < 5; i++ {

		startedContainer, err = s.ensureDockerClient().InspectContainer(container.ID)
		if !startedContainer.State.Running {
			return nil, errors.New("Container stopped unexpectedly")
		}
		time.Sleep(1 * time.Second)
	}
	return startedContainer, err

}

func (s *ServiceRuntime) StartIfNotRunning(serviceConfig *registry.ServiceConfig) (bool, *docker.Container, error) {
	container, err := s.ensureDockerClient().InspectContainer(serviceConfig.ContainerName())
	_, ok := err.(*docker.NoSuchContainer)
	// Expected container is not actually running. Skip it and leave old ones.
	if (err != nil && ok) || container == nil {
		container, err := s.Start(serviceConfig)
		return true, container, err
	}

	if err != nil {
		return false, nil, err
	}

	containerName := strings.TrimPrefix(container.Name, "/")

	// check if container is the right version
	if !serviceConfig.IsContainerVersion(containerName) {
		return false, container, nil
	}

	image, err := s.ensureDockerClient().InspectImage(serviceConfig.Version())
	if err != nil {
		return false, nil, err
	}

	imageDiffers := image.ID != container.Image
	configDiffers := containerName != serviceConfig.ContainerName()
	notRunning := !container.State.Running

	if imageDiffers || configDiffers || notRunning {
		container, err := s.Start(serviceConfig)
		return true, container, err
	}

	return false, container, nil

}

func (s *ServiceRuntime) PullImage(version string, force bool) (*docker.Image, error) {
	image, err := s.ensureDockerClient().InspectImage(version)

	if err != nil && err != docker.ErrNoSuchImage {
		return nil, err
	}

	if image != nil && !force {
		return image, nil
	}

	registry, repository, tag := utils.SplitDockerImage(version)

	// No, pull it down locally
	pullOpts := docker.PullImageOptions{
		Repository:   repository,
		Tag:          tag,
		OutputStream: log.DefaultLogger}

	dockerAuth := docker.AuthConfiguration{}
	if registry != "" && s.authConfig == nil {

		pullOpts.Repository = registry + "/" + repository
		pullOpts.Registry = registry
		pullOpts.Tag = tag

		homeDir := utils.HomeDir()
		if homeDir == "" {
			return nil, errors.New("ERROR: Unable to determine current home dir. Set $HOME")
		}

		// use ~/.dockercfg
		authConfig, err := auth.LoadConfig(homeDir)
		if err != nil {
			panic(err)
		}

		pullOpts.Registry = registry
		authCreds := authConfig.ResolveAuthConfig(registry)

		dockerAuth.Username = authCreds.Username
		dockerAuth.Password = authCreds.Password
		dockerAuth.Email = authCreds.Email
	}

	retries := 0
	for {
		retries += 1
		err = s.ensureDockerClient().PullImage(pullOpts, dockerAuth)
		if err != nil {

			// Don't retry 404, they'll never succeed
			if err.Error() == "HTTP code: 404" {
				return image, nil
			}

			if retries > 3 {
				return image, err
			}
			log.Errorf("ERROR: error pulling image %s. Attempt %d: %s", version, retries, err)
			continue
		}
		break
	}

	return s.ensureDockerClient().InspectImage(version)

}
