package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"

	"github.com/litl/galaxy/config"
)

type dumpConfig struct {
	Pools   []string
	Hosts   []config.HostInfo
	Configs []config.AppDefinition
	Regs    []config.ServiceRegistration
}

// Dump everything related to a single environment from galaxy to stdout,
// including current runtime config, hosts, IPs etc.
// This isn't really useful other than to sync between config backends, but we
// can probably convert this to a better backup once we stabilize the code some
// more.
func dump(env string) {
	envDump := &dumpConfig{
		Configs: []config.AppDefinition{},
		Regs:    []config.ServiceRegistration{},
	}

	pools, err := configStore.ListPools(env)
	if err != nil {
		log.Fatal(err)
	}

	envDump.Pools = pools

	for _, pool := range pools {
		hosts, err := configStore.ListHosts(env, pool)
		if err != nil {
			log.Fatal(err)
		}
		for _, host := range hosts {
			host.Pool = pool
			envDump.Hosts = append(envDump.Hosts, host)
		}
	}

	apps, err := configStore.ListApps(env)
	if err != nil {
		log.Fatal(err)
	}

	for _, app := range apps {
		// AppDefinition is intended to be serializable itself
		if ad, ok := app.(*config.AppDefinition); ok {
			envDump.Configs = append(envDump.Configs, *ad)
			continue
		}

		// otherwise, manually convert the App to an AppDefinition
		ad := config.AppDefinition{
			AppName:     app.Name(),
			Image:       app.Version(),
			ImageID:     app.VersionID(),
			Environment: app.Env(),
		}

		for _, pool := range app.RuntimePools() {
			ad.SetProcesses(pool, app.GetProcesses(pool))
			ad.SetMemory(pool, app.GetMemory(pool))
			ad.SetCPUShares(pool, app.GetCPUShares(pool))
		}

		envDump.Configs = append(envDump.Configs, ad)
	}

	// The registrations are temporary, but dump them anyway, so we can try and
	// convert an environment by keeping the runtime config in sync.
	regs, err := configStore.ListRegistrations(env)
	if err != nil {
		log.Fatal(err)
	}
	envDump.Regs = append(envDump.Regs, regs...)

	js, err := json.MarshalIndent(envDump, "", "  ")
	if err != nil {
		log.Fatal(err)
	}

	os.Stdout.Write(js)
}

// Restore everything we can from a Galaxy dump on stdin.
// This probably will panic if not using consul
func restore(env string) {
	js, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}

	envDump := dumpConfig{}
	err = json.Unmarshal(js, &envDump)
	if err != nil {
		log.Fatal(err)
	}

	for _, pool := range envDump.Pools {
		_, err := configStore.CreatePool(env, pool)
		if err != nil {
			log.Println(err)
		}
	}

	for _, appDef := range envDump.Configs {
		_, err := configStore.UpdateApp(&appDef, env)
		if err != nil {
			log.Println(err)
		}
	}

	for _, hostInfo := range envDump.Hosts {
		err := configStore.UpdateHost(env, pool, hostInfo)
		if err != nil {
			log.Println(err)
		}
	}

	for _, reg := range envDump.Regs {
		err := configStore.Backend.RegisterService(env, reg.Pool, &reg)
		if err != nil {
			log.Println(err)
		}
	}

}
