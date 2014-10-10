package main

import (
	"github.com/litl/galaxy/log"
	"net/url"
	"strings"
	"time"

	influxClient "github.com/influxdb/influxdb/client"
)

func storeInfluxDB(tscChan chan *TSCollection) {

	defer wg.Done()

	var client *influxClient.Client = nil

	config := &influxClient.ClientConfig{}

	url, err := url.Parse(influxDbAddr)
	if err != nil {
		log.Fatalf("ERROR: %s", err)
	}

	if url.Scheme != "influxdb" {
		log.Fatalf("ERROR: Unknown influxdb protocol: %s", url.Scheme)
	}

	config.Host = url.Host
	if url.User != nil {
		config.Username = url.User.Username()
		pw, set := url.User.Password()
		if set {
			config.Password = pw
		}
	}

	config.Database = strings.TrimPrefix(url.Path, "/")
	client, err = influxClient.NewClient(config)

	if err != nil {
		log.Fatalf("ERROR: %s", err)
	}

	for {

	RETRY:
		err = client.Ping()
		if err != nil {
			log.Errorf("ERROR: %s", err)
			time.Sleep(10 * time.Second)
			goto RETRY

		}

		err = client.AuthenticateDatabaseUser(config.Database, config.Username, config.Password)
		if err != nil {
			log.Errorf("ERROR: Unable to connect to influxdb: %s", err)
			time.Sleep(10 * time.Second)
			goto RETRY
		}

		series := []*influxClient.Series{}

		tsc := <-tscChan

		for name, ts := range tsc.series {
			columns := []string{"time", "value"}

			attrNames := ts.AttrNames()
			columns = append(columns, attrNames...)

			name = env + "." + name
			if statsPrefix != "" {
				name = statsPrefix + "." + name
			}
			serie := &influxClient.Series{
				Name:    name,
				Columns: columns,
			}

			for _, value := range ts.Metrics() {
				dp := []interface{}{
					value.TS,
					value.Value,
				}
				for _, k := range attrNames {
					dp = append(dp, ts.attr[k])
				}

				serie.Points = append(serie.Points, dp)
			}
			series = append(series, serie)
		}

		if len(series) > 0 {
			err := client.WriteSeriesWithTimePrecision(series, influxClient.Second)
			if err != nil {
				log.Printf("ERROR: %s", err)
				time.Sleep(10 * time.Second)
				goto RETRY
			}
		}
		log.Debugf("Stored %d records in influxdb", len(series))
	}

}
