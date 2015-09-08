package commander

import (
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"strings"

	"github.com/litl/galaxy/config"
	"github.com/litl/galaxy/log"
)

func ConfigList(configStore *config.Store, app, env string) error {

	cfg, err := configStore.GetApp(app, env)
	if err != nil {
		return err
	}

	if cfg == nil {
		return fmt.Errorf("unable to list config for %s.", app)
	}

	keys := sort.StringSlice{"ENV"}
	for k, _ := range cfg.Env() {
		keys = append(keys, k)
	}

	keys.Sort()

	for _, k := range keys {
		if k == "ENV" {
			log.Printf("%s=%s\n", k, env)
			continue
		}
		fmt.Printf("%s=%s\n", k, cfg.Env()[k])
	}

	return nil
}

func ConfigSet(configStore *config.Store, app, env string, envVars []string) error {

	if len(envVars) == 0 {
		bytes, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			return err

		}
		envVars = strings.Split(string(bytes), "\n")
	}

	if len(envVars) == 0 {
		return fmt.Errorf("no config values specified.")
	}

	svcCfg, err := configStore.GetApp(app, env)
	if err != nil {
		return fmt.Errorf("unable to set config: %s.", err)
	}

	if svcCfg == nil {
		svcCfg = configStore.NewAppConfig(app, "")
	}

	updated := false
	for _, arg := range envVars {

		if strings.TrimSpace(arg) == "" {
			continue
		}

		if !strings.Contains(arg, "=") {
			return fmt.Errorf("bad config variable format: %s", arg)
		}

		sep := strings.Index(arg, "=")
		k := strings.ToUpper(strings.TrimSpace(arg[0:sep]))
		v := strings.TrimSpace(arg[sep+1:])
		if k == "ENV" {
			log.Warnf("%s cannot be updated.", k)
			continue
		}

		log.Printf("%s=%s\n", k, v)
		svcCfg.EnvSet(k, v)
		updated = true
	}

	if !updated {
		return fmt.Errorf("configuration NOT changed for %s", app)
	}

	updated, err = configStore.UpdateApp(svcCfg, env)
	if err != nil {
		return fmt.Errorf("unable to set config: %s.", err)
	}

	if !updated {
		return fmt.Errorf("configuration NOT changed for %s", app)
	}
	log.Printf("Configuration changed for %s. v%d\n", app, svcCfg.ID())
	return nil
}

func ConfigGet(configStore *config.Store, app, env string, envVars []string) error {

	cfg, err := configStore.GetApp(app, env)
	if err != nil {
		return err
	}

	for _, arg := range envVars {
		fmt.Printf("%s=%s\n", strings.ToUpper(arg), cfg.Env()[strings.ToUpper(arg)])
	}
	return nil
}

func ConfigUnset(configStore *config.Store, app, env string, envVars []string) error {

	if len(envVars) == 0 {
		return fmt.Errorf("no config values specified.")
	}

	svcCfg, err := configStore.GetApp(app, env)
	if err != nil {
		return fmt.Errorf("unable to unset config: %s.", err)
	}

	updated := false
	for _, arg := range envVars {
		k := strings.ToUpper(strings.TrimSpace(arg))
		if k == "ENV" || svcCfg.EnvGet(k) == "" {
			log.Warnf("%s cannot be unset.", k)
			continue
		}

		log.Printf("%s\n", k)
		svcCfg.EnvSet(strings.ToUpper(arg), "")
		updated = true
	}

	if !updated {
		return fmt.Errorf("Configuration NOT changed for %s", app)
	}

	updated, err = configStore.UpdateApp(svcCfg, env)
	if err != nil {
		return fmt.Errorf("ERROR: Unable to unset config: %s.", err)

	}

	if !updated {
		return fmt.Errorf("Configuration NOT changed for %s", app)

	}
	log.Printf("Configuration changed for %s. v%d.\n", app, svcCfg.ID())
	return nil
}
