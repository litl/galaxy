package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/iron-io/iron_go/mq"
	"github.com/litl/galaxy/log"
)

type IronMQStat struct {
}

func loadIronMQStats(statChan chan []Stat) {
	log.Debug("Checking IronMQ...")
	defer func() {
		log.Debugf("Done checking IronMQ")
	}()

	if len(ironmqFlag) == 0 {
		log.Debugf("No ironmq credentials found. Skipping collection permanently.")
		return
	}

	// We can only get current stats from iron.io.
	// Fetch new info every minute.
	ticker := time.NewTicker(60 * time.Second)

	for {
		stats, err := GetIronMQStats()
		if err != nil {
			log.Printf("ERR: %s", err)
			continue
		}
		statChan <- stats

		<-ticker.C
	}
}

func GetIronMQStats() ([]Stat, error) {
	var stats []Stat

	for _, ironmqProj := range ironmqFlag {
		proj := strings.Split(ironmqProj, ":")
		if len(proj) != 3 {
			fmt.Printf("ERR: malformed IronMQ Flag '%s'", ironmqProj)
			continue
		}
		projectEnv, projectId, token := proj[0], proj[1], proj[2]

		queues, err := mq.ListProjectQueues(projectId, token, 0, 500)
		if err != nil {
			fmt.Printf("ERR: %s", err)
			return nil, err
		}
		log.Debugf("ironmq: queues %+v", queues)

		for _, queue := range queues {
			now := time.Now().UTC()

			info, err := queue.Info()
			if err != nil {
				fmt.Printf("ERR: %s", err)
				continue
			}

			log.Debugf("got queue info: %+v", info)

			prefix := strings.Join([]string{projectEnv, "ironmq", queue.Name}, ".") + "."

			stats = append(stats, Stat{Path: prefix + "size", Value: float64(info.Size), TS: now})
			stats = append(stats, Stat{Path: prefix + "reserved", Value: float64(info.Reserved), TS: now})
			stats = append(stats, Stat{Path: prefix + "retries", Value: float64(info.Retries), TS: now})
			stats = append(stats, Stat{Path: prefix + "total", Value: float64(info.TotalMessages), TS: now})
		}
	}
	return stats, nil
}
