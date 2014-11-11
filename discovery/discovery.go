package discovery

import (
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/runtime"
	shuttle "github.com/litl/galaxy/shuttle/client"
	"github.com/litl/galaxy/utils"
	"github.com/ryanuber/columnize"
	"os"
	"strings"
	"time"
)

func Status(serviceRuntime *runtime.ServiceRuntime, serviceRegistry *registry.ServiceRegistry, env, pool, hostIP string) error {

	containers, err := serviceRuntime.ManagedContainers()
	if err != nil {
		panic(err)
	}

	columns := []string{
		"APP | CONTAINER ID | IMAGE | EXTERNAL | INTERNAL | PORT | CREATED | EXPIRES"}

	for _, container := range containers {
		name := serviceRuntime.EnvFor(container)["GALAXY_APP"]
		registered, err := serviceRegistry.GetServiceRegistration(
			env, pool, hostIP, container)
		if err != nil {
			return err
		}

		if registered != nil {
			columns = append(columns,
				strings.Join([]string{
					registered.Name,
					registered.ContainerID[0:12],
					registered.Image,
					registered.ExternalAddr(),
					registered.InternalAddr(),
					registered.Port,
					utils.HumanDuration(time.Now().UTC().Sub(registered.StartedAt)) + " ago",
					"In " + utils.HumanDuration(registered.Expires.Sub(time.Now().UTC())),
				}, " | "))

		} else {
			columns = append(columns,
				strings.Join([]string{
					name,
					container.ID[0:12],
					container.Image,
					"",
					"",
					"",
					utils.HumanDuration(time.Now().Sub(container.Created)) + " ago",
					"",
				}, " | "))
		}

	}

	result, _ := columnize.SimpleFormat(columns)
	log.Println(result)
	return nil
}

func Unregister(serviceRuntime *runtime.ServiceRuntime, serviceRegistry *registry.ServiceRegistry,
	env, pool, hostIP, shuttleAddr string) {
	unregisterShuttle(serviceRegistry, env, hostIP, shuttleAddr)
	serviceRuntime.UnRegisterAll(env, pool, hostIP)
	os.Exit(0)
}

func RegisterAll(serviceRuntime *runtime.ServiceRuntime, serviceRegistry *registry.ServiceRegistry, env, pool, hostIP, shuttleAddr string, loggedOnce bool) {
	columns := []string{"CONTAINER ID | IMAGE | EXTERNAL | INTERNAL | CREATED | EXPIRES"}

	registrations, err := serviceRuntime.RegisterAll(env, pool, hostIP)
	if err != nil {
		log.Errorf("ERROR: Unable to register containers: %s", err)
		return
	}

	fn := log.Debugf
	if !loggedOnce {
		fn = log.Printf
	}

	for _, registration := range registrations {
		if !loggedOnce || time.Now().Unix()%60 < 10 {
			fn("Registered %s running as %s for %s%s", strings.TrimPrefix(registration.ContainerName, "/"),
				registration.ContainerID[0:12], registration.Name, locationAt(registration))
		}

		columns = append(columns, strings.Join([]string{
			registration.ContainerID[0:12],
			registration.Image,
			registration.ExternalAddr(),
			registration.InternalAddr(),
			utils.HumanDuration(time.Now().Sub(registration.StartedAt)) + " ago",
			"In " + utils.HumanDuration(registration.Expires.Sub(time.Now().UTC())),
		}, " | "))

	}

	registerShuttle(serviceRegistry, env, shuttleAddr)
}

func Register(serviceRuntime *runtime.ServiceRuntime, serviceRegistry *registry.ServiceRegistry, env, pool, hostIP, shuttleAddr string) {

	if shuttleAddr != "" {
		client = shuttle.NewClient(shuttleAddr)
	}

	RegisterAll(serviceRuntime, serviceRegistry, env, pool, hostIP, shuttleAddr, false)

	containerEvents := make(chan runtime.ContainerEvent)
	err := serviceRuntime.RegisterEvents(env, pool, hostIP, containerEvents)
	if err != nil {
		log.Printf("ERROR: Unable to register docker event listener: %s", err)
	}

	for {

		select {
		case ce := <-containerEvents:
			switch ce.Status {
			case "start":
				reg, err := serviceRegistry.RegisterService(env, pool, hostIP, ce.Container)
				if err != nil {
					log.Errorf("ERROR: Unable to register container: %s", err)
					continue
				}

				log.Printf("Registered %s running as %s for %s%s", strings.TrimPrefix(reg.ContainerName, "/"),
					reg.ContainerID[0:12], reg.Name, locationAt(reg))
				registerShuttle(serviceRegistry, env, shuttleAddr)
			case "die", "stop":
				reg, err := serviceRegistry.UnRegisterService(env, pool, hostIP, ce.Container)
				if err != nil {
					log.Errorf("ERROR: Unable to unregister container: %s", err)
					continue
				}

				if reg != nil {
					log.Printf("Unregistered %s running as %s for %s%s", strings.TrimPrefix(reg.ContainerName, "/"),
						reg.ContainerID[0:12], reg.Name, locationAt(reg))
				}
				pruneShuttleBackends(serviceRuntime, serviceRegistry, env, shuttleAddr)
			}

		case <-time.After(10 * time.Second):
			RegisterAll(serviceRuntime, serviceRegistry, env, pool, hostIP, shuttleAddr, true)
			pruneShuttleBackends(serviceRuntime, serviceRegistry, env, shuttleAddr)
		}
	}
}

func locationAt(reg *registry.ServiceRegistration) string {
	location := reg.ExternalAddr()
	if location != "" {
		location = " at " + location
	}
	return location
}
