package registry

import (
	"fmt"
	"log"
	"strings"
	"time"
)

var restartChan chan *ConfigChange

func (r *ServiceRegistry) CheckForChangesNow() {
	r.pollCh <- true
}

func (r *ServiceRegistry) checkForChanges(env string) {
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
					ServiceConfig: &changeCopy,
				}
			}
		}
	}
}

func (r *ServiceRegistry) checkForChangePeriodically(stop chan struct{}) {
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

func (r *ServiceRegistry) restartApp(app, env string) {
	serviceConfig, err := r.GetServiceConfig(app, env)
	if err != nil {
		restartChan <- &ConfigChange{
			Error: err,
		}
		return
	}

	restartChan <- &ConfigChange{
		Restart:       true,
		ServiceConfig: serviceConfig,
	}
}

func (r *ServiceRegistry) NotifyRestart(app, env string) error {
	// TODO: received count ignored, use it somehow?
	_, err := r.backend.Notify(fmt.Sprintf("galaxy-%s", env), fmt.Sprintf("restart %s", app))
	if err != nil {
		return err
	}
	return nil
}

func (r *ServiceRegistry) notifyChanged() error {
	// TODO: received count ignored, use it somehow?
	_, err := r.backend.Notify(fmt.Sprintf("galaxy-%s", r.Env), "config")
	if err != nil {
		return err
	}
	return nil
}

func (r *ServiceRegistry) subscribeChanges(env string) {

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

func (r *ServiceRegistry) Watch(env string, stop chan struct{}) chan *ConfigChange {
	restartChan = make(chan *ConfigChange, 10)
	go r.checkForChanges(env)
	go r.checkForChangePeriodically(stop)
	go r.subscribeChanges(env)
	return restartChan
}
