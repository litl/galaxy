package commander

import (
	"sort"

	"github.com/litl/galaxy/config"
)

// Balanced returns the number of instances that should be run on the host
// according to the desired state for the app in the given env and pool. The
// number returned for the host represent an approximately equal distribution
// across all hosts.
func Balanced(configStore *config.Store, hostId, app, env, pool string) (int, error) {
	hosts, err := configStore.ListHosts(env, pool)
	if err != nil {
		return 0, err
	}

	cfg, err := configStore.GetApp(app, env)
	if err != nil {
		return 0, err
	}

	desired := cfg.GetProcesses(pool)
	if desired == 0 {
		return 0, nil
	}

	if desired == -1 {
		return 1, nil
	}

	hostIds := []string{}
	for _, h := range hosts {
		hostIds = append(hostIds, h.HostIP)
	}
	sort.Strings(hostIds)

	hostIdx := -1
	for i, v := range hostIds {
		if v == hostId {
			hostIdx = i
			break
		}
	}

	if hostIdx < 0 {
		return 0, nil
	}

	count := 0
	for i := 0; i < desired; i++ {
		if i%len(hosts) == hostIdx {
			count = count + 1
		}
	}

	return count, nil
}
