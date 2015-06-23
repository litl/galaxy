package config

import (
	"fmt"
	"log"
	"strings"
	"time"
)

type ConfigChange struct {
	AppConfig App
	Restart   bool
	Error     error
}

func (s *Store) CheckForChangesNow() {
	s.pollCh <- true
}

func (s *Store) checkForChanges(env string) {
	lastVersion := make(map[string]int64)
	for {
		appCfg, err := s.ListApps(env)
		if err != nil {
			s.restartChan <- &ConfigChange{
				Error: err,
			}
			time.Sleep(5 * time.Second)
			continue
		}

		for _, config := range appCfg {
			lastVersion[config.Name()] = config.ID()
		}
		break

	}

	for {
		<-s.pollCh
		appCfg, err := s.ListApps(env)
		if err != nil {
			s.restartChan <- &ConfigChange{
				Error: err,
			}
			continue
		}
		for _, changedConfig := range appCfg {
			changeCopy := changedConfig
			if changedConfig.ID() != lastVersion[changedConfig.Name()] {
				log.Printf("%s changed from %d to %d", changedConfig.Name(),
					lastVersion[changedConfig.Name()], changedConfig.ID())
				lastVersion[changedConfig.Name()] = changedConfig.ID()
				s.restartChan <- &ConfigChange{
					AppConfig: changeCopy,
				}
			}
		}
	}
}

func (s *Store) checkForChangePeriodically(stop chan struct{}) {
	// TODO: default polling interval
	ticker := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-stop:
			ticker.Stop()
			return
		case <-ticker.C:
			s.CheckForChangesNow()
		}
	}
}

func (s *Store) restartApp(app, env string) {
	appCfg, err := s.GetApp(app, env)
	if err != nil {
		s.restartChan <- &ConfigChange{
			Error: err,
		}
		return
	}

	s.restartChan <- &ConfigChange{
		Restart:   true,
		AppConfig: appCfg,
	}
}

func (s *Store) NotifyRestart(app, env string) error {
	// TODO: received count ignored, use it somehow?
	_, err := s.Backend.Notify(fmt.Sprintf("galaxy-%s", env), fmt.Sprintf("restart %s", app))
	if err != nil {
		return err
	}
	return nil
}

func (s *Store) NotifyEnvChanged(env string) error {
	// TODO: received count ignored, use it somehow?
	_, err := s.Backend.Notify(fmt.Sprintf("galaxy-%s", env), "config")
	if err != nil {
		return err
	}
	return nil
}

func (s *Store) subscribeChanges(env string) {

	msgs := s.Backend.Subscribe(fmt.Sprintf("galaxy-%s", env))
	for {

		msg := <-msgs
		if msg == "config" {
			s.CheckForChangesNow()
		} else if strings.HasPrefix(msg, "restart") {
			parts := strings.Split(msg, " ")
			app := parts[1]
			s.restartApp(app, env)
		} else {
			log.Printf("Ignoring notification: %s\n", msg)
		}
	}
}

func (s *Store) Watch(env string, stop chan struct{}) chan *ConfigChange {
	go s.checkForChanges(env)
	go s.checkForChangePeriodically(stop)
	go s.subscribeChanges(env)
	return s.restartChan
}
