package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/BurntSushi/toml"
	"github.com/codegangsta/cli"
	"github.com/coreos/go-etcd/etcd"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/runtime"
	"github.com/litl/galaxy/utils"
	"github.com/ryanuber/columnize"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	ETCD_ENTRY_ALREADY_EXISTS = 105
	ETCD_ENTRY_NOT_EXISTS     = 100
)

var (
	serviceRuntime  *runtime.ServiceRuntime
	serviceRegistry *registry.ServiceRegistry
)

var config struct {
	Host       string `toml:"host"`
	PrivateKey string `toml:"private_key"`
}

func ensureEtcClient(c *cli.Context) *etcd.Client {
	machines := strings.Split(c.GlobalString("etcd"), ",")
	ectdClient := etcd.NewClient(machines)
	return ectdClient
}

func ensureAppParam(c *cli.Context, command string) string {
	app := c.Args().First()
	if app == "" {
		fmt.Println("ERROR: app name missing")
		cli.ShowCommandHelp(c, command)
		os.Exit(1)
	}
	return app
}

func countInstances(etcdClient *etcd.Client, path, app string) int {
	total := 0
	nodes, err := etcdClient.Get(filepath.Join(path, "hosts"), true, true)
	if err != nil {
		return -1
	}
	for _, node := range nodes.Node.Nodes {
		for _, runtime := range node.Nodes {
			if filepath.Base(runtime.Key) == app {
				total += 1
			}
		}
	}
	return total
}

func envExists(etcdClient *etcd.Client, env string) (bool, error) {
	_, err := etcdClient.Get(utils.EtcdJoin(env), false, false)
	if err != nil && err.(*etcd.EtcdError).ErrorCode == ETCD_ENTRY_NOT_EXISTS {
		return false, nil
	}

	if err != nil {
		return false, err
	}
	return true, nil
}

func poolExists(etcdClient *etcd.Client, env, pool string) (bool, error) {
	_, err := etcdClient.Get(utils.EtcdJoin(env, pool), false, false)
	if err != nil && err.(*etcd.EtcdError).ErrorCode == ETCD_ENTRY_NOT_EXISTS {
		return false, nil
	}

	if err != nil {
		return false, err
	}
	return true, nil
}

func envPoolExists(etcdClient *etcd.Client, env, pool string) (bool, error) {
	exists, err := envExists(etcdClient, env)
	if err != nil {
		return false, errors.New(fmt.Sprintf("Could not determine if env `%s` exists: %s.", env, err))
	}

	if !exists {
		return false, errors.New(fmt.Sprintf("Environment `%s` does not exist. Create it first.", env))
	}

	exists, err = poolExists(etcdClient, env, pool)
	if err != nil {
		return false, errors.New(fmt.Sprintf("Could not determine if env `%s` pool `%s` exists: %s", env, pool, err))
	}

	if !exists {
		return false, errors.New(fmt.Sprintf("Pool `%s` for env `%s` does not exist. Create it first.", pool, env))
	}
	return true, nil
}

func appExists(etcdClient *etcd.Client, env, pool, app string) (bool, error) {
	exists, err := envPoolExists(etcdClient, env, pool)
	if err != nil {
		return false, err
	}

	if !exists {
		return false, nil
	}

	_, err = etcdClient.Get(utils.EtcdJoin(env, pool, app), false, false)
	if err != nil && err.(*etcd.EtcdError).ErrorCode == ETCD_ENTRY_NOT_EXISTS {
		return false, errors.New(fmt.Sprintf("App %s does not exist. Create it first.", app))
	}

	if err != nil {
		return false, err
	}
	return true, nil
}

func appList(c *cli.Context) {

	etcdClient := ensureEtcClient(c)
	path := utils.EtcdJoin(c.GlobalString("env"), c.GlobalString("pool"))

	poolExists, err := envPoolExists(etcdClient, c.GlobalString("env"), c.GlobalString("pool"))
	if err != nil {
		fmt.Printf("ERROR: %s\n", err)
		return
	}

	if !poolExists {
		return
	}

	entries, err := etcdClient.Get(path, false, false)
	if err != nil && err.(*etcd.EtcdError).ErrorCode == ETCD_ENTRY_NOT_EXISTS {
		fmt.Printf("ERROR: Environment (%s) or pool (%s) does not exist.\n",
			c.GlobalString("env"), c.GlobalString("pool"))
		return
	}

	if err != nil {
		fmt.Printf("ERROR: Could not find registered apps: %s\n", err)
		return
	}

	columns := []string{"NAME | CONFIGURED | VERSION | REGISTERED | POOL | ENV"}
	for _, entry := range entries.Node.Nodes {
		name := filepath.Base(entry.Key)
		// skip runtime host entry
		if name == "hosts" {
			continue
		}

		environmentConfigured := false
		_, err := etcdClient.Get(filepath.Join(path, name, "environment"), false, false)
		if err == nil {
			environmentConfigured = true
		}

		versionDeployed := ""
		version, err := etcdClient.Get(filepath.Join(path, name, "version"), false, false)
		if err == nil {
			versionDeployed = version.Node.Value
		}

		registered := countInstances(etcdClient, path, name)

		columns = append(columns, strings.Join([]string{
			name, strconv.FormatBool(environmentConfigured),
			versionDeployed, strconv.FormatInt(int64(registered), 10),
			c.GlobalString("pool"),
			c.GlobalString("env")}, " | "))
	}
	output, _ := columnize.SimpleFormat(columns)
	fmt.Println(output)
}

func appCreate(c *cli.Context) {

	etcdClient := ensureEtcClient(c)
	app := ensureAppParam(c, "app:create")

	exists, err := envExists(etcdClient, c.GlobalString("env"))
	if err != nil {
		fmt.Printf("ERROR: Could not create app: %s\n", err)
		return
	}

	if !exists {
		fmt.Printf("Environment %s does not exist. Create it first.\n", c.GlobalString("env"))
		return
	}

	_, err = etcdClient.CreateDir(utils.EtcdJoin(c.GlobalString("env"), c.GlobalString("pool"), app), 0)
	if err != nil {
		fmt.Printf("ERROR: Could not create app: %s\n", err)
		return
	}
	fmt.Printf("App `%s` created in env `%s` on pool `%s`\n", app, c.GlobalString("env"), c.GlobalString("pool"))
}

func appDelete(c *cli.Context) {

	etcdClient := ensureEtcClient(c)
	app := ensureAppParam(c, "app:delete")

	// Don't allow deleting runtime hosts entries
	if app == "hosts" {
		return
	}

	_, err := etcdClient.Delete(utils.EtcdJoin(c.GlobalString("env"), c.GlobalString("pool"), app), true)
	if err != nil && err.(*etcd.EtcdError).ErrorCode != ETCD_ENTRY_NOT_EXISTS {
		fmt.Printf("ERROR: Could not delete app: %s\n", err)
		return
	}
	fmt.Printf("App `%s` deleted from env `%s` on pool `%s`\n", app, c.GlobalString("env"), c.GlobalString("pool"))
}

func appDeploy(c *cli.Context) {

	etcdClient := ensureEtcClient(c)
	app := ensureAppParam(c, "app:deploy")

	version := ""
	if len(c.Args().Tail()) == 1 {
		version = c.Args().Tail()[0]
	}

	if version == "" {
		fmt.Println("ERROR: version missing")
		cli.ShowCommandHelp(c, "app:deploy")
		return
	}

	_, err := etcdClient.Get(utils.EtcdJoin(c.GlobalString("env"), c.GlobalString("pool"), app), false, false)
	if err != nil && err.(*etcd.EtcdError).ErrorCode == ETCD_ENTRY_NOT_EXISTS {
		fmt.Printf("ERROR: App %s does not exist. Create it first.\n", app)
		return
	}

	_, err = etcdClient.Set(utils.EtcdJoin(c.GlobalString("env"), c.GlobalString("pool"), app, "version"), version, 0)
	if err != nil {
		fmt.Printf("ERROR: Could not store version: %s\n", err)
		return
	}
}

func appRun(c *cli.Context) {

	etcdClient := ensureEtcClient(c)
	app := ensureAppParam(c, "app:run")

	if len(c.Args()) < 2 {
		fmt.Printf("ERROR: Missing command to run.\n")
		return
	}

	_, err := etcdClient.Get(utils.EtcdJoin(c.GlobalString("env"), c.GlobalString("pool"), app), false, false)
	if err != nil && err.(*etcd.EtcdError).ErrorCode == ETCD_ENTRY_NOT_EXISTS {
		fmt.Printf("ERROR: App %s does not exist. Create it first.\n", app)
		return
	}

	outputBuffer := &utils.OutputBuffer{}
	serviceRegistry = &registry.ServiceRegistry{
		EtcdHosts:    c.GlobalString("etcd"),
		Env:          c.GlobalString("env"),
		Pool:         c.GlobalString("pool"),
		HostIp:       c.GlobalString("hostIp"),
		TTL:          uint64(c.Int("ttl")),
		HostSSHAddr:  c.GlobalString("sshAddr"),
		OutputBuffer: outputBuffer,
	}

	serviceConfig, _ := serviceRegistry.GetServiceConfig(app)

	_, err = serviceRuntime.StartInteractive(serviceConfig, c.Args()[1:])
	if err != nil {
		fmt.Printf("ERROR: Could not start container: %s\n", err)
		return
	}
}

func configList(c *cli.Context) {

	etcdClient := ensureEtcClient(c)
	app := ensureAppParam(c, "config")

	exists, err := appExists(etcdClient, c.GlobalString("env"), c.GlobalString("pool"), app)
	if err != nil {
		fmt.Printf("ERROR: %s\n", err)
		return
	}

	if !exists {
		fmt.Printf("App %s does not exist. Create it first.", app)
		return
	}

	resp, err := etcdClient.Get(utils.EtcdJoin(c.GlobalString("env"), c.GlobalString("pool"), app, "environment"), true, true)
	if err != nil && err.(*etcd.EtcdError).ErrorCode != ETCD_ENTRY_NOT_EXISTS {
		fmt.Printf("ERROR: Could not unmarshall config: %s\n", err)
		return
	}

	if err != nil && err.(*etcd.EtcdError).ErrorCode == ETCD_ENTRY_NOT_EXISTS {
		return
	}

	var env map[string]string
	err = json.Unmarshal([]byte(resp.Node.Value), &env)
	if err != nil {
		fmt.Printf("ERROR: Could not unmarshall config: %s\n", err)
		return
	}

	for k, v := range env {
		fmt.Printf("%s=%s\n", k, v)
	}
}

func configSet(c *cli.Context) {

	etcdClient := ensureEtcClient(c)
	app := ensureAppParam(c, "config:set")

	appExists, err := appExists(etcdClient, c.GlobalString("env"), c.GlobalString("pool"), app)
	if err != nil {
		return
	}

	if !appExists {
		fmt.Printf("App %s does not exist. Create it first.", app)
		return
	}

	env := make(map[string]string)

	resp, err := etcdClient.Get(utils.EtcdJoin(c.GlobalString("env"), c.GlobalString("pool"), app, "environment"), true, true)
	if err != nil && err.(*etcd.EtcdError).ErrorCode != ETCD_ENTRY_NOT_EXISTS {
		fmt.Printf("ERROR: Could not connect to etcd: %s\n", err)
		return
	}

	if err == nil || err.(*etcd.EtcdError).ErrorCode != ETCD_ENTRY_NOT_EXISTS {
		err = json.Unmarshal([]byte(resp.Node.Value), &env)
		if err != nil {
			fmt.Printf("ERROR: Could not unmarshall config: %s\n", err)
			return
		}
	}

	for _, arg := range c.Args().Tail() {
		if !strings.Contains(arg, "=") {
			fmt.Printf("ERROR: bad config variable format: %s\n", arg)
			cli.ShowCommandHelp(c, "config")
			return

		}
		values := strings.Split(arg, "=")
		env[strings.ToUpper(values[0])] = values[1]
	}

	serialized, err := json.Marshal(env)
	if err != nil {
		fmt.Printf("ERROR: Could not marshall config: %s\n", err)
		return
	}

	resp, err = etcdClient.Set(utils.EtcdJoin(c.GlobalString("env"), c.GlobalString("pool"), app, "environment"), string(serialized), 0)
	if err != nil {
		fmt.Printf("ERROR: Could not store config: %s\n", err)
		return
	}
	fmt.Printf("Configuration changed for %s\n", app)
}

func configUnset(c *cli.Context) {

	etcdClient := ensureEtcClient(c)
	app := ensureAppParam(c, "config:unset")

	env := map[string]string{}

	resp, err := etcdClient.Get(utils.EtcdJoin(c.GlobalString("env"), c.GlobalString("pool"), app, "environment"), true, true)
	if err != nil && err.(*etcd.EtcdError).ErrorCode != ETCD_ENTRY_NOT_EXISTS {
		fmt.Printf("ERROR: Could not connect to etcd: %s\n", err)
		return
	}

	err = json.Unmarshal([]byte(resp.Node.Value), &env)
	if err != nil {
		fmt.Printf("ERROR: Could not unmarshall config: %s\n", err)
		return
	}

	for _, arg := range c.Args().Tail() {
		delete(env, strings.ToUpper(arg))
	}

	serialized, err := json.Marshal(env)
	if err != nil {
		fmt.Printf("ERROR: Could not marshall config: %s\n", err)
		return
	}

	resp, err = etcdClient.Set(utils.EtcdJoin(c.GlobalString("env"), c.GlobalString("pool"), app, "environment"), string(serialized), 0)
	if err != nil {
		fmt.Printf("ERROR: Could not store config: %s\n", err)
		return
	}
	fmt.Printf("Configuration changed for %s\n", app)
}

func configGet(c *cli.Context) {

	etcdClient := ensureEtcClient(c)
	app := ensureAppParam(c, "config:get")

	appExists, err := appExists(etcdClient, c.GlobalString("env"), c.GlobalString("pool"), app)
	if err != nil {
		return
	}

	if !appExists {
		fmt.Printf("App %s does not exist. Create it first.", app)
		return
	}

	env := map[string]string{}

	resp, err := etcdClient.Get(utils.EtcdJoin(c.GlobalString("env"), c.GlobalString("pool"), app, "environment"), true, true)
	if err != nil && err.(*etcd.EtcdError).ErrorCode != ETCD_ENTRY_NOT_EXISTS {
		fmt.Printf("ERROR: Could not connect to etcd: %s\n", err)
		return
	}

	err = json.Unmarshal([]byte(resp.Node.Value), &env)
	if err != nil {
		fmt.Printf("ERROR: Could not unmarshall config: %s\n", err)
		return
	}

	for _, arg := range c.Args().Tail() {
		fmt.Printf("%s=%s\n", strings.ToUpper(arg), env[strings.ToUpper(arg)])
	}
}

func login(c *cli.Context) {

	if c.Args().First() == "" {
		fmt.Println("ERROR: host missing")
		cli.ShowCommandHelp(c, "login")
		return
	}

	currentUser, err := user.Current()
	if err != nil {
		fmt.Printf("ERROR: Unable to determine current user: %s\n", err)
		return
	}

	configDir := filepath.Join(currentUser.HomeDir, ".galaxy")
	_, err = os.Stat(configDir)
	if err != nil && os.IsNotExist(err) {
		os.Mkdir(configDir, 0700)
	}
	availableKeys := findSshKeys(currentUser.HomeDir)

	if len(availableKeys) == 0 {
		fmt.Printf("ERROR: No SSH private keys found.  Create one first.\n")
		return
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

	config.Host = c.Args().First()
	config.PrivateKey = availableKeys[i]

	configFile, err := os.Create(filepath.Join(configDir, "galaxy.toml"))
	if err != nil {
		fmt.Printf("ERROR: Unable to create config file: %s\n", err)
		return
	}
	defer configFile.Close()

	encoder := toml.NewEncoder(configFile)
	encoder.Encode(config)
	configFile.WriteString("\n")
	fmt.Printf("Login sucessful")
}

func logout(c *cli.Context) {
	currentUser, err := user.Current()
	if err != nil {
		fmt.Printf("ERROR: Unable to determine current user: %s\n", err)
		return
	}
	configFile := filepath.Join(currentUser.HomeDir, ".galaxy", "galaxy.toml")

	_, err = os.Stat(configFile)
	if err == nil {
		err = os.Remove(configFile)
		if err != nil {
			fmt.Printf("ERROR: Unable to logout: %s\n", err)
			return
		}
	}
	fmt.Printf("Logout sucessful\n")
}

func poolList(c *cli.Context) {

	etcdClient := ensureEtcClient(c)
	resp, err := etcdClient.Get("/"+c.GlobalString("env"), true, true)
	if err != nil {
		fmt.Printf("ERROR: Unable to retrieve pools: %s\n", err)
		return
	}

	if len(resp.Node.Nodes) == 0 {
		fmt.Printf("No pools in %s\n", c.GlobalString("env"))
		return
	}

	for _, v := range resp.Node.Nodes {
		fmt.Printf("%s\n", filepath.Base(v.Key))
	}
}

func poolCreate(c *cli.Context) {

	etcdClient := ensureEtcClient(c)

	_, err := etcdClient.CreateDir(utils.EtcdJoin(c.GlobalString("env"), c.GlobalString("pool")), 0)
	if err != nil {
		fmt.Printf("ERROR: Could not create pool: %s\n", err)
	}

	//TODO: Create ASG
}

func poolDelete(c *cli.Context) {

	etcdClient := ensureEtcClient(c)
	resp, err := etcdClient.Get(utils.EtcdJoin(c.GlobalString("env"), c.GlobalString("pool")), true, true)
	if err != nil {
		fmt.Printf("ERROR: Could not delete pool: %s\n", err)
		return
	}

	hasAppsConfigured := false
	for _, v := range resp.Node.Nodes {
		if filepath.Base(v.Key) != "nodes" {
			hasAppsConfigured = true
			break
		}
	}

	if hasAppsConfigured {
		fmt.Printf("ERROR: Could not delete pool. Apps currently registered. Delete them first.\n")
		return
	}

	// TODO: Delete ASG

	_, err = etcdClient.Delete(utils.EtcdJoin(c.GlobalString("env"), c.GlobalString("pool")), true)
	if err != nil && err.(*etcd.EtcdError).ErrorCode != ETCD_ENTRY_NOT_EXISTS {
		fmt.Printf("ERROR: Could not delete pool: %s\n", err)
	}
}

func runRemote() {
	Sshcmd(config.Host, "galaxy "+strings.Join(os.Args[1:], " "), false, false)
}

func loadConfig() {

	currentUser, err := user.Current()
	if err != nil {
		fmt.Printf("ERROR: Unable to determine current user: %s\n", err)
		return
	}
	configFile := filepath.Join(currentUser.HomeDir, ".galaxy", "galaxy.toml")

	_, err = os.Stat(configFile)
	if err == nil {
		if _, err := toml.DecodeFile(configFile, &config); err != nil {
			fmt.Printf("ERROR: Unable to logout: %s\n", err)
			return
		}
	}

}

func main() {

	loadConfig()
	serviceRuntime = &runtime.ServiceRuntime{}
	if config.Host != "" && len(os.Args) > 1 && (os.Args[1] != "login" && os.Args[1] != "logout") {
		runRemote()
		return
	}

	app := cli.NewApp()
	app.Name = "galaxy"
	app.Usage = "galaxy cli"
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "etcd", Value: utils.GetEnv("GALAXY_ETCD_HOST", "http://127.0.0.1:4001"), Usage: "host:port[,host:port,..]"},
		cli.StringFlag{Name: "env", Value: utils.GetEnv("GALAXY_ENV", "dev"), Usage: "environment (dev, test, prod, etc.)"},
		cli.StringFlag{Name: "pool", Value: utils.GetEnv("GALAXY_POOL", "web"), Usage: "pool (web, worker, etc.)"},
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
			Name:        "app",
			Usage:       "list the apps currently created",
			Action:      appList,
			Description: "app",
		},
		{
			Name:        "app:create",
			Usage:       "create a new app",
			Action:      appCreate,
			Description: "app:create",
		},
		{
			Name:        "app:delete",
			Usage:       "delete a new app",
			Action:      appDelete,
			Description: "app:delete",
		},
		{
			Name:        "app:deploy",
			Usage:       "deploy a new version of an app",
			Action:      appDeploy,
			Description: "app:deploy <app> <version>",
		},
		{
			Name:        "app:run",
			Usage:       "run a command in a container",
			Action:      appRun,
			Description: "app:run <app> <command>",
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
		{
			Name:        "pool",
			Usage:       "list the pools",
			Action:      poolList,
			Description: "pool",
		},
		{
			Name:        "pool:create",
			Usage:       "create a pool",
			Action:      poolCreate,
			Description: "pool:create",
		},
		{
			Name:        "pool:delete",
			Usage:       "deletes a pool",
			Action:      poolDelete,
			Description: "pool:delete",
		},
	}
	app.Run(os.Args)
}
