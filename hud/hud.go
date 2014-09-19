package main

import (
	"flag"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/stack"
)

var (
	debug              bool
	version            bool
	buildVersion       string
	stats              *TSCollection
	ironmqFlag         sliceVar
	graphiteCarbonAddr string
	graphiteAddr       string
	httpClient         *http.Client
	wg                 sync.WaitGroup
)

type sliceVar []string

type Page struct {
	Templates template.HTML
}

func (s *sliceVar) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func (s *sliceVar) String() string {
	return strings.Join(*s, ",")
}

func loadCloudwatchStats() {

	defer wg.Done()
	for {

		names, err := stack.ListActive()
		if err != nil {
			log.Errorf("ERROR: %s\n", err)
			time.Sleep(10 * time.Second)
			continue
		}

		for _, i := range names {

			rs, err := stack.ListStackResources(i)
			if err != nil {
				log.Errorf("ERROR: %s\n", err)
				continue
			}

			var elbName string
			for _, i := range rs.Resources {
				if i.Type == "AWS::ElasticLoadBalancing::LoadBalancer" {
					elbName = i.PhysicalId
				}
			}

			if elbName == "" {
				log.Debugf("Could not lookup ELB name for %s. Skipping.", i)
				continue
			}

			parts := strings.Split(i, "-")
			if len(parts) != 3 {
				log.Debugf("ELB %s. Does not appear to be related to galaxy. Skipping.", elbName)
				continue
			}

			source := parts[0]
			env := parts[1]

			for _, metric := range []string{"RequestCount", "HTTPCode_Backend_2XX",
				"HTTPCode_Backend_3XX", "HTTPCode_Backend_4XX",
				"HTTPCode_Backend_5XX", "HTTPCode_ELB_4XX", "HTTPCode_ELB_5XX", "Latency"} {
				cwStat := CloudwatchStat{
					Namespace: "AWS/ELB",
					Dimensions: map[string]string{
						"LoadBalancerName": elbName,
					},
					MetricName: metric,
					Statistic:  "Sum",
				}

				if metric == "Latency" {
					cwStat.Statistic = "Average"
				}

				prefix := fmt.Sprintf("%s.%s", env, source)
				err := cwStat.Load(prefix, stats)
				if err != nil {
					log.Errorf("ERROR: %s\n", err)
					continue
				}
			}
		}
		time.Sleep(60 * time.Second)
	}
}

func loadIronMQStats() {

	defer wg.Done()
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
			stat.Load(source, stats)
		}

		time.Sleep(30 * time.Second)
	}
}

func main() {
	flag.BoolVar(&debug, "debug", false, "Enables debug build")
	flag.BoolVar(&version, "v", false, "display version info")

	flag.Var(&ironmqFlag, "ironmq", "env:project_id:token")
	flag.StringVar(&graphiteCarbonAddr, "graphite-carbon-addr", "localhost:2003", "Graphite carbon host:port")
	flag.StringVar(&graphiteAddr, "graphite-addr", "localhost", "Graphite host:port")

	flag.Parse()

	if version {
		fmt.Println(buildVersion)
		return
	}

	if debug {
		log.DefaultLogger.Level = log.DEBUG
	}

	transport := &http.Transport{ResponseHeaderTimeout: 10 * time.Second}
	httpClient = &http.Client{Transport: transport}

	stats = NewTSCollection()
	wg.Add(3)
	go loadCloudwatchStats()
	go loadIronMQStats()
	go storeGraphite(stats)
	wg.Wait()

}
