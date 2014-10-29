package config

import (
	"fmt"
	"log"
	"strings"
	"time"
)

type ConfigChange struct {
	AppConfig *AppConfig
	Restart   bool
	Error     error
}

var restartChan chan *ConfigChange

func (r *Store) CheckForChangesNow() {
	r.pollCh <- true
}

func (r *Store) checkForChanges(env string) {
	lastVersion := make(map[string]int64)
	for {
		serviceConfigs, err := r.ListApps(env)
		if err != nil {
			restartChan <- &ConfigChange{
				Error: err,
			}
			time.Sleep(5 * time.Second)
			continue
		}

		for _, config := range serviceConfigs {
			lastVersion[config.Name] = config.ID()
		}
		break

	}

	for {
		<-r.pollCh
		serviceConfigs, err := r.ListApps(env)
		if err != nil {
			restartChan <- &ConfigChange{
				Error: err,
			}
			continue
		}
		for _, changedConfig := range serviceConfigs {
			changeCopy := changedConfig
			if changedConfig.ID() != lastVersion[changedConfig.Name] {
				log.Printf("%s changed from %d to %d", changedConfig.Name,
					lastVersion[changedConfig.Name], changedConfig.ID())
				lastVersion[changedConfig.Name] = changedConfig.ID()
				restartChan <- &ConfigChange{
					AppConfig: &changeCopy,
				}
			}
		}
	}
}

func (r *Store) checkForChangePeriodically(stop chan struct{}) {
	// TODO: default polling interval
	ticker := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-stop:
			ticker.Stop()
			return
		case <-ticker.C:
			r.CheckForChangesNow()
		}
	}
}

func (r *Store) restartApp(app, env string) {
	serviceConfig, err := r.GetApp(app, env)
	if err != nil {
		restartChan <- &ConfigChange{
			Error: err,
		}
		return
	}

	restartChan <- &ConfigChange{
		Restart:   true,
		AppConfig: serviceConfig,
	}
}

func (r *Store) NotifyRestart(app, env string) error {
	// TODO: received count ignored, use it somehow?
	_, err := r.backend.Notify(fmt.Sprintf("galaxy-%s", env), fmt.Sprintf("restart %s", app))
	if err != nil {
		return err
	}
	return nil
}

func (r *Store) NotifyEnvChanged(env string) error {
	// TODO: received count ignored, use it somehow?
	_, err := r.backend.Notify(fmt.Sprintf("galaxy-%s", env), "config")
	if err != nil {
		return err
	}
	return nil
}

func (r *Store) subscribeChanges(env string) {

	msgs := r.backend.Subscribe(fmt.Sprintf("galaxy-%s", env))
	for {

		msg := <-msgs
		if msg == "config" {
			r.CheckForChangesNow()
		} else if strings.HasPrefix(msg, "restart") {
			parts := strings.Split(msg, " ")
			app := parts[1]
			r.restartApp(app, env)
		} else {
			log.Printf("Ignoring notification: %s\n", msg)
		}
	}
}

func (r *Store) Watch(env string, stop chan struct{}) chan *ConfigChange {
	restartChan = make(chan *ConfigChange, 10)
	go r.checkForChanges(env)
	go r.checkForChangePeriodically(stop)
	go r.subscribeChanges(env)
	return restartChan
}
