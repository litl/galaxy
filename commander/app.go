package commander

import (
	"fmt"
	"github.com/litl/galaxy/config"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/runtime"
	"github.com/ryanuber/columnize"
	"strings"
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

	columns := []string{"ENV | NAME | VERSION | PORT "}

	for _, env := range envs {

		appList, err := configStore.ListApps(env)
		if err != nil {
			return err
		}

		for _, app := range appList {
			name := app.Name
			port := app.EnvGet("GALAXY_PORT")
			versionDeployed := app.Version()

			columns = append(columns, strings.Join([]string{
				env,
				name,
				versionDeployed,
				port,
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

func AppDeploy(configStore *config.Store, serviceRuntime *runtime.ServiceRuntime, app, env, version string, force bool) error {

	image, err := serviceRuntime.PullImage(version, "", force)
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
	serviceConfig, err := configStore.GetApp(app, env)
	if err != nil {
		return fmt.Errorf("unable to run command: %s.", err)

	}

	_, err = serviceRuntime.RunCommand(serviceConfig, args)
	if err != nil {
		return fmt.Errorf("could not start container: %s", err)
	}
	return nil
}

func AppShell(configStore *config.Store, serviceRuntime *runtime.ServiceRuntime, app, env string) error {
	serviceConfig, err := configStore.GetApp(app, env)
	if err != nil {
		return fmt.Errorf("unable to run command: %s.", err)
	}

	err = serviceRuntime.StartInteractive(env, serviceConfig)
	if err != nil {
		return fmt.Errorf("could not start container: %s", err)
	}
	return nil
}
