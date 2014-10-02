package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

type graphiteResponse struct {
	Target     string      "json:'target'"
	Datapoints [][]float64 "json:'datapoints'"
}

func loadGraphite(tsc *TSCollection, from, to int64, names []string) {
	targets := []string{}
	for _, name := range names {
		targets = append(targets, fmt.Sprintf("target=summarize(%s,'1min')", name))
	}

	url := fmt.Sprintf("http://%s/render?%s&from=%d&to=%d&format=json", graphiteAddr,
		strings.Join(targets, "&"), from, to)
	req, err := http.NewRequest("GET",
		url, nil)
	if err != nil {
		log.Printf("ERROR: %s", err)
		return
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("ERROR: %s", err)
		return
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("ERROR: %s", err)
		return
	}

	var data []*graphiteResponse
	err = json.Unmarshal(body, &data)
	if err != nil {
		log.Printf("ERROR: %s", err)
		return
	}

	for i, g := range data {
		ts := tsc.Get(names[i])
		for _, d := range g.Datapoints {
			ts.Add(int64(d[1]), d[0])
		}
	}
	return
}

func storeGraphite(tsc *TSCollection) {
	defer wg.Done()

	lastSent := int64(0)
	for {
		now := time.Now().UTC().Unix()
		values := []string{}
		for name, ts := range tsc.series {
			for _, value := range ts.Filter(lastSent-5*60, now) {
				values = append(values, fmt.Sprintf("%s %f %d", name, value.Value, value.TS))
			}
		}

		if len(values) > 0 {
			conn, err := net.DialTimeout("tcp", graphiteCarbonAddr, 60*time.Second)
			if err != nil {
				log.Printf("ERROR: %s", err)
				return
			}
			defer conn.Close()
			_, err = conn.Write([]byte(strings.Join(values, "\n")))
			if err != nil {
				log.Printf("ERROR: %s", err)
				return

			}
			conn.Close()
			lastSent = now
		}

		time.Sleep(10 * time.Second)
	}
}
