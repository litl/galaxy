package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"sync"

	"github.com/litl/galaxy/log"
)

func loadConfig() {
	for _, cfgPath := range []string{stateConfig, defaultConfig} {
		if cfgPath == "" {
			continue
		}

		cfgData, err := ioutil.ReadFile(cfgPath)
		if err != nil {
			log.Warnln("Error reading config:", err)
			continue
		}

		var svcs []ServiceConfig
		err = json.Unmarshal(cfgData, &svcs)
		if err != nil {
			log.Warnln("Config error:", err)
			continue
		}
		log.Debug("Loaded config from:", cfgPath)

		for _, svcCfg := range svcs {
			if e := Registry.AddService(svcCfg); e != nil {
				log.Println("Unabled to add service %s: error:", svcCfg.Name, e)
			}
		}
	}
}

// protects the state config file
var configMutex sync.Mutex

func writeStateConfig() {
	configMutex.Lock()
	defer configMutex.Unlock()

	if stateConfig == "" {
		log.Println("No state file. Not saving changes")
		return
	}

	cfg := marshal(Registry.Config())
	if len(cfg) == 0 {
		return
	}

	lastCfg, _ := ioutil.ReadFile(stateConfig)
	if bytes.Equal(cfg, lastCfg) {
		log.Println("No change in config")
		return
	}

	// We should probably write a temp file and mv for atomic update.
	err := ioutil.WriteFile(stateConfig, cfg, 0644)
	if err != nil {
		log.Println("Error saving config state:", err)
	}
}
