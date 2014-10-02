package main

import (
	"fmt"
	"time"

	"github.com/crowdmob/goamz/aws"
	"github.com/crowdmob/goamz/cloudwatch"
)

type CloudwatchStat struct {
	Namespace  string
	Dimensions map[string]string
	MetricName string
	Statistic  string
}

func (c *CloudwatchStat) Load(prefix string, tsc *TSCollection) error {
	ts := NewTimeSeries()

	auth, err := aws.EnvAuth()
	if err != nil {
		return err
	}

	cw, err := cloudwatch.NewCloudWatch(auth, aws.USEast.CloudWatchServicepoint)
	if err != nil {
		return err
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
		StartTime:  time.Now().UTC().Add(-4 * time.Hour),
		EndTime:    time.Now().UTC(),
		Period:     60,
		Statistics: []string{c.Statistic},
	})
	if err != nil {
		return err
	}

	for _, metric := range s.GetMetricStatisticsResult.Datapoints {
		if c.Statistic == "Sum" {
			ts.Add(metric.Timestamp.Unix(), metric.Sum)
		} else if c.Statistic == "Average" {
			ts.Add(metric.Timestamp.Unix(), metric.Average)
		}
	}

	//unixNow := time.Now().UTC()
	//secs := unixNow.Unix() % 60
	//ts.Fill(unixNow.Add(-4*time.Hour).Unix()-secs, unixNow.Unix()-secs, 60, 0)

	key := fmt.Sprintf("%s.%s.%s.%s", prefix, "aws", "elb", c.MetricName)
	tsc.Get(key).AddAll(ts)
	return nil
}
