package commander

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/litl/galaxy/config"
	"github.com/litl/galaxy/utils"
	"github.com/ryanuber/columnize"
)

type RuntimeOptions struct {
	Ps              int
	Memory          string
	CPUShares       string
	VirtualHost     string
	Port            string
	MaintenanceMode string
}

func RuntimeList(configStore *config.Store, app, env, pool string) error {

	envs := []string{env}

	if env == "" {
		var err error
		envs, err = configStore.ListEnvs()
		if err != nil {
			return err
		}
	}

	columns := []string{"ENV | NAME | POOL | PS | MEM | VHOSTS | PORT | MAINT"}

	for _, env := range envs {

		appList, err := configStore.ListApps(env)
		if err != nil {
			return err
		}

		for _, appCfg := range appList {

			if app != "" && appCfg.Name() != app {
				continue
			}

			for _, p := range appCfg.RuntimePools() {

				if pool != "" && p != pool {
					continue
				}

				name := appCfg.Name()
				ps := appCfg.GetProcesses(p)
				mem := appCfg.GetMemory(p)

				columns = append(columns, strings.Join([]string{
					env,
					name,
					p,
					strconv.FormatInt(int64(ps), 10),
					mem,
					appCfg.Env()["VIRTUAL_HOST"],
					appCfg.Env()["GALAXY_PORT"],
					fmt.Sprint(appCfg.GetMaintenanceMode(p)),
				}, " | "))
			}
		}
	}
	output := columnize.SimpleFormat(columns)
	fmt.Println(output)
	return nil

}

func RuntimeSet(configStore *config.Store, app, env, pool string, options RuntimeOptions) (bool, error) {

	cfg, err := configStore.GetApp(app, env)
	if err != nil {
		return false, err
	}

	if options.Ps != 0 && options.Ps != cfg.GetProcesses(pool) {
		cfg.SetProcesses(pool, options.Ps)
	}

	if options.Memory != "" && options.Memory != cfg.GetMemory(pool) {
		cfg.SetMemory(pool, options.Memory)
	}

	vhosts := []string{}
	vhostsFromEnv := cfg.Env()["VIRTUAL_HOST"]
	if vhostsFromEnv != "" {
		vhosts = strings.Split(cfg.Env()["VIRTUAL_HOST"], ",")
	}

	if options.VirtualHost != "" && !utils.StringInSlice(options.VirtualHost, vhosts) {
		vhosts = append(vhosts, options.VirtualHost)
		cfg.EnvSet("VIRTUAL_HOST", strings.Join(vhosts, ","))
	}

	if options.Port != "" {
		cfg.EnvSet("GALAXY_PORT", options.Port)
	}

	if options.MaintenanceMode != "" {
		b, err := strconv.ParseBool(options.MaintenanceMode)
		if err != nil {
			return false, err
		}

		cfg.SetMaintenanceMode(pool, b)
	}

	return configStore.UpdateApp(cfg, env)
}

func RuntimeUnset(configStore *config.Store, app, env, pool string, options RuntimeOptions) (bool, error) {

	cfg, err := configStore.GetApp(app, env)
	if err != nil {
		return false, err
	}

	if options.Ps != 0 {
		cfg.SetProcesses(pool, -1)
	}

	if options.Memory != "" {
		cfg.SetMemory(pool, "")
	}

	vhosts := strings.Split(cfg.Env()["VIRTUAL_HOST"], ",")
	if options.VirtualHost != "" && utils.StringInSlice(options.VirtualHost, vhosts) {
		vhosts = utils.RemoveStringInSlice(options.VirtualHost, vhosts)
		cfg.EnvSet("VIRTUAL_HOST", strings.Join(vhosts, ","))
	}

	if options.Port != "" {
		cfg.EnvSet("GALAXY_PORT", "")
	}

	return configStore.UpdateApp(cfg, env)
}
