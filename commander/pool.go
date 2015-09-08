package commander

import (
	"fmt"
	"strings"

	"github.com/litl/galaxy/config"
	"github.com/ryanuber/columnize"
)

// TODO: shouldn't the command cmd be printing the output, and not the package?
// The app, config, host, and runtime sections all do this too. (otherwise we
// should just combine the two packages). And why do we print the output here,
// but print the error in main???
func ListPools(configStore *config.Store, env string) error {
	var envs []string
	var err error

	if env != "" {
		envs = []string{env}
	} else {
		envs, err = configStore.ListEnvs()
		if err != nil {
			return err
		}

	}

	columns := []string{"ENV | POOL | APPS "}

	for _, env := range envs {

		pools, err := configStore.ListPools(env)
		if err != nil {
			return fmt.Errorf("ERROR: cannot list pools: %s", err)
		}

		if len(pools) == 0 {
			columns = append(columns, strings.Join([]string{
				env,
				"",
				""}, " | "))
			continue
		}

		for _, pool := range pools {

			assigments, err := configStore.ListAssignments(env, pool)
			if err != nil {
				fmt.Printf("ERROR: cannot list pool assignments for %s/%s: %s", env, pool, err)
			}

			columns = append(columns, strings.Join([]string{
				env,
				pool,
				strings.Join(assigments, ",")}, " | "))

		}
	}
	fmt.Println(columnize.SimpleFormat(columns))
	return nil
}

// Create a pool for an environment
func PoolCreate(configStore *config.Store, env, pool string) error {
	exists, err := configStore.PoolExists(env, pool)
	if err != nil {
		return err
	} else if exists {
		return fmt.Errorf("pool '%s' exists", pool)
	}

	_, err = configStore.CreatePool(pool, env)
	if err != nil {
		return err
	}

	return nil
}

func PoolDelete(configStore *config.Store, env, pool string) error {
	exists, err := configStore.PoolExists(env, pool)
	if err != nil {
		return err
	} else if !exists {
		return fmt.Errorf("pool '%s' does not exist", pool)
	}

	empty, err := configStore.DeletePool(pool, env)
	if err != nil {
		return err
	}

	if !empty {
		return fmt.Errorf("pool '%s' is not epmty", pool)
	}
	return nil
}
