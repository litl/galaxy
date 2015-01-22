package main

import (
	"strings"
	"sync"
	"time"

	"github.com/goamz/goamz/aws"
	"github.com/goamz/goamz/cloudwatch"
	"github.com/goamz/goamz/rds"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/stack"
)

type CloudwatchStat struct {
	Namespace  string
	Dimensions map[string]string
	MetricName string
	Statistic  string
	Component  string
}

var ELBMetrics = []string{
	"RequestCount", "HTTPCode_Backend_2XX", "HTTPCode_Backend_3XX",
	"HTTPCode_Backend_4XX", "HTTPCode_Backend_5XX", "HTTPCode_ELB_4XX",
	"HTTPCode_ELB_5XX", "Latency", "HealthyHostCount", "UnHealthyHostCount",
	"BackendConnectionErrors", "SurgeQueueLength", "SpilloverCount",
}

var RDSMetrics = []string{
	"BinLogDiskUsage", "CPUUtilization", "DatabaseConnections",
	"DiskQueueDepth", "FreeableMemory", "FreeStorageSpace", "ReplicaLag",
	"SwapUsage", "ReadIOPS", "WriteIOPS", "ReadLatency", "WriteLatency",
	"ReadThroughput", "WriteThroughput", "NetworkReceiveThroughput",
	"NetworkTransmitThroughput",
}

func loadELBStats(auth aws.Auth, statChan chan []Stat, done *sync.WaitGroup) {
	log.Debugf("Checking ELB...")
	defer func() {
		log.Debugf("Done checking ELB")
		done.Done()
	}()

	names, err := stack.ListActive()
	if err != nil {
		log.Errorf("ERROR: %s\n", err)
		return
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
		resourceEnv := parts[1]

		for _, metric := range ELBMetrics {
			cwStat := CloudwatchStat{
				Namespace: "AWS/ELB",
				Dimensions: map[string]string{
					"LoadBalancerName": elbName,
				},
				MetricName: metric,
				Statistic:  "Sum",
				Component:  "elb",
			}

			if metric == "SurgeQueueLength" {
				cwStat.Statistic = "Maximum"
			}
			if metric == "Latency" || metric == "HealthyHostCount" || metric == "UnHealthyHostCount" {
				cwStat.Statistic = "Average"
			}

			datapoints, err := cwStat.Get()
			if err != nil {
				log.Errorf("ERROR: %s\n", err)
				continue
			}

			var stats []Stat
			prefix := strings.Join([]string{resourceEnv, "aws", "elb", elbName, metric}, ".")
			for _, metric := range datapoints {
				stats = append(stats, Stat{Path: prefix, Value: metric.Average, TS: metric.Timestamp})
			}

			statChan <- stats
		}
	}
}

func loadRDSStats(auth aws.Auth, statChan chan []Stat, done *sync.WaitGroup) {

	log.Debugf("Checking RDS...")
	defer func() {
		log.Debugf("Done checking RDS")
		done.Done()
	}()

	reg, err := stack.GetAWSRegion("")
	if err != nil {
		log.Errorf("ERROR: %s", err)
		return
	}

	rdsClient, err := rds.New(auth, *reg)
	if err != nil {
		log.Errorf("ERROR: %s", err)
		return
	}

	resp, err := rdsClient.DescribeDBInstances("", 0, "")
	if err != nil {
		log.Errorf("ERROR: %s", err)
		return
	}

	instanceIds := []string{}
	for _, inst := range resp.DBInstances {
		instanceIds = append(instanceIds, inst.DBInstanceIdentifier)
	}

	for _, dbInstance := range instanceIds {
		log.Debugf("fetching stats for RDS: %s", dbInstance)

		for _, metric := range RDSMetrics {
			cwStat := CloudwatchStat{
				Namespace: "AWS/RDS",
				Dimensions: map[string]string{
					"DBInstanceIdentifier": dbInstance,
				},
				MetricName: metric,
				Statistic:  "Average",
				Component:  "rds",
			}

			datapoints, err := cwStat.Get()
			if err != nil {
				log.Errorf("ERROR: %s\n", err)
				continue
			}

			var stats []Stat
			prefix := strings.Join([]string{dbInstance, "aws", "rds", metric}, ".")
			for _, metric := range datapoints {
				stats = append(stats, Stat{Path: prefix, Value: metric.Average, TS: metric.Timestamp})
			}

			statChan <- stats
		}
	}
}

func loadCloudwatchStats(statChan chan []Stat) {
	pollWg := sync.WaitGroup{}

	ticker := time.NewTicker(5 * time.Minute)
	for {
		auth, err := aws.GetAuth("", "", "", time.Now().UTC())
		if err != nil {
			log.Debugf("%s. Skipping collection.", err)
			time.Sleep(60 * time.Second)
			continue
		}

		log.Debugf("Checking cloudwatch...")
		pollWg.Add(1)
		go loadELBStats(auth, statChan, &pollWg)

		pollWg.Add(1)
		go loadRDSStats(auth, statChan, &pollWg)

		pollWg.Wait()
		<-ticker.C
	}
}

// This fetches the actual stats from Cloudwatch and return a []Stat for feeding into graphite
func (c *CloudwatchStat) Get() ([]cloudwatch.Datapoint, error) {
	auth, err := aws.GetAuth("", "", "", time.Now())
	if err != nil {
		return nil, err
	}

	cw, err := cloudwatch.NewCloudWatch(auth, aws.USEast.CloudWatchServicepoint)
	if err != nil {
		return nil, err
	}

	dimensions := []cloudwatch.Dimension{}
	for k, v := range c.Dimensions {
		dimensions = append(dimensions, cloudwatch.Dimension{
			Name:  k,
			Value: v,
		})
	}

	s, err := cw.GetMetricStatistics(&cloudwatch.GetMetricStatisticsRequest{
		Namespace:  c.Namespace,
		Dimensions: dimensions,
		MetricName: c.MetricName,
		StartTime:  time.Now().UTC().Add(-5 * time.Minute),
		EndTime:    time.Now().UTC(),
		Period:     60,
		Statistics: []string{c.Statistic},
	})
	if err != nil {
		return nil, err
	}

	return s.GetMetricStatisticsResult.Datapoints, nil
}
