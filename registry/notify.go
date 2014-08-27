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

func (r *ServiceRegistry) checkForChanges() {
	lastVersion := make(map[string]int64)
	for {
		serviceConfigs, err := r.ListApps()
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
		serviceConfigs, err := r.ListApps()
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

func (r *ServiceRegistry) restartApp(app string) {
	serviceConfig, err := r.GetServiceConfig(app)
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

func (r *ServiceRegistry) NotifyRestart(app string) error {
	// TODO: received count ignored, use it somehow?
	_, err := r.backend.Notify("galaxy", fmt.Sprintf("restart %s", app))
	if err != nil {
		return err
	}
	return nil
}

func (r *ServiceRegistry) notifyChanged() error {
	// TODO: received count ignored, use it somehow?
	_, err := r.backend.Notify("galaxy", "config")
	if err != nil {
		return err
	}
	return nil
}

func (r *ServiceRegistry) subscribeChanges() {

	msgs := r.backend.Subscribe("galaxy")
	for {

		msg := <-msgs
		if msg == "config" {
			r.CheckForChangesNow()
		} else if strings.HasPrefix(msg, "restart") {
			parts := strings.Split(msg, " ")
			app := parts[1]
			r.restartApp(app)
		} else {
			log.Printf("Ignoring notification: %s\n", msg)
		}
	}
}

func (r *ServiceRegistry) Watch(stop chan struct{}) chan *ConfigChange {
	restartChan = make(chan *ConfigChange, 10)
	go r.checkForChanges()
	go r.checkForChangePeriodically(stop)
	go r.subscribeChanges()
	return restartChan
}
