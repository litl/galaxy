package main

import (
	"github.com/litl/galaxy/discovery/command"
	"github.com/litl/galaxy/utils"
	"github.com/mitchellh/cli"
	"os"
)

var Commands map[string]cli.CommandFactory

func init() {
	ui := &cli.BasicUi{Writer: os.Stdout}

	Commands = map[string]cli.CommandFactory{

		"register": func() (cli.Command, error) {
			return &command.RegisterCommand{
				Ui:           ui,
				Client:       client,
				Hostname:     hostname,
				OutputBuffer: &utils.OutputBuffer{},
			}, nil
		},
		"status": func() (cli.Command, error) {
			return &command.StatusCommand{
				Ui:           ui,
				Client:       client,
				Hostname:     hostname,
				OutputBuffer: &utils.OutputBuffer{},
			}, nil
		},
	}
}
