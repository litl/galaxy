package runtime

import (
	"errors"
	"net"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"strings"
	"time"

	auth "github.com/dotcloud/docker/registry"
	"github.com/fsouza/go-dockerclient"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/utils"
)

var blacklistedContainerId = make(map[string]bool)

type ServiceRuntime struct {
	dockerClient    *docker.Client
	authConfig      *auth.ConfigFile
	shuttleHost     string
	serviceRegistry *registry.ServiceRegistry
}

func NewServiceRuntime(shuttleHost, env, pool, redisHost string) *ServiceRuntime {
	if shuttleHost == "" {
		dockerZero, err := net.InterfaceByName("docker0")
		if err != nil {
			log.Fatalf("ERROR: Unable to find docker0 interface")
		}
		addrs, _ := dockerZero.Addrs()
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err != nil {
				log.Fatalf("ERROR: Unable to parse %s", addr.String())
			}
			if ip.DefaultMask() != nil {
				shuttleHost = ip.String()
				break
			}
		}
	}

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
		serviceRegistry: serviceRegistry,
	}

}

func (s *ServiceRuntime) ensureDockerClient() *docker.Client {
	if s.dockerClient == nil {
		endpoint := "unix:///var/run/docker.sock"
		client, err := docker.NewClient(endpoint)
		if err != nil {
			panic(err)
		}
		s.dockerClient = client

	}
	return s.dockerClient
}

func (s *ServiceRuntime) InspectImage(image string) (*docker.Image, error) {
	return s.ensureDockerClient().InspectImage(image)
}

func (s *ServiceRuntime) IsRunning(img string) (string, error) {

	image, err := s.ensureDockerClient().InspectImage(img)
	if err != nil {
		return "", err
	}

	containers, err := s.ensureDockerClient().ListContainers(docker.ListContainersOptions{
		All: false,
	})
	if err != nil {
		return "", err
	}

	for _, container := range containers {
		dockerContainer, err := s.ensureDockerClient().InspectContainer(container.ID)
		if err != nil {
			return "", err
		}

		if image.ID == dockerContainer.Image {
			return container.ID, nil
		}
	}
	return "", nil
}

func (s *ServiceRuntime) StopAllButLatest(img string, latest *docker.Container, stopCutoff int64) error {
	imageParts := strings.Split(img, ":")
	repository := imageParts[0]

	containers, err := s.ensureDockerClient().ListContainers(docker.ListContainersOptions{
		All:    false,
		Before: latest.ID,
	})
	if err != nil {
		return err
	}
	for _, container := range containers {

		if strings.HasPrefix(container.Image, repository) && container.ID != latest.ID &&
			container.Created < (time.Now().Unix()-stopCutoff) {

			// HACK: Docker 0.9 gets zombie containers randomly.  The only way to remove
			// them is to restart the docker daemon.  If we timeout once trying to stop
			// one of these containers, blacklist it and leave it running

			if _, ok := blacklistedContainerId[container.ID]; ok {
				log.Printf("Container %s blacklisted. Won't try to stop.\n", container.ID)
				continue
			}

			log.Printf("Stopping container %s\n", container.ID)
			c := make(chan error, 1)
			go func() { c <- s.ensureDockerClient().StopContainer(container.ID, 10) }()
			select {
			case err := <-c:
				if err != nil {
					log.Printf("ERROR: Unable to stop container: %s\n", container.ID)
					continue
				}
			case <-time.After(20 * time.Second):
				blacklistedContainerId[container.ID] = true
				log.Printf("ERROR: Timed out trying to stop container. Zombie?. Blacklisting: %s\n", container.ID)
				continue
			}

			s.ensureDockerClient().RemoveContainer(docker.RemoveContainerOptions{
				ID:            container.ID,
				RemoveVolumes: true,
			})
		}
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

func (s *ServiceRuntime) StartInteractive(serviceConfig *registry.ServiceConfig, cmd []string) (*docker.Container, error) {

	registry, repository, _ := utils.SplitDockerImage(serviceConfig.Version())

	// see if we have the image locally
	_, err := s.ensureDockerClient().InspectImage(serviceConfig.Version())

	if err == docker.ErrNoSuchImage {
		_, err := s.PullImage(registry, repository)
		if err != nil {
			return nil, err
		}
	}

	// setup env vars from etcd
	envVars := []string{
		"HOME=/",
		"PATH=" + "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOSTNAME=" + "app",
		"TERM=xterm",
	}

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
		log.Println("Stopping command")
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
		&docker.HostConfig{})

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

func (s *ServiceRuntime) Start(serviceConfig *registry.ServiceConfig) (*docker.Container, error) {
	img := serviceConfig.Version()
	registry, repository, _ := utils.SplitDockerImage(img)

	// see if we have the image locally
	_, err := s.ensureDockerClient().InspectImage(img)

	if err == docker.ErrNoSuchImage {
		_, err = s.PullImage(registry, repository)
		if err != nil {
			return nil, err
		}
	}

	// setup env vars from etcd
	var envVars []string
	for key, value := range serviceConfig.Env() {
		envVars = append(envVars, strings.ToUpper(key)+"="+value)
	}

	serviceConfigs, err := s.serviceRegistry.ListApps()
	if err != nil {
		return nil, err
	}

	for _, config := range serviceConfigs {
		for port, _ := range config.Ports() {
			// FIXME: Need a deterministic way to map local shuttle ports to remote services
			envVars = append(envVars, strings.ToUpper(config.Name)+"_ADDR_"+port+"="+s.shuttleHost+":"+port)
		}
	}

	containerName := serviceConfig.Name + "_" + strconv.FormatInt(serviceConfig.ID(), 10)
	container, err := s.ensureDockerClient().InspectContainer(containerName)
	_, ok := err.(*docker.NoSuchContainer)
	if err != nil && !ok {
		return nil, err
	}

	if container == nil {
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

	err = s.ensureDockerClient().StartContainer(container.ID,
		&docker.HostConfig{
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

func (s *ServiceRuntime) StartIfNotRunning(serviceConfig *registry.ServiceConfig) (*docker.Container, error) {
	img := serviceConfig.Version()
	containerId, err := s.IsRunning(img)
	if err != nil && err != docker.ErrNoSuchImage {
		return nil, err
	}

	// already running, grab the container details
	if containerId != "" {
		return s.ensureDockerClient().InspectContainer(containerId)
	}
	return s.Start(serviceConfig)
}

func (s *ServiceRuntime) PullImage(registry, repository string) (*docker.Image, error) {
	// No, pull it down locally
	pullOpts := docker.PullImageOptions{
		Repository:   repository,
		OutputStream: os.Stdout}

	dockerAuth := docker.AuthConfiguration{}
	if registry != "" && s.authConfig == nil {

		pullOpts.Repository = registry + "/" + repository
		pullOpts.Registry = registry

		currentUser, err := user.Current()
		if err != nil {
			panic(err)
		}

		// use ~/.dockercfg
		authConfig, err := auth.LoadConfig(currentUser.HomeDir)
		if err != nil {
			panic(err)
		}

		pullOpts.Registry = registry
		authCreds := authConfig.ResolveAuthConfig(registry)

		dockerAuth.Username = authCreds.Username
		dockerAuth.Password = authCreds.Password
		dockerAuth.Email = authCreds.Email
	}

	err := s.ensureDockerClient().PullImage(pullOpts, dockerAuth)
	if err != nil {
		return nil, err
	}
	return s.ensureDockerClient().InspectImage(registry + "/" + repository)

}
