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
	"github.com/litl/galaxy/config"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/utils"
)

var blacklistedContainerId = make(map[string]bool)

type ServiceRuntime struct {
	dockerClient    *docker.Client
	authConfig      *auth.ConfigFile
	dns             string
	serviceRegistry *registry.ServiceRegistry
	dockerIP        string
	hostIP          string
}

type ContainerEvent struct {
	Status              string
	Container           *docker.Container
	ServiceRegistration *registry.ServiceRegistration
}

func NewServiceRuntime(serviceRegistry *registry.ServiceRegistry, dns, hostIP string) *ServiceRuntime {
	dockerZero, err := dockerBridgeIp()
	if err != nil {
		log.Fatalf("ERROR: Unable to find docker0 bridge: %s", err)
	}

	return &ServiceRuntime{
		dns:             dns,
		serviceRegistry: serviceRegistry,
		hostIP:          hostIP,
		dockerIP:        dockerZero,
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
		client.HTTPClient.Timeout = 60 * time.Second
		s.dockerClient = client

	}
	return s.dockerClient
}

func (s *ServiceRuntime) Ping() error {
	return s.ensureDockerClient().Ping()
}

func (s *ServiceRuntime) InspectImage(image string) (*docker.Image, error) {
	return s.ensureDockerClient().InspectImage(image)
}

func (s *ServiceRuntime) InspectContainer(id string) (*docker.Container, error) {
	return s.ensureDockerClient().InspectContainer(id)
}

func (s *ServiceRuntime) StopAllMatching(name string) error {
	containers, err := s.ManagedContainers()
	if err != nil {
		return err
	}

	for _, container := range containers {

		env := s.EnvFor(container)
		// Container name does match one that would be started w/ this service config
		if env["GALAXY_APP"] != name {
			continue
		}

		s.stopContainer(container)
	}
	return nil

}

func (s *ServiceRuntime) Stop(appCfg *config.AppConfig) error {
	containers, err := s.ManagedContainers()
	if err != nil {
		return err
	}

	for _, container := range containers {
		cenv := s.EnvFor(container)
		if cenv["GALAXY_APP"] == appCfg.Name &&
			cenv["GALAXY_VERSION"] == strconv.FormatInt(appCfg.ID(), 10) &&
			appCfg.VersionID() == container.Image {
			return s.stopContainer(container)
		}
	}
	return nil
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

	return nil
	/*	return s.ensureDockerClient().RemoveContainer(docker.RemoveContainerOptions{
		ID:            container.ID,
		RemoveVolumes: true,
	})*/
}

func (s *ServiceRuntime) StopOldVersion(appCfg *config.AppConfig, limit int) error {
	containers, err := s.ManagedContainers()
	if err != nil {
		return err
	}

	stopped := 0

	for _, container := range containers {

		if stopped == limit {
			return nil
		}

		env := s.EnvFor(container)
		// Container name does match one that would be started w/ this service config
		if env["GALAXY_APP"] != appCfg.Name {
			continue
		}

		image, err := s.InspectImage(container.Image)
		if err != nil {
			log.Errorf("ERROR: Unable to inspect image: %s", container.Image)
			continue
		}

		if image == nil {
			log.Errorf("ERROR: Image for container %s does not exist!", container.ID[0:12])
			continue

		}

		version := env["GALAXY_VERSION"]

		imageDiffers := image.ID != appCfg.VersionID() && appCfg.VersionID() != ""
		versionDiffers := version != strconv.FormatInt(appCfg.ID(), 10) && version != ""

		if imageDiffers || versionDiffers {
			s.stopContainer(container)
			stopped = stopped + 1
		}
	}
	return nil
}

func (s *ServiceRuntime) StopAllButCurrentVersion(appCfg *config.AppConfig) error {
	containers, err := s.ManagedContainers()
	if err != nil {
		return err
	}

	for _, container := range containers {

		env := s.EnvFor(container)
		// Container name does match one that would be started w/ this service config
		if env["GALAXY_APP"] != appCfg.Name {
			continue
		}

		image, err := s.InspectImage(container.Image)
		if err != nil {
			log.Errorf("ERROR: Unable to inspect image: %s", container.Image)
			continue
		}

		if image == nil {
			log.Errorf("ERROR: Image for container %s does not exist!", container.ID[0:12])
			continue

		}

		version := env["GALAXY_VERSION"]

		imageDiffers := image.ID != appCfg.VersionID() && appCfg.VersionID() != ""
		versionDiffers := version != strconv.FormatInt(appCfg.ID(), 10) && version != ""

		if imageDiffers || versionDiffers {
			s.stopContainer(container)
		}
	}
	return nil
}

func (s *ServiceRuntime) StopAllButLatestService(name string, stopCutoff int64) error {
	containers, err := s.ManagedContainers()
	if err != nil {
		return err
	}

	var toStop []*docker.Container
	var latestContainer *docker.Container
	for _, container := range containers {
		if s.EnvFor(container)["GALAXY_APP"] == name {
			if latestContainer == nil || container.Created.After(latestContainer.Created) {
				latestContainer = container
			}
			toStop = append(toStop, container)
		}
	}

	for _, container := range toStop {
		if container.ID != latestContainer.ID &&
			container.Created.Unix() < (time.Now().Unix()-stopCutoff) {
			s.stopContainer(container)
		}
	}
	return nil
}

func (s *ServiceRuntime) StopAllButLatest(env string, stopCutoff int64) error {

	containers, err := s.ManagedContainers()
	if err != nil {
		return err
	}

	for _, c := range containers {
		s.StopAllButLatestService(s.EnvFor(c)["GALAXY_APP"], stopCutoff)
	}

	return nil
}

func (s *ServiceRuntime) StopAll(env string) error {

	containers, err := s.ManagedContainers()
	if err != nil {
		return err
	}

	for _, c := range containers {
		s.stopContainer(c)
	}

	return nil
}

func (s *ServiceRuntime) GetImageByName(img string) (*docker.APIImages, error) {
	imgs, err := s.ensureDockerClient().ListImages(docker.ListImagesOptions{All: true})
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

func (s *ServiceRuntime) RunCommand(env string, appCfg *config.AppConfig, cmd []string) (*docker.Container, error) {

	// see if we have the image locally
	fmt.Fprintf(os.Stderr, "Pulling latest image for %s\n", appCfg.Version())
	_, err := s.PullImage(appCfg.Version(), appCfg.VersionID())
	if err != nil {
		return nil, err
	}

	instanceId, err := s.NextInstanceSlot(appCfg.Name, strconv.FormatInt(appCfg.ID(), 10))
	if err != nil {
		return nil, err
	}

	envVars := []string{"ENV=" + env}

	for key, value := range appCfg.Env() {
		if key == "ENV" {
			continue
		}
		envVars = append(envVars, strings.ToUpper(key)+"="+s.replaceVarEnv(value, s.hostIP))
	}
	envVars = append(envVars, "GALAXY_APP="+appCfg.Name)
	envVars = append(envVars, "GALAXY_VERSION="+strconv.FormatInt(appCfg.ID(), 10))
	envVars = append(envVars, fmt.Sprintf("GALAXY_INSTANCE=%s", strconv.FormatInt(int64(instanceId), 10)))

	runCmd := []string{"/bin/bash", "-c", strings.Join(cmd, " ")}

	container, err := s.ensureDockerClient().CreateContainer(docker.CreateContainerOptions{
		Config: &docker.Config{
			Image:        appCfg.Version(),
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
	config := &docker.HostConfig{}
	if s.dns != "" {
		config.DNS = []string{s.dns}
	}
	err = s.ensureDockerClient().StartContainer(container.ID, config)

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

func (s *ServiceRuntime) StartInteractive(env, pool string, appCfg *config.AppConfig) error {

	// see if we have the image locally
	fmt.Fprintf(os.Stderr, "Pulling latest image for %s\n", appCfg.Version())
	_, err := s.PullImage(appCfg.Version(), appCfg.VersionID())
	if err != nil {
		return err
	}

	args := []string{
		"run", "--rm", "-i",
	}
	args = append(args, "-e")
	args = append(args, "ENV"+"="+env)

	for key, value := range appCfg.Env() {
		if key == "ENV" {
			continue
		}

		args = append(args, "-e")
		args = append(args, strings.ToUpper(key)+"="+s.replaceVarEnv(value, s.hostIP))
	}

	args = append(args, "-e")
	args = append(args, fmt.Sprintf("HOST_IP=%s", s.hostIP))
	if s.dns != "" {
		args = append(args, "--dns")
		args = append(args, s.dns)
	}
	args = append(args, "-e")
	args = append(args, fmt.Sprintf("GALAXY_APP=%s", appCfg.Name))
	args = append(args, "-e")
	args = append(args, fmt.Sprintf("GALAXY_VERSION=%s", strconv.FormatInt(appCfg.ID(), 10)))

	instanceId, err := s.NextInstanceSlot(appCfg.Name, strconv.FormatInt(appCfg.ID(), 10))
	if err != nil {
		return err
	}
	args = append(args, "-e")
	args = append(args, fmt.Sprintf("GALAXY_INSTANCE=%s", strconv.FormatInt(int64(instanceId), 10)))

	publicDns, err := EC2PublicHostname()
	if err != nil {
		log.Warnf("Unable to determine public hostname. Not on AWS? %s", err)
		publicDns = "127.0.0.1"
	}

	args = append(args, "-e")
	args = append(args, fmt.Sprintf("PUBLIC_HOSTNAME=%s", publicDns))

	mem := appCfg.GetMemory(pool)
	if mem != "" {
		args = append(args, "-m")
		args = append(args, mem)
	}

	cpu := appCfg.GetCPUShares(pool)
	if cpu != "" {
		args = append(args, "-c")
		args = append(args, cpu)
	}

	args = append(args, []string{"-t", appCfg.Version(), "/bin/bash"}...)
	// shell out to docker run to get signal forwarded and terminal setup correctly
	//cmd := exec.Command("docker", "run", "-rm", "-i", "-t", appCfg.Version(), "/bin/bash")
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

func (s *ServiceRuntime) Start(env, pool string, appCfg *config.AppConfig) (*docker.Container, error) {

	img := appCfg.Version()

	imgIdRef := appCfg.Version()
	if appCfg.VersionID() != "" {
		imgIdRef = appCfg.VersionID()
	}
	// see if we have the image locally
	image, err := s.PullImage(img, imgIdRef)
	if err != nil {
		return nil, err
	}

	// setup env vars from etcd
	var envVars []string
	envVars = append(envVars, "ENV"+"="+env)

	for key, value := range appCfg.Env() {
		if key == "ENV" {
			continue
		}
		envVars = append(envVars, strings.ToUpper(key)+"="+s.replaceVarEnv(value, s.hostIP))
	}

	instanceId, err := s.NextInstanceSlot(appCfg.Name, strconv.FormatInt(appCfg.ID(), 10))
	if err != nil {
		return nil, err
	}

	envVars = append(envVars, fmt.Sprintf("HOST_IP=%s", s.hostIP))
	envVars = append(envVars, fmt.Sprintf("GALAXY_APP=%s", appCfg.Name))
	envVars = append(envVars, fmt.Sprintf("GALAXY_VERSION=%s", strconv.FormatInt(appCfg.ID(), 10)))
	envVars = append(envVars, fmt.Sprintf("GALAXY_INSTANCE=%s", strconv.FormatInt(int64(instanceId), 10)))

	publicDns, err := EC2PublicHostname()
	if err != nil {
		log.Warnf("Unable to determine public hostname. Not on AWS? %s", err)
		publicDns = "127.0.0.1"
	}
	envVars = append(envVars, fmt.Sprintf("PUBLIC_HOSTNAME=%s", publicDns))

	containerName := appCfg.ContainerName() + "." + strconv.FormatInt(int64(instanceId), 10)
	container, err := s.ensureDockerClient().InspectContainer(containerName)
	_, ok := err.(*docker.NoSuchContainer)
	if err != nil && !ok {
		return nil, err
	}

	// Existing container is running or stopped.  If the image has changed, stop
	// and re-create it.
	if container != nil && container.Image != image.ID {
		if container.State.Running {
			log.Printf("Stopping %s version %s running as %s", appCfg.Name, appCfg.Version(), container.ID[0:12])
			err := s.ensureDockerClient().StopContainer(container.ID, 10)
			if err != nil {
				return nil, err
			}
		}

		log.Printf("Removing %s version %s running as %s", appCfg.Name, appCfg.Version(), container.ID[0:12])
		err = s.ensureDockerClient().RemoveContainer(docker.RemoveContainerOptions{
			ID: container.ID,
		})
		if err != nil {
			return nil, err
		}
		container = nil
	}

	if container == nil {

		config := &docker.Config{
			Image: img,
			Env:   envVars,
		}

		mem := appCfg.GetMemory(pool)
		if mem != "" {
			m, err := utils.ParseMemory(mem)
			if err != nil {
				return nil, err
			}
			config.Memory = m
		}

		cpu := appCfg.GetCPUShares(pool)
		if cpu != "" {
			if c, err := strconv.Atoi(cpu); err == nil {
				config.CPUShares = int64(c)
			}
		}

		log.Printf("Creating %s version %s", appCfg.Name, appCfg.Version())
		container, err = s.ensureDockerClient().CreateContainer(docker.CreateContainerOptions{
			Name:   containerName,
			Config: config,
		})
		if err != nil {
			return nil, err
		}
	}

	log.Printf("Starting %s version %s running as %s", appCfg.Name, appCfg.Version(), container.ID[0:12])

	config := &docker.HostConfig{
		PublishAllPorts: true,
	}

	if s.dns != "" {
		config.DNS = []string{s.dns}
	}
	err = s.ensureDockerClient().StartContainer(container.ID, config)

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

func (s *ServiceRuntime) StartIfNotRunning(env, pool string, appCfg *config.AppConfig) (bool, *docker.Container, error) {

	containers, err := s.ManagedContainers()
	if err != nil {
		return false, nil, err
	}

	image, err := s.InspectImage(appCfg.Version())
	if err != nil {
		return false, nil, err
	}

	var running *docker.Container
	for _, container := range containers {
		cenv := s.EnvFor(container)
		if cenv["GALAXY_APP"] == appCfg.Name &&
			cenv["GALAXY_VERSION"] == strconv.FormatInt(appCfg.ID(), 10) &&
			image.ID == container.Image {
			running = container
			break
		}
	}

	if running != nil {
		return false, running, nil
	}

	err = s.ensureDockerClient().RemoveContainer(docker.RemoveContainerOptions{
		ID: appCfg.ContainerName(),
	})
	_, ok := err.(*docker.NoSuchContainer)
	if err != nil && !ok {
		return false, nil, err
	}

	container, err := s.Start(env, pool, appCfg)
	return true, container, err
}

func (s *ServiceRuntime) PullImage(version, id string) (*docker.Image, error) {
	image, err := s.InspectImage(version)

	if err != nil && err != docker.ErrNoSuchImage {
		return nil, err
	}

	if image != nil && image.ID == id {
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

	return s.InspectImage(version)

}

func (s *ServiceRuntime) RegisterAll(env, pool, hostIP string) ([]*registry.ServiceRegistration, error) {
	containers, err := s.ManagedContainers()
	if err != nil {
		return nil, err
	}

	registrations := []*registry.ServiceRegistration{}

	for _, container := range containers {
		name := s.EnvFor(container)["GALAXY_APP"]
		registration, err := s.serviceRegistry.RegisterService(env, pool, hostIP, container)
		if err != nil {
			log.Printf("ERROR: Could not register %s: %s\n", name, err)
			continue
		}
		registrations = append(registrations, registration)
	}

	return registrations, nil

}

func (s *ServiceRuntime) UnRegisterAll(env, pool, hostIP string) ([]*docker.Container, error) {

	containers, err := s.ManagedContainers()
	if err != nil {
		return nil, err
	}

	removed := []*docker.Container{}

	for _, container := range containers {
		name := s.EnvFor(container)["GALAXY_APP"]
		_, err = s.serviceRegistry.UnRegisterService(env, pool, hostIP, container)
		if err != nil {
			log.Printf("ERROR: Could not unregister %s: %s\n", name, err)
			return removed, err
		}

		removed = append(removed, container)
		log.Printf("Unregistered %s as %s", container.ID[0:12], name)
	}

	return removed, nil
}

func (s *ServiceRuntime) RegisterEvents(env, pool, hostIP string, listener chan ContainerEvent) error {
	go func() {
		c := make(chan *docker.APIEvents)

		watching := false
		for {

			err := s.Ping()
			if err != nil {
				log.Errorf("ERROR: Unable to ping docker daemaon: %s", err)
				if watching {
					s.ensureDockerClient().RemoveEventListener(c)
					watching = false
				}
				time.Sleep(10 * time.Second)
				continue

			}

			if !watching {
				err = s.ensureDockerClient().AddEventListener(c)
				if err != nil && err != docker.ErrListenerAlreadyExists {
					log.Printf("ERROR: Error registering docker event listener: %s", err)
					time.Sleep(10 * time.Second)
					continue
				}
				watching = true
			}

			select {

			case e := <-c:
				if e.Status == "start" || e.Status == "stop" || e.Status == "die" {
					container, err := s.InspectContainer(e.ID)
					if err != nil {
						log.Printf("ERROR: Error inspecting container: %s", err)
						continue
					}

					if container == nil {
						log.Printf("WARN: Nil container returned for %s", e.ID[:12])
						continue
					}

					name := s.EnvFor(container)["GALAXY_APP"]
					if name != "" {
						registration, err := s.serviceRegistry.GetServiceRegistration(env, pool, hostIP, container)
						if err != nil {
							log.Printf("WARN: Could not find service registration for %s/%s: %s", name, container.ID[:12], err)
							continue
						}

						if registration == nil && e.Status != "start" {
							continue
						}

						listener <- ContainerEvent{
							Status:              e.Status,
							Container:           container,
							ServiceRegistration: registration,
						}
					}

				}
			case <-time.After(10 * time.Second):
				// check for docker liveness
			}

		}
	}()
	return nil
}

func (s *ServiceRuntime) EnvFor(container *docker.Container) map[string]string {
	env := map[string]string{}
	for _, item := range container.Config.Env {
		sep := strings.Index(item, "=")
		k := item[0:sep]
		v := item[sep+1:]
		env[k] = v
	}
	return env
}

func (s *ServiceRuntime) ManagedContainers() ([]*docker.Container, error) {
	apps := []*docker.Container{}
	containers, err := s.ensureDockerClient().ListContainers(docker.ListContainersOptions{
		All: false,
	})
	if err != nil {
		return apps, err
	}

	for _, c := range containers {
		container, err := s.ensureDockerClient().InspectContainer(c.ID)
		if err != nil {
			log.Printf("ERROR: Unable to inspect container: %s\n", c.ID)
			continue
		}
		name := s.EnvFor(container)["GALAXY_APP"]
		if name != "" {
			apps = append(apps, container)
		}
	}
	return apps, nil
}

func (s *ServiceRuntime) instanceIds(app, versionId string) ([]int, error) {
	containers, err := s.ManagedContainers()
	if err != nil {
		return []int{}, err
	}

	instances := []int{}
	for _, c := range containers {
		ga := s.EnvFor(c)["GALAXY_APP"]

		if ga != app {
			continue
		}

		gi := s.EnvFor(c)["GALAXY_INSTANCE"]
		gv := s.EnvFor(c)["GALAXY_VERSION"]
		if gi != "" {
			i, err := strconv.ParseInt(gi, 10, 64)
			if err != nil {
				log.Warnf("WARN: Invalid number %s for %s. Ignoring.", gi, c.ID[:12])
				continue
			}

			if versionId != "" && gv != versionId {
				continue
			}
			instances = append(instances, int(i))
		}
	}
	return instances, nil
}

func (s *ServiceRuntime) InstanceCount(app, versionId string) (int, error) {
	instances, err := s.instanceIds(app, versionId)
	return len(instances), err
}

func (s *ServiceRuntime) NextInstanceSlot(app, versionId string) (int, error) {
	instances, err := s.instanceIds(app, versionId)
	if err != nil {
		return 0, err
	}

	return utils.NextSlot(instances), nil
}

func (s ServiceRuntime) replaceVarEnv(in, hostIp string) string {
	out := strings.Replace(in, "$HOST_IP", hostIp, -1)
	return strings.Replace(out, "$DOCKER_IP", s.dockerIP, -1)
}
