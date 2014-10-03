package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/iron-io/iron_go/mq"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/stack"
)

type IronMQStat struct {
}

func loadIronMQStats(tscChan chan *TSCollection) {
	defer wg.Done()
	if len(ironmqFlag) == 0 {
		log.Debugf("No ironmq credentials found. Skipping collection.")
		return
	}

	stat := IronMQStat{}
	for {

		names, err := stack.ListActive()
		if err != nil {
			time.Sleep(10 * time.Second)
			continue
		}

		for _, i := range names {
			parts := strings.Split(i, "-")
			if len(parts) != 3 {
				continue
			}

			source := parts[0]
			stat.Load(tscChan, source)
		}

		time.Sleep(30 * time.Second)
	}
}

func (s *IronMQStat) Load(tscChan chan *TSCollection, source string) error {
	for _, entry := range ironmqFlag {
		projectEnv, projectId, token := s.splitFlagOption(entry)

		if projectEnv != env {
			continue
		}

		queues, err := mq.ListProjectQueues(projectId, token, 0, 500)
		if err != nil {
			fmt.Printf("ERR: %s", err)
			return err
		}

		for _, element := range queues {

			tsc := NewTSCollection()
			now := time.Now().UTC()
			info, err := element.Info()
			if err != nil {
				fmt.Printf("ERR: %s", err)
				continue
			}

			attr := map[string]interface{}{
				"env":       env,
				"source":    source,
				"projectId": projectId,
				"provider":  "ironio",
				"component": "mq",
				"queue":     element.Name,
			}

			ts := tsc.Get(s.key("Size"))
			ts.Add(now.Unix()-now.Unix()%60, float64(info.Size), attr)

			ts = tsc.Get(s.key("Reserved"))
			ts.Add(now.Unix()-now.Unix()%60, float64(info.Reserved), attr)

			ts = tsc.Get(s.key("Retries"))
			ts.Add(now.Unix()-now.Unix()%60, float64(info.Retries), attr)

			ts = tsc.Get(s.key("TotalMessages"))
			ts.Add(now.Unix()-now.Unix()%60, float64(info.TotalMessages), attr)

			tscChan <- tsc
		}
	}
	return nil
}

func (s *IronMQStat) key(metric string) string {
	return fmt.Sprintf("%s.%s.%s", "ironio", "mq", metric)
}

func (s *IronMQStat) splitFlagOption(flag string) (string, string, string) {
	parts := strings.Split(flag, ":")
	return parts[0], parts[1], parts[2]
}
