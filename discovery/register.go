package main

import (
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/codegangsta/cli"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/utils"
	"github.com/ryanuber/columnize"
)

func unregisterAll(c *cli.Context, signals chan os.Signal) {
	// Block until a signal is received.
	<-signals
	unregisterShuttle(c)
	serviceRuntime.UnRegisterAll()
	os.Exit(0)
}

func register(c *cli.Context) {

	initOrDie(c)
	var lastLogged int64

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, os.Kill)
	go unregisterAll(c, signals)

	for {

		outputBuffer.Log(strings.Join([]string{
			"CONTAINER ID", "IMAGE",
			"EXTERNAL", "INTERNAL", "CREATED", "EXPIRES",
		}, " | "))

		registrations, err := serviceRuntime.RegisterAll()
		if err != nil {
			log.Fatalf("ERROR: Unable to register containers: %s", err)
		}

		for _, registration := range registrations {
			if lastLogged == 0 || time.Now().UnixNano()-lastLogged > (60*time.Second).Nanoseconds() {
				location := registration.ExternalAddr()
				if location != "" {
					location = " at " + location
				}
				log.Printf("Registered %s running as %s for %s%s", strings.TrimPrefix(registration.ContainerName, "/"),
					registration.ContainerID[0:12], registration.Name, location)
			}

			statusLine := strings.Join([]string{
				registration.ContainerID[0:12],
				registration.Image,
				registration.ExternalAddr(),
				registration.InternalAddr(),
				utils.HumanDuration(time.Now().Sub(registration.StartedAt)) + " ago",
				"In " + utils.HumanDuration(registration.Expires.Sub(time.Now().UTC())),
			}, " | ")

			outputBuffer.Log(statusLine)
		}

		registerShuttle(c)

		if !c.Bool("loop") {
			break
		}
		time.Sleep(10 * time.Second)

	}

	result, _ := columnize.SimpleFormat(outputBuffer.Output)
	log.Println(result)

}
