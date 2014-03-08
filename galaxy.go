package main

import (
	"encoding/json"
	"fmt"
	"github.com/codegangsta/cli"
	"github.com/coreos/go-etcd/etcd"
	"os"
	"strings"
)

const (
	ETCD_ENTRY_ALREADY_EXISTS = 105
	ETCD_ENTRY_NOT_EXISTS     = 100
)

func ensureEtcClient(c *cli.Context, command string) (*etcd.Client, string) {
	machines := strings.Split(c.GlobalString("etcd"), ",")
	ectdClient := etcd.NewClient(machines)
	app := c.Args().First()
	if app == "" {
		println("ERROR: app name missing")
		cli.ShowCommandHelp(c, command)
		os.Exit(1)
	}
	return ectdClient, app
}

func appDeploy(c *cli.Context) {

	etcdClient, app := ensureEtcClient(c, "app:deploy")

	version := c.Args().Tail()[0]
	if version == "" {
		println("ERROR: app name missing")
		cli.ShowCommandHelp(c, "config")
		os.Exit(1)
	}

	_, err := etcdClient.Set("/"+c.GlobalString("env")+"/"+c.GlobalString("pool")+"/"+app+"/version", version, 0)
	if err != nil {
		fmt.Printf("ERROR: Could not store version: %s\n", err)
		os.Exit(1)
	}
}

func configList(c *cli.Context) {

	etcdClient, app := ensureEtcClient(c, "config")

	resp, err := etcdClient.Get("/"+c.GlobalString("env")+"/"+c.GlobalString("pool")+"/"+app+"/environment", true, true)
	if err != nil && err.(*etcd.EtcdError).ErrorCode != ETCD_ENTRY_NOT_EXISTS {
		fmt.Printf("ERROR: Could not unmarshall config: %s\n", err)
		os.Exit(1)
	}

	if err != nil && err.(*etcd.EtcdError).ErrorCode == ETCD_ENTRY_NOT_EXISTS {
		return
	}

	var env map[string]string
	err = json.Unmarshal([]byte(resp.Node.Value), &env)
	if err != nil {
		fmt.Printf("ERROR: Could not unmarshall config: %s\n", err)
		os.Exit(1)
	}

	for k, v := range env {
		println(fmt.Sprintf("%s=%s", k, v))
	}
}

func configSet(c *cli.Context) {

	etcdClient, app := ensureEtcClient(c, "config:set")

	var env map[string]string

	resp, err := etcdClient.Get("/"+c.GlobalString("env")+"/"+c.GlobalString("pool")+"/"+app+"/environment", true, true)
	if err != nil && err.(*etcd.EtcdError).ErrorCode != ETCD_ENTRY_NOT_EXISTS {
		fmt.Printf("ERROR: Could not connect to etcd: %s\n", err)
		os.Exit(1)
	}

	if err == nil || err.(*etcd.EtcdError).ErrorCode != ETCD_ENTRY_NOT_EXISTS {
		err = json.Unmarshal([]byte(resp.Node.Value), &env)
		if err != nil {
			fmt.Printf("ERROR: Could not unmarshall config: %s\n", err)
			os.Exit(1)
		}
	}

	for _, arg := range c.Args().Tail() {
		if !strings.Contains(arg, "=") {
			fmt.Printf("ERROR: bad config variable format: %s\n", arg)
			cli.ShowCommandHelp(c, "config")
			os.Exit(1)

		}
		values := strings.Split(arg, "=")
		env[strings.ToUpper(values[0])] = values[1]
	}

	serialized, err := json.Marshal(env)
	if err != nil {
		fmt.Printf("ERROR: Could not marshall config: %s\n", err)
		os.Exit(1)
	}

	resp, err = etcdClient.Set("/"+c.GlobalString("env")+"/"+c.GlobalString("pool")+"/"+app+"/environment", string(serialized), 0)
	if err != nil {
		fmt.Printf("ERROR: Could not store config: %s\n", err)
		os.Exit(1)
	}
}

func configUnset(c *cli.Context) {

	etcdClient, app := ensureEtcClient(c, "config:unset")

	env := map[string]string{}

	resp, err := etcdClient.Get("/"+c.GlobalString("env")+"/"+c.GlobalString("pool")+"/"+app+"/environment", true, true)
	if err != nil && err.(*etcd.EtcdError).ErrorCode != ETCD_ENTRY_NOT_EXISTS {
		fmt.Printf("ERROR: Could not connect to etcd: %s\n", err)
		os.Exit(1)
	}

	err = json.Unmarshal([]byte(resp.Node.Value), &env)
	if err != nil {
		fmt.Printf("ERROR: Could not unmarshall config: %s\n", err)
		os.Exit(1)
	}

	for _, arg := range c.Args().Tail() {
		delete(env, strings.ToUpper(arg))
	}

	serialized, err := json.Marshal(env)
	if err != nil {
		fmt.Printf("ERROR: Could not marshall config: %s\n", err)
		os.Exit(1)
	}

	resp, err = etcdClient.Set("/"+c.GlobalString("env")+"/"+c.GlobalString("pool")+"/"+app+"/environment", string(serialized), 0)
	if err != nil {
		fmt.Printf("ERROR: Could not store config: %s\n", err)
		os.Exit(1)
	}
}

func configGet(c *cli.Context) {

	etcdClient, app := ensureEtcClient(c, "config:get")

	env := map[string]string{}

	resp, err := etcdClient.Get("/"+c.GlobalString("env")+"/"+c.GlobalString("pool")+"/"+app+"/environment", true, true)
	if err != nil && err.(*etcd.EtcdError).ErrorCode != ETCD_ENTRY_NOT_EXISTS {
		fmt.Printf("ERROR: Could not connect to etcd: %s\n", err)
		os.Exit(1)
	}

	err = json.Unmarshal([]byte(resp.Node.Value), &env)
	if err != nil {
		fmt.Printf("ERROR: Could not unmarshall config: %s\n", err)
		os.Exit(1)
	}

	for _, arg := range c.Args().Tail() {
		fmt.Printf("%s=%s\n", strings.ToUpper(arg), env[strings.ToUpper(arg)])
	}
}

func main() {
	app := cli.NewApp()
	app.Name = "galaxy"
	app.Usage = "galaxy cli"
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "etcd", Value: "http://127.0.0.1:4001", Usage: "host:port[,host:port,..]"},
		cli.StringFlag{Name: "env", Value: "dev", Usage: "environment (dev, test, prod, etc.)"},
		cli.StringFlag{Name: "pool", Value: "web", Usage: "pool (web, worker, etc.)"},
	}

	app.Commands = []cli.Command{
		{
			Name:        "app:deploy",
			Usage:       "deploy a new version of an app",
			Action:      appDeploy,
			Description: "config <app> <version>",
		},
		{
			Name:        "config",
			Usage:       "list the config values for an app",
			Action:      configList,
			Description: "config <app>",
		},
		{
			Name:        "config:set",
			Usage:       "set one or more configuration variables",
			Action:      configSet,
			Description: "config <app> KEY=VALUE[,KEY=VALUE,..]",
		},
		{
			Name:        "config:unset",
			Usage:       "unset one or more configuration variables",
			Action:      configUnset,
			Description: "config <app> KEY[ KEY, etc..]",
		},

		{
			Name:   "config:get",
			Usage:  "display the config value for an app",
			Action: configGet,
		},
	}
	app.Run(os.Args)
}
