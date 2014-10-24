package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/codegangsta/cli"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/utils"
)

type backupData struct {
	Time time.Time
	Apps []*appCfg
}

// Serialized backup format
type appCfg struct {
	Name    string
	Version string
	Env     map[string]string
	Ports   map[string]string
}

// Backup app config to a file or STDOUT
func appBackup(c *cli.Context) {
	initRegistry(c)

	env := utils.GalaxyEnv(c)
	if env == "" {
		log.Fatal("ERROR: env is required.  Pass --env or set GALAXY_ENV")
	}

	backup := &backupData{
		Time: time.Now(),
	}

	toBackup := c.Args()

	if len(toBackup) == 0 {
		appList, err := serviceRegistry.ListApps(env)
		if err != nil {
			log.Fatalf("ERROR: %s\n", err)
		}

		for _, app := range appList {
			toBackup = append(toBackup, app.Name)
		}
	}

	errCount := 0
	for _, app := range toBackup {
		data, err := getAppBackup(app, env)
		if err != nil {
			// log errors and continue
			log.Errorf("ERROR: %s [%s]", err, app)
			errCount++
			continue
		}
		backup.Apps = append(backup.Apps, data)
	}

	if errCount > 0 {
		fmt.Printf("WARNING: backup completed with %d errors\n", errCount)
		defer os.Exit(errCount)
	}

	j, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		log.Fatal(err)
	}

	fileName := c.String("file")
	if fileName != "" {
		if err := ioutil.WriteFile(fileName, j, 0666); err != nil {
			log.Fatal(err)
		}
		return
	}

	os.Stdout.Write(j)
}

func getAppBackup(app, env string) (*appCfg, error) {
	svcCfg, err := serviceRegistry.GetServiceConfig(app, env)
	if err != nil {
		return nil, err
	}

	if svcCfg == nil {
		return nil, fmt.Errorf("app not found")
	}

	backup := &appCfg{
		Name:    app,
		Version: svcCfg.Version(),
		Env:     svcCfg.Env(),
		Ports:   svcCfg.Ports(),
	}
	return backup, nil
}

// restore an app's config from backup
func appRestore(c *cli.Context) {
	initRegistry(c)

	var err error
	var rawBackup []byte

	fileName := c.String("file")
	if fileName != "" {
		rawBackup, err = ioutil.ReadFile(fileName)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		log.Println("Reading backup from STDIN")
		rawBackup, err = ioutil.ReadAll(os.Stdin)
		if err != nil {
			log.Fatal(err)
		}
	}

	backup := &backupData{}
	if err := json.Unmarshal(rawBackup, backup); err != nil {
		log.Fatal(err)
	}

	fmt.Println("Found backup from ", backup.Time)

	var toRestore []*appCfg

	if apps := c.Args(); len(apps) > 0 {
		for _, app := range apps {
			found := false
			for _, bkup := range backup.Apps {
				if bkup.Name == app {
					toRestore = append(toRestore, bkup)
					found = true
					break
				}
			}
			if !found {
				log.Fatalf("no backup found for '%s'\n", app)
			}

		}
	} else {
		toRestore = backup.Apps
	}

	// check for conflicts
	// NOTE: there is still a race here if an app is created after this check
	if !c.Bool("force") {
		needForce := false
		for _, bkup := range toRestore {
			exists, err := serviceRegistry.AppExists(bkup.Name, utils.GalaxyEnv(c))
			if err != nil {
				log.Fatal(err)
			}
			if exists {
				log.Warnf("Cannot restore over existing app '%s'", bkup.Name)
				needForce = true
			}
		}
		if needForce {
			log.Fatal("Use -force to overwrite")
		}
	}

	loggedErr := false
	for _, bkup := range toRestore {
		if err := restoreApp(bkup, utils.GalaxyEnv(c)); err != nil {
			log.Errorf("%s", err)
			loggedErr = true
		}
	}

	if loggedErr {
		// This is mostly to give a non-zero exit status
		log.Fatal("Error occured during restore")
	}
}

func restoreApp(bkup *appCfg, env string) error {
	fmt.Println("restoring", bkup.Name)

	svcCfg, err := serviceRegistry.GetServiceConfig(bkup.Name, env)
	if err != nil {
		return err
	}

	if svcCfg == nil {
		svcCfg = registry.NewServiceConfig(bkup.Name, bkup.Version)
	}

	for port, net := range bkup.Ports {
		svcCfg.AddPort(port, net)
	}

	for k, v := range bkup.Env {
		svcCfg.EnvSet(k, v)
	}

	_, err = serviceRegistry.SetServiceConfig(svcCfg, env)
	return err
}
