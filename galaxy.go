package main

import (
	"encoding/json"
	"fmt"
	"github.com/BurntSushi/toml"
	"github.com/codegangsta/cli"
	"github.com/coreos/go-etcd/etcd"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

const (
	ETCD_ENTRY_ALREADY_EXISTS = 105
	ETCD_ENTRY_NOT_EXISTS     = 100
)

type config struct {
	Host       string `toml:"host"`
	PrivateKey string `toml:"private_key"`
}

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

func findPrivateKeys(root string) []string {
	var availableKeys = []string{}
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		// Skip really big files to avoid OOM errors since they are
		// unlikely to be private keys
		if info.Size() > 1024*8 {
			return nil
		}
		contents, err := ioutil.ReadFile(path)
		if strings.Contains(string(contents), "PRIVATE KEY") {
			availableKeys = append(availableKeys, path)
		}
		return nil
	})
	return availableKeys
}

func findSshKeys(root string) []string {

	// Looks in .ssh dir and .vagrant.d dir for ssh keys
	var availableKeys = []string{}
	availableKeys = append(availableKeys, findPrivateKeys(filepath.Join(root, ".ssh"))...)
	availableKeys = append(availableKeys, findPrivateKeys(filepath.Join(root, ".vagrant.d"))...)

	return availableKeys
}

func login(c *cli.Context) {

	if c.Args().First() == "" {
		println("ERROR: host missing")
		cli.ShowCommandHelp(c, "login")
		os.Exit(1)
	}

	currentUser, err := user.Current()
	if err != nil {
		fmt.Printf("ERROR: Unable to determine current user: %s\n", err)
		os.Exit(1)
	}

	configDir := filepath.Join(currentUser.HomeDir, ".galaxy")
	_, err = os.Stat(configDir)
	if err != nil && os.IsNotExist(err) {
		os.Mkdir(configDir, 0700)
	}
	availableKeys := findSshKeys(currentUser.HomeDir)

	if len(availableKeys) == 0 {
		fmt.Printf("ERROR: No SSH private keys found.  Create one first.\n")
		os.Exit(1)
	}

	for i, key := range availableKeys {
		fmt.Printf("%d) %s\n", i, key)
	}

	fmt.Printf("Select private key to use [0]: ")
	var i int
	fmt.Scanf("%d", &i)

	if i < 0 || i > len(availableKeys) {
		i = 0
	}
	fmt.Printf("Using %s\n", availableKeys[i])

	galaxyConfig := config{
		Host:       c.Args().First(),
		PrivateKey: availableKeys[i],
	}

	configFile, err := os.Create(filepath.Join(configDir, "galaxy.toml"))
	if err != nil {
		fmt.Printf("ERROR: Unable to create config file: %s\n", err)
		os.Exit(1)
	}
	defer configFile.Close()

	encoder := toml.NewEncoder(configFile)
	encoder.Encode(galaxyConfig)
	configFile.WriteString("\n")
	fmt.Printf("Login sucessful")
}

func logout(c *cli.Context) {
	currentUser, err := user.Current()
	if err != nil {
		fmt.Printf("ERROR: Unable to determine current user: %s\n", err)
		os.Exit(1)
	}
	configFile := filepath.Join(currentUser.HomeDir, ".galaxy", "galaxy.toml")

	_, err = os.Stat(configFile)
	if err == nil {
		err = os.Remove(configFile)
		if err != nil {
			fmt.Printf("ERROR: Unable to logout: %s\n", err)
			os.Exit(1)
		}
	}
	fmt.Printf("Logout sucessful\n")
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
			Name:        "login",
			Usage:       "login to a controller",
			Action:      login,
			Description: "login host[:port]",
		},
		{
			Name:        "logout",
			Usage:       "logout off a controller",
			Action:      logout,
			Description: "logout",
		},
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
