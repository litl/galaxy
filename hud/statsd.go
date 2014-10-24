package main

import (
	"bytes"

	"github.com/litl/galaxy/log"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var priorValue *TSCollection

func StatsdListener(tscChan chan *TSCollection) {
	defer wg.Done()
	priorValue = NewTSCollection()
	address, _ := net.ResolveUDPAddr("udp", statsdAddr)
	listener, err := net.ListenUDP("udp", address)
	if err != nil {
		log.Fatalf("ERROR: Error starting statsd listener: %s", err)
	}
	defer listener.Close()

	log.Printf("Listening for statsd at: %s", address.String())
	for {
		message := make([]byte, 512)
		n, remaddr, error := listener.ReadFrom(message)
		if error != nil {
			continue
		}
		buf := bytes.NewBuffer(message[0:n])

		log.Debugf("%s", "Packet received: "+string(message[0:n]))

		go handleMessage(listener, remaddr, buf, tscChan)
	}
}

func handleMessage(conn *net.UDPConn, remaddr net.Addr, buf *bytes.Buffer, tscChan chan *TSCollection) {

	var sanitizeRegexp = regexp.MustCompile("[^a-zA-Z0-9\\-_\\+\\.:\\|@]")
	var packetRegexp = regexp.MustCompile("([a-zA-Z0-9_\\.]+):([\\-\\+]?[0-9\\.]+)\\|(c|ms|g)(\\|@([0-9\\.]+))?")
	s := sanitizeRegexp.ReplaceAllString(buf.String(), "")
	tsc := NewTSCollection()
	ts := time.Now().UTC().Unix()
	for _, item := range packetRegexp.FindAllStringSubmatch(s, -1) {
		bucket := item[1]
		value := item[2]
		metricType := item[3]

		if item[3] == "ms" {
			_, err := strconv.ParseFloat(item[2], 32)
			if err != nil {
				value = "0"
			}
		}

		sampleRate, err := strconv.ParseFloat(item[5], 32)
		if err != nil {
			sampleRate = 1
		}

		floatValue, _ := strconv.ParseFloat(value, 64)

		if metricType == "g" && priorValue != nil && (strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-")) {
			intValue, _ := strconv.Atoi(value)
			floatValue = priorValue.Get(bucket).Current().Value + float64(intValue)
		}
		tsc.Get(bucket).Add(ts, floatValue, map[string]interface{}{})
		priorValue.Get(bucket).Add(ts, floatValue, map[string]interface{}{})
		priorValue.Get(bucket).RemoveBefore(ts - 60*2)

		log.Debugf("Packet: bucket = %s, value = %s, modifier = %s, sampling = %f",
			bucket, value, metricType, sampleRate)

	}
	tscChan <- tsc
}
