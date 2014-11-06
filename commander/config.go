package commander

import (
	"fmt"
	"github.com/litl/galaxy/config"
	"github.com/litl/galaxy/log"
	"sort"
)

func ConfigList(configStore *config.Store, app, env string) error {

	cfg, err := configStore.GetApp(app, env)
	if err != nil {
		return err
	}

	if cfg == nil {
		return fmt.Errorf("unable to list config for %s.", app)
	}

	keys := sort.StringSlice{}
	for k, _ := range cfg.Env() {
		keys = append(keys, k)
	}

	keys.Sort()

	for _, k := range keys {
		log.Printf("%s=%s\n", k, cfg.Env()[k])
	}

	return nil
}

func ConfigSet(configStore *config.Store, app, env string, envVars []string) error {

	args := c.Args().Tail()
	if len(args) == 0 {
		bytes, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			log.Fatalf("ERROR: Unable to read stdin: %s.", err)
			return

		}
		args = strings.Split(string(bytes), "\n")
	}

	if len(args) == 0 {
		log.Fatalf("ERROR: No config values specified.")
		return
	}

	svcCfg, err := configStore.GetApp(app, utils.GalaxyEnv(c))
	if err != nil {
		log.Fatalf("ERROR: Unable to set config: %s.", err)
		return
	}

	if svcCfg == nil {
		svcCfg = gconfig.NewAppConfig(app, "")
	}

	updated := false
	for _, arg := range args {

		if strings.TrimSpace(arg) == "" {
			continue
		}

		if !strings.Contains(arg, "=") {
			log.Fatalf("ERROR: bad config variable format: %s", arg)
			cli.ShowCommandHelp(c, "config")
			return

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
		log.Errorf("Configuration NOT changed for %s", app)
		return
	}

	updated, err = configStore.UpdateApp(svcCfg, utils.GalaxyEnv(c))
	if err != nil {
		log.Fatalf("ERROR: Unable to set config: %s.", err)
		return
	}

	if !updated {
		log.Errorf("Configuration NOT changed for %s", app)
		return
	}
	log.Printf("Configuration changed for %s. v%d\n", app, svcCfg.ID())
}
