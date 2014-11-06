package main

import (
	"strings"
	"time"

	"github.com/codegangsta/cli"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/utils"
	"github.com/ryanuber/columnize"
)

func unregister(c *cli.Context) {

	initOrDie(c)

	outputBuffer.Log(strings.Join([]string{
		"CONTAINER ID", "IMAGE",
		"EXTERNAL", "INTERNAL", "CREATED", "EXPIRES",
	}, " | "))

	unregisterShuttle(c)
	containers, err := serviceRuntime.UnRegisterAll(utils.GalaxyEnv(c), utils.GalaxyPool(c),
		c.GlobalString("hostIp"))
	if err != nil {
		log.Fatalf("ERROR: Problem unregistering containers: %s", err)
	}

	for _, container := range containers {
		//FIXME: This needs to be moved out of here
		statusLine := strings.Join([]string{
			container.ID[0:12],
			container.Config.Image,
			"",
			"",
			utils.HumanDuration(time.Now().Sub(container.Created)) + " ago",
			"",
		}, " | ")

		outputBuffer.Log(statusLine)
	}

	result, _ := columnize.SimpleFormat(outputBuffer.Output)
	log.Println(result)
}
