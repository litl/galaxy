package commander

import (
	"github.com/litl/galaxy/config"
	"github.com/litl/galaxy/log"
	"github.com/ryanuber/columnize"
	"strings"
)

func HostsList(configStore *config.Store, env, pool string) error {

	envs := []string{env}

	if env == "" {
		var err error
		envs, err = configStore.ListEnvs()
		if err != nil {
			return err
		}
	}

	columns := []string{"ENV | POOL | HOST IP "}

	for _, env := range envs {

		var err error
		pools := []string{pool}
		if pool != "" {
			pools, err = configStore.ListPools(env)
			if err != nil {
				return err
			}
		}

		for _, pool := range pools {

			hosts, err := configStore.ListHosts(env, pool)
			if err != nil {
				return err
			}

			for _, p := range hosts {
				columns = append(columns, strings.Join([]string{
					env,
					pool,
					p.HostIP,
				}, " | "))
			}
		}
	}
	output, _ := columnize.SimpleFormat(columns)
	log.Println(output)
	return nil

}
