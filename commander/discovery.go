package commander

import (
	"strings"
	"time"

	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/runtime"
	"github.com/litl/galaxy/utils"
	"github.com/ryanuber/columnize"
)

func DiscoveryStatus(serviceRuntime *runtime.ServiceRuntime, serviceRegistry *registry.ServiceRegistry, env, pool, hostIP string) error {

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
