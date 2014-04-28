package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/codegangsta/cli"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/runtime"
	"github.com/litl/galaxy/utils"
	"github.com/ryanuber/columnize"
)

var (
	serviceRuntime  *runtime.ServiceRuntime
	serviceRegistry *registry.ServiceRegistry

	initOnce sync.Once
)

var config struct {
	Host       string `toml:"host"`
	PrivateKey string `toml:"private_key"`
}

// ensure the registry as a redis host, but only once
func initRegistry(c *cli.Context) {
	f := func() {

		serviceRegistry = registry.NewServiceRegistry(
			c.GlobalString("env"),
			c.GlobalString("pool"),
			c.GlobalString("hostIp"),
			uint64(c.Int("ttl")),
			c.GlobalString("sshAddr"),
		)

		serviceRegistry.Connect(c.GlobalString("redis"))
	}

	initOnce.Do(f)
}

// ensure the registry as a redis host, but only once
func initRuntime(c *cli.Context) {
	serviceRuntime = runtime.NewServiceRuntime(
		"",
		c.GlobalString("env"),
		c.GlobalString("pool"),
		c.GlobalString("redis"),
	)
}

func ensureAppParam(c *cli.Context, command string) string {
	app := c.Args().First()
	if app == "" {
		log.Println("ERROR: app name missing")
		cli.ShowCommandHelp(c, command)
		os.Exit(1)
	}
	return app
}

func countInstances(app string) int {
	return serviceRegistry.CountInstances(app)
}

func envExists() (bool, error) {
	return serviceRegistry.EnvExists()
}

func poolExists() (bool, error) {
	return serviceRegistry.PoolExists()
}

func appExists(app string) (bool, error) {
	return serviceRegistry.AppExists(app)
}

func appList(c *cli.Context) {
	initRegistry(c)

	appList, err := serviceRegistry.ListApps()
	if err != nil {
		log.Printf("ERROR: %s\n", err)
		return
	}

	columns := []string{"NAME | CONFIGURED | VERSION | REGISTERED | POOL | ENV"}
	for _, app := range appList {
		name := app.Name
		environmentConfigured := app.Env != nil
		versionDeployed := app.Version()
		registered := serviceRegistry.CountInstances(name)

		columns = append(columns, strings.Join([]string{
			name, strconv.FormatBool(environmentConfigured),
			versionDeployed, strconv.Itoa(registered),
			c.GlobalString("pool"),
			c.GlobalString("env")}, " | "))
	}
	output, _ := columnize.SimpleFormat(columns)
	log.Println(output)
}

func appCreate(c *cli.Context) {
	initRegistry(c)
	app := ensureAppParam(c, "app:delete")

	// Don't allow deleting runtime hosts entries
	if app == "hosts" {
		return
	}

	exists, err := serviceRegistry.PoolExists()
	if err != nil {
		log.Printf("ERROR: Could not create app: %s\n", err)
		return
	}
	if !exists {
		log.Printf("ERROR: Pool %s does not exist. Create it first.\n", serviceRegistry.Pool)
		return
	}

	created, err := serviceRegistry.CreateApp(app)

	if err != nil {
		log.Printf("ERROR: Could not create app: %s\n", err)
		return
	}
	if created {
		log.Printf("Created %s in env %s on pool %s.\n", app, c.GlobalString("env"), c.GlobalString("pool"))
	} else {
		log.Printf("%s already exists in in env %s on pool %s.\n", app, c.GlobalString("env"), c.GlobalString("pool"))
	}
}

func appDelete(c *cli.Context) {
	initRegistry(c)
	app := ensureAppParam(c, "app:delete")

	// Don't allow deleting runtime hosts entries
	if app == "hosts" {
		return
	}

	deleted, err := serviceRegistry.DeleteApp(app)
	if err != nil {
		log.Printf("ERROR: Could not delete app: %s\n", err)
		return
	}
	if deleted {
		log.Printf("Deleted %s from env %s on pool %s.\n", app, c.GlobalString("env"), c.GlobalString("pool"))
	} else {
		log.Printf("%s does not exists in env %s on pool %s.\n", app, c.GlobalString("env"), c.GlobalString("pool"))
	}

}

func appDeploy(c *cli.Context) {
	initRegistry(c)
	initRuntime(c)

	app := ensureAppParam(c, "app:deploy")

	version := ""
	if len(c.Args().Tail()) == 1 {
		version = c.Args().Tail()[0]
	}

	if version == "" {
		log.Println("ERROR: version missing")
		cli.ShowCommandHelp(c, "app:deploy")
		return
	}

	registry, repository, _ := utils.SplitDockerImage(version)

	image, err := serviceRuntime.InspectImage(version)
	if image == nil && err == nil {
		image, err = serviceRuntime.PullImage(registry, repository)
		if err != nil {
			log.Printf("ERROR: Unable to pull %s. Has it been released yet?\n", version)
			return
		}
	}

	svcCfg, err := serviceRegistry.GetServiceConfig(app)
	if err != nil {
		log.Printf("ERROR: Unable to deploy app: %s.\n", err)
		return
	}

	if svcCfg == nil {
		log.Printf("ERROR: App %s does not exist. Create it first.\n", app)
		return
	}

	svcCfg.SetVersion(version)
	// TODO, the ID should be handled behinf the scenes
	svcCfg.ID = time.Now().UnixNano()

	svcCfg.ClearPorts()
	for k, _ := range image.Config.ExposedPorts {
		svcCfg.AddPort(k.Port(), k.Proto())
	}

	updated, err := serviceRegistry.SetServiceConfig(svcCfg)
	if err != nil {
		log.Printf("ERROR: Could not store version: %s\n", err)
		return
	}
	if !updated {
		log.Printf("%s NOT deployed.\n")
		return
	}
	log.Printf("Deployed %s.\n", version)
}

func appRun(c *cli.Context) {
	initRegistry(c)
	initRuntime(c)

	app := ensureAppParam(c, "app:run")

	if len(c.Args()) < 2 {
		log.Printf("ERROR: Missing command to run.\n")
		return
	}

	serviceConfig, err := serviceRegistry.GetServiceConfig(app)
	if err != nil {
		log.Printf("ERROR: Unable to run command: %s.\n", err)
		return
	}

	_, err = serviceRuntime.StartInteractive(serviceConfig, c.Args()[1:])
	if err != nil {
		log.Printf("ERROR: Could not start container: %s\n", err)
		return
	}
}

func configList(c *cli.Context) {
	initRegistry(c)
	app := ensureAppParam(c, "config")

	cfg, err := serviceRegistry.GetServiceConfig(app)
	if err != nil {
		log.Printf("ERROR: Unable to list config: %s.\n", err)
		return
	}

	if cfg == nil {
		log.Printf("ERROR: Unable to list config for %s.\n", app)
		return
	}

	for k, v := range cfg.Env() {
		log.Printf("%s=%s\n", k, v)
	}
}

func configSet(c *cli.Context) {
	initRegistry(c)
	app := ensureAppParam(c, "config:set")

	if len(c.Args().Tail()) == 0 {
		log.Printf("ERROR: No config values specified.\n")
		return
	}

	svcCfg, err := serviceRegistry.GetServiceConfig(app)
	if err != nil {
		log.Printf("ERROR: Unable to set config: %s.\n", err)
		return
	}

	if svcCfg == nil {
		svcCfg = registry.NewServiceConfig(app, "")
	}

	for _, arg := range c.Args().Tail() {
		if !strings.Contains(arg, "=") {
			log.Printf("ERROR: bad config variable format: %s\n", arg)
			cli.ShowCommandHelp(c, "config")
			return

		}
		values := strings.Split(arg, "=")
		svcCfg.EnvSet(strings.ToUpper(values[0]), values[1])
	}

	updated, err := serviceRegistry.SetServiceConfig(svcCfg)
	if err != nil {
		log.Printf("ERROR: Unable to set config: %s.\n", err)
		return
	}

	if !updated {
		log.Printf("Configuration NOT changed for %s\n", app)
		return
	}
	log.Printf("Configuration changed for %s\n", app)
}

func configUnset(c *cli.Context) {
	initRegistry(c)
	app := ensureAppParam(c, "config:unset")

	if len(c.Args().Tail()) == 0 {
		log.Printf("ERROR: No config values specified.\n")
		return
	}

	svcCfg, err := serviceRegistry.GetServiceConfig(app)
	if err != nil {
		log.Printf("ERROR: Unable to unset config: %s.\n", err)
		return
	}

	for _, arg := range c.Args().Tail() {
		svcCfg.EnvSet(strings.ToUpper(arg), "")
	}

	updated, err := serviceRegistry.SetServiceConfig(svcCfg)
	if err != nil {
		log.Printf("ERROR: Unable to unset config: %s.\n", err)
		return
	}

	if !updated {
		log.Printf("Configuration NOT changed for %s\n", app)
		return
	}
	log.Printf("Configuration changed for %s\n", app)

}

func configGet(c *cli.Context) {
	initRegistry(c)
	app := ensureAppParam(c, "config:get")

	cfg, err := serviceRegistry.GetServiceConfig(app)
	if err != nil {
		log.Printf("ERROR: Unable to get config: %s.\n", err)
		return
	}

	for _, arg := range c.Args().Tail() {
		log.Printf("%s=%s\n", strings.ToUpper(arg), cfg.Env()[strings.ToUpper(arg)])
	}
}

func login(c *cli.Context) {
	initRegistry(c)

	if c.Args().First() == "" {
		log.Println("ERROR: host missing")
		cli.ShowCommandHelp(c, "login")
		return
	}

	currentUser, err := user.Current()
	if err != nil {
		log.Printf("ERROR: Unable to determine current user: %s\n", err)
		return
	}

	configDir := filepath.Join(currentUser.HomeDir, ".galaxy")
	_, err = os.Stat(configDir)
	if err != nil && os.IsNotExist(err) {
		os.Mkdir(configDir, 0700)
	}
	availableKeys := findSSHKeys(currentUser.HomeDir)

	if len(availableKeys) == 0 {
		log.Printf("ERROR: No SSH private keys found.  Create one first.\n")
		return
	}

	for i, key := range availableKeys {
		log.Printf("%d) %s\n", i, key)
	}

	log.Printf("Select private key to use [0]: ")
	var i int
	fmt.Scanf("%d", &i)

	if i < 0 || i > len(availableKeys) {
		i = 0
	}
	log.Printf("Using %s\n", availableKeys[i])

	config.Host = c.Args().First()
	config.PrivateKey = availableKeys[i]

	configFile, err := os.Create(filepath.Join(configDir, "galaxy.toml"))
	if err != nil {
		log.Printf("ERROR: Unable to create config file: %s\n", err)
		return
	}
	defer configFile.Close()

	encoder := toml.NewEncoder(configFile)
	encoder.Encode(config)
	configFile.WriteString("\n")
	log.Printf("Login sucessful")
}

func logout(c *cli.Context) {
	initRegistry(c)
	currentUser, err := user.Current()
	if err != nil {
		log.Printf("ERROR: Unable to determine current user: %s\n", err)
		return
	}
	configFile := filepath.Join(currentUser.HomeDir, ".galaxy", "galaxy.toml")

	_, err = os.Stat(configFile)
	if err == nil {
		err = os.Remove(configFile)
		if err != nil {
			log.Printf("ERROR: Unable to logout: %s\n", err)
			return
		}
	}
	log.Printf("Logout sucessful\n")
}

func poolCreate(c *cli.Context) {

	initRegistry(c)
	created, err := serviceRegistry.CreatePool(c.GlobalString("pool"))
	if err != nil {
		log.Printf("ERROR: Could not create pool: %s\n", err)
		return
	}

	if created {
		log.Printf("Pool %s created\n", c.GlobalString("pool"))
	} else {
		log.Printf("Pool %s already exists\n", c.GlobalString("pool"))
	}

}

func poolList(c *cli.Context) {
	initRegistry(c)
	pools, err := serviceRegistry.ListPools()
	if err != nil {
		log.Printf("ERROR: cannot list pools: %s\n", err)
		return
	}

	for _, pool := range pools {
		log.Println(pool)
	}
}

func poolDelete(c *cli.Context) {

	initRegistry(c)
	created, err := serviceRegistry.DeletePool(c.GlobalString("pool"))
	if err != nil {
		log.Printf("ERROR: Could not delete pool: %s\n", err)
		return
	}

	if created {
		log.Printf("Pool %s delete\n", c.GlobalString("pool"))
	} else {
		log.Printf("Pool %s has apps configured. Delete them first.\n", c.GlobalString("pool"))
	}
}

func runRemote() {
	SSHCmd(config.Host, "galaxy "+strings.Join(os.Args[1:], " "), false, false)
}

func loadConfig() {

	currentUser, err := user.Current()
	if err != nil {
		log.Printf("ERROR: Unable to determine current user: %s\n", err)
		return
	}
	configFile := filepath.Join(currentUser.HomeDir, ".galaxy", "galaxy.toml")

	_, err = os.Stat(configFile)
	if err == nil {
		if _, err := toml.DecodeFile(configFile, &config); err != nil {
			log.Printf("ERROR: Unable to logout: %s\n", err)
			return
		}
	}

}

func main() {

	loadConfig()

	// Don't print date, etc..
	log.DefaultLogger.SetFlags(0)

	if config.Host != "" && len(os.Args) > 1 && (os.Args[1] != "login" && os.Args[1] != "logout") {
		runRemote()
		return
	}

	app := cli.NewApp()
	app.Name = "galaxy"
	app.Usage = "galaxy cli"
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "redis", Value: utils.GetEnv("GALAXY_REDIS_HOST", "127.0.0.1:6379"), Usage: "host:port[,host:port,..]"},
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
