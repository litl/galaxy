package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/iron-io/iron_go/mq"
)

type IronMQStat struct {
}

func (s *IronMQStat) Load(prefix string, tsc *TSCollection) error {
	for _, entry := range ironmqFlag {
		parts := strings.Split(entry, ":")
		env := parts[0]
		projectId := parts[1]
		token := parts[2]

		queues, err := mq.ListProjectQueues(projectId, token, 0, 100)
		if err != nil {
			fmt.Printf("ERR: %s", err)
			return err
		}

		for _, element := range queues {

			info, err := element.Info()
			if err != nil {
				fmt.Printf("ERR: %s", err)
				continue
			}

			s.add(tsc, prefix, env, element.Name, "Size", info.Size)
			s.add(tsc, prefix, env, element.Name, "Reserved", info.Reserved)
			s.add(tsc, prefix, env, element.Name, "Retries", info.Retries)
			s.add(tsc, prefix, env, element.Name, "TotalMessages", info.TotalMessages)
		}
	}
	return nil
}

func (s *IronMQStat) add(tsc *TSCollection, prefix, env, dimension, metric string, value int) {
	now := time.Now().UTC()
	key := fmt.Sprintf("%s.%s.%s.%s.%s.%s", env, prefix, "ironio", "mq", dimension, metric)
	ts := tsc.Get(key)
	ts.Add(now.Unix()-now.Unix()%60, float64(value))
	//secs := now.Unix() % 60
	//ts.Fill(now.Add(-4*time.Hour).Unix()-secs, now.Unix()-secs, 60, 0)
}
