package commander

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/litl/galaxy/config"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/runtime"
	"github.com/litl/galaxy/utils"
	"github.com/ryanuber/columnize"
)

func AppList(configStore *config.Store, env string) error {

	envs := []string{env}

	if env == "" {
		var err error
		envs, err = configStore.ListEnvs()
		if err != nil {
			return err
		}
	}

	columns := []string{"NAME | ENV | VERSION | IMAGE ID | CONFIG | POOLS "}

	for _, env := range envs {

		appList, err := configStore.ListApps(env)
		if err != nil {
			return err
		}

		pools, err := configStore.ListPools(env)
		if err != nil {
			return err
		}

		for _, app := range appList {
			name := app.Name()
			versionDeployed := app.Version()
			versionID := app.VersionID()
			if len(versionID) > 12 {
				versionID = versionID[:12]
			}

			assignments := []string{}
			for _, pool := range pools {
				aa, err := configStore.ListAssignments(env, pool)
				if err != nil {
					return err
				}
				if utils.StringInSlice(app.Name(), aa) {
					assignments = append(assignments, pool)
				}
			}

			columns = append(columns, strings.Join([]string{
				name,
				env,
				versionDeployed,
				versionID,
				strconv.FormatInt(app.ID(), 10),
				strings.Join(assignments, ","),
			}, " | "))
		}
	}
	output, _ := columnize.SimpleFormat(columns)
	log.Println(output)
	return nil
}

func AppCreate(configStore *config.Store, app, env string) error {
	// Don't allow creating runtime hosts entries
	if app == "hosts" {
		return fmt.Errorf("could not create app: %s", app)
	}

	created, err := configStore.CreateApp(app, env)

	if err != nil {
		return fmt.Errorf("could not create app: %s", err)
	}

	if created {
		log.Printf("Created %s in env %s.\n", app, env)
	} else {
		log.Printf("%s already exists in in env %s.", app, env)
	}
	return nil
}

func AppDelete(configStore *config.Store, app, env string) error {

	// Don't allow deleting runtime hosts entries
	if app == "hosts" || app == "pools" {
		return fmt.Errorf("could not delete app: %s", app)
	}

	deleted, err := configStore.DeleteApp(app, env)
	if err != nil {
		return fmt.Errorf("could not delete app: %s", err)
	}

	if deleted {
		log.Printf("Deleted %s from env %s.\n", app, env)
	} else {
		log.Printf("%s does not exists in env %s.\n", app, env)
	}
	return nil
}

func AppDeploy(configStore *config.Store, serviceRuntime *runtime.ServiceRuntime, app, env, version string) error {
	log.Printf("Pulling image %s...", version)

	image, err := serviceRuntime.PullImage(version, "")
	if image == nil || err != nil {
		return fmt.Errorf("unable to pull %s. Has it been released yet?", version)
	}

	svcCfg, err := configStore.GetApp(app, env)
	if err != nil {
		return fmt.Errorf("unable to deploy app: %s.", err)
	}

	if svcCfg == nil {
		return fmt.Errorf("app %s does not exist. Create it first.", app)
	}

	svcCfg.SetVersion(version)
	svcCfg.SetVersionID(image.ID)

	svcCfg.ClearPorts()
	for k, _ := range image.Config.ExposedPorts {
		svcCfg.AddPort(k.Port(), k.Proto())
	}

	updated, err := configStore.UpdateApp(svcCfg, env)
	if err != nil {
		return fmt.Errorf("could not store version: %s", err)
	}
	if !updated {
		return fmt.Errorf("%s NOT deployed.", version)
	}
	log.Printf("Deployed %s.\n", version)
	return nil
}

func AppRestart(Store *config.Store, app, env string) error {
	err := Store.NotifyRestart(app, env)
	if err != nil {
		return fmt.Errorf("could not restart %s: %s", app, err)
	}
	return nil
}

func AppRun(configStore *config.Store, serviceRuntime *runtime.ServiceRuntime, app, env string, args []string) error {
	appCfg, err := configStore.GetApp(app, env)
	if err != nil {
		return fmt.Errorf("unable to run command: %s.", err)

	}

	_, err = serviceRuntime.RunCommand(env, appCfg, args)
	if err != nil {
		return fmt.Errorf("could not start container: %s", err)
	}
	return nil
}

func AppShell(configStore *config.Store, serviceRuntime *runtime.ServiceRuntime, app, env, pool string) error {
	appCfg, err := configStore.GetApp(app, env)
	if err != nil {
		return fmt.Errorf("unable to run command: %s.", err)
	}

	err = serviceRuntime.StartInteractive(env, pool, appCfg)
	if err != nil {
		return fmt.Errorf("could not start container: %s", err)
	}
	return nil
}

func AppAssign(configStore *config.Store, app, env, pool string) error {
	// Don't allow deleting runtime hosts entries
	if app == "hosts" || app == "pools" {
		return fmt.Errorf("invalid app name: %s", app)
	}

	exists, err := configStore.PoolExists(env, pool)
	if err != nil {
		return err
	}

	if !exists {
		log.Warnf("WARN: Pool %s does not exist.", pool)
	}

	created, err := configStore.AssignApp(app, env, pool)

	if err != nil {
		return err
	}

	if created {
		log.Printf("Assigned %s in env %s to pool %s.\n", app, env, pool)
	} else {
		log.Printf("%s already assigned to pool %s in env %s.\n", app, pool, env)
	}
	return nil
}

func AppUnassign(configStore *config.Store, app, env, pool string) error {
	// Don't allow deleting runtime hosts entries
	if app == "hosts" || app == "pools" {
		return fmt.Errorf("invalid app name: %s", app)
	}

	deleted, err := configStore.UnassignApp(app, env, pool)
	if err != nil {
		return err
	}

	if deleted {
		log.Printf("Unassigned %s in env %s from pool %s\n", app, env, pool)
	} else {
		log.Printf("%s could not be unassigned.\n", pool)
	}
	return nil
}
