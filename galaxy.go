package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/codegangsta/cli"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/runtime"
	"github.com/litl/galaxy/stack"
	"github.com/litl/galaxy/utils"
	"github.com/ryanuber/columnize"
)

var (
	serviceRuntime  *runtime.ServiceRuntime
	serviceRegistry *registry.ServiceRegistry

	initOnce     sync.Once
	buildVersion string
)

var config struct {
	Host string `toml:"host"`
}

// ensure the registry as a redis host, but only once
func initRegistry(c *cli.Context) {
	serviceRegistry = registry.NewServiceRegistry(
		c.GlobalString("env"),
		c.GlobalString("pool"),
		c.GlobalString("hostIp"),
		uint64(c.Int("ttl")),
		c.GlobalString("sshAddr"),
	)

	serviceRegistry.Connect(c.GlobalString("redis"))
}

// ensure the registry as a redis host, but only once
func initRuntime(c *cli.Context) {
	serviceRuntime = runtime.NewServiceRuntime(
		"",
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

	exists, err := appExists(app)
	if err != nil {
		log.Printf("ERROR: can't deteremine if %s exists: %s\n", app, err)
		os.Exit(1)
	}

	if !exists {
		log.Printf("ERROR: %s does not exist. Create it first.\n", app)
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

	appList, err := serviceRegistry.ListApps("")
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
	app := c.Args().First()
	if app == "" {
		log.Println("ERROR: app name missing")
		cli.ShowCommandHelp(c, "app:create")
		os.Exit(1)
	}

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

	image, err := serviceRuntime.PullImage(version, c.Bool("force"))
	if image == nil || err != nil {
		log.Printf("ERROR: Unable to pull %s. Has it been released yet?\n", version)
		return
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
		log.Printf("%s NOT deployed.", version)
		return
	}
	log.Printf("Deployed %s.\n", version)
}

func appRestart(c *cli.Context) {
	initRegistry(c)

	app := ensureAppParam(c, "app:restart")

	err := serviceRegistry.NotifyRestart(app)
	if err != nil {
		log.Printf("ERROR: Could not restart %s: %s\n", app, err)
		return
	}
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

	_, err = serviceRuntime.RunCommand(serviceConfig, c.Args()[1:])
	if err != nil {
		log.Printf("ERROR: Could not start container: %s\n", err)
		return
	}
}

func appShell(c *cli.Context) {
	initRegistry(c)
	initRuntime(c)

	app := ensureAppParam(c, "app:shell")

	serviceConfig, err := serviceRegistry.GetServiceConfig(app)
	if err != nil {
		log.Printf("ERROR: Unable to run command: %s.\n", err)
		return
	}

	err = serviceRuntime.StartInteractive(serviceConfig)
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

	keys := sort.StringSlice{}
	for k, _ := range cfg.Env() {
		keys = append(keys, k)
	}

	keys.Sort()

	for _, k := range keys {
		log.Printf("%s=%s\n", k, cfg.Env()[k])
	}

}

func configSet(c *cli.Context) {
	initRegistry(c)
	app := ensureAppParam(c, "config:set")

	args := c.Args().Tail()
	if len(args) == 0 {
		bytes, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			log.Printf("ERROR: Unable to read stdin: %s.\n", err)
			return

		}
		args = strings.Split(string(bytes), "\n")
	}

	if len(args) == 0 {
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

	for _, arg := range args {

		if strings.TrimSpace(arg) == "" {
			continue
		}

		if !strings.Contains(arg, "=") {
			log.Printf("ERROR: bad config variable format: %s\n", arg)
			cli.ShowCommandHelp(c, "config")
			return

		}
		values := strings.Split(arg, "=")
		svcCfg.EnvSet(strings.ToUpper(strings.TrimSpace(values[0])), strings.TrimSpace(values[1]))
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
		fmt.Printf("%s=%s\n", strings.ToUpper(arg), cfg.Env()[strings.ToUpper(arg)])
	}
}

// Return the path for the config directory, and create it if it doesn't exist
func cfgDir() string {
	homeDir := utils.HomeDir()
	if homeDir == "" {
		log.Fatal("ERROR: Unable to determine current home dir. Set $HOME.")
	}

	configDir := filepath.Join(homeDir, ".galaxy")
	_, err := os.Stat(configDir)
	if err != nil && os.IsNotExist(err) {
		err = os.Mkdir(configDir, 0700)
		if err != nil {
			log.Fatal("ERROR: cannot create config directory:", err)
		}
	}
	return configDir
}

func login(c *cli.Context) {
	initRegistry(c)

	if c.Args().First() == "" {
		log.Println("ERROR: host missing")
		cli.ShowCommandHelp(c, "login")
		return
	}

	configDir := cfgDir()
	config.Host = c.Args().First()

	// This will exit if it fails
	utils.SSHCmd(config.Host, "echo \"test\" > /dev/null", false, false)

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

	configFile := filepath.Join(cfgDir(), "galaxy.toml")

	_, err := os.Stat(configFile)
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
	utils.SSHCmd(config.Host, "galaxy "+strings.Join(os.Args[1:], " "), false, false)
}

func loadConfig() {
	configFile := filepath.Join(cfgDir(), "galaxy.toml")

	_, err := os.Stat(configFile)
	if err == nil {
		if _, err := toml.DecodeFile(configFile, &config); err != nil {
			log.Printf("ERROR: Unable to logout: %s\n", err)
			return
		}
	}

}

func pgPsql(c *cli.Context) {
	initRegistry(c)
	app := ensureAppParam(c, "pg:psql")

	serviceConfig, err := serviceRegistry.GetServiceConfig(app)
	if err != nil {
		log.Printf("ERROR: Unable to run command: %s.\n", err)
		return
	}

	database_url := serviceConfig.Env()["DATABASE_URL"]
	if database_url == "" {
		log.Printf("No DATABASE_URL configured.  Set one with config:set first.")
		return
	}

	if !strings.HasPrefix(database_url, "postgres://") {
		log.Printf("DATABASE_URL is not a postgres database.")
		return
	}

	cmd := exec.Command("psql", database_url)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Ignore SIGINT while the process is running
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)

	defer func() {
		signal.Stop(ch)
		close(ch)
	}()

	go func() {
		for {
			_, ok := <-ch
			if !ok {
				break
			}
		}
	}()

	err = cmd.Start()
	if err != nil {
		log.Fatal(err)
	}

	err = cmd.Wait()
	if err != nil {
		fmt.Printf("Command finished with error: %v\n", err)
	}
}

func stackInit(c *cli.Context) {
	stackName := c.Args().First()
	if stackName == "" {
		log.Fatal("stack name required")
	}

	keyPair := c.String("keypair")
	if keyPair == "" {
		log.Fatal("-keypair required")
	}

	var stackTmpl []byte
	tmplLoc := c.String("template")

	if tmplLoc != "" {
		var err error
		stackTmpl, err = ioutil.ReadFile(tmplLoc)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		stackTmpl = stack.GalaxyTemplate()
	}

	//TODO: Make this an option
	opts := map[string]string{
		"KeyPair": "admin-us-east",
	}

	err := stack.Create(stackName, stackTmpl, opts)
	if err != nil {
		log.Fatal(err)
	}
}

// manually create a pool stack
func stackCreatePool(c *cli.Context) {
	poolName := c.GlobalString("pool")
	if poolName == "" {
		log.Fatal("pool name required")
	}

	keyPair := c.String("keypair")
	if keyPair == "" {
		log.Fatal("keypair required")
	}

	amiID := c.String("ami")
	if amiID == "" {
		log.Fatal("ami required")
	}

	baseStack := c.String("base")
	if baseStack == "" {
		log.Fatal("base stack required")
	}

	poolEnv := c.GlobalString("env")
	if poolEnv == "" {
		log.Fatal("env required")
	}

	// get the resources we need from the base stack
	resources, err := stack.GetSharedResources(baseStack)
	if err != nil {
		log.Fatal(err)
	}

	// add all subnets
	subnets := []string{}
	for _, val := range resources.Subnets {
		subnets = append(subnets, val)
	}

	// TODO: add more options
	pool := stack.Pool{
		Name:            poolName,
		Env:             poolEnv,
		DesiredCapacity: 1,
		KeyName:         "admin-us-east",
		InstanceType:    "m3.medium",
		ImageID:         amiID,
		SubnetIDs:       subnets,
		SecurityGroups: []string{
			resources.SecurityGroups["sshSG"],
			resources.SecurityGroups["defaultSG"],
		},
	}

	if poolEnv == "web" {
		pool.ELB = true
		pool.ELBSecurityGroups = []string{
			resources.SecurityGroups["defaultSG"],
			resources.SecurityGroups["webSG"],
		}
		pool.ELBHealthCheck = "HTTP:8080/"
	}

	poolTmpl, err := stack.CreatePoolTemplate(pool)

	stackName := baseStack + poolName + poolEnv

	if err := stack.Update(stackName, poolTmpl, nil); err != nil {
		log.Fatal(err)
	}

	if err := stack.Wait(stackName, 5*time.Minute); err != nil {
		log.Fatal(err)
	}

}

// Print a Cloudformation template to stdout.  This is useful for generating a
// config file to edit, then create/update the base stack.
func stackTemplate(c *cli.Context) {
	stackName := c.Args().First()

	if stackName == "" {
		if _, err := os.Stdout.Write(stack.GalaxyTemplate()); err != nil {
			log.Fatal(err)
		}
		return
	}

	stackTmpl, err := stack.GetTemplate(stackName)
	if err != nil {
		log.Fatal(err)
	}

	if _, err := os.Stdout.Write(stackTmpl); err != nil {
		log.Fatal(err)
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
	app.Version = buildVersion
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
			Flags: []cli.Flag{
				cli.BoolFlag{Name: "force", Usage: "force pulling the image"},
			},
		},
		{
			Name:        "app:restart",
			Usage:       "restart an app",
			Action:      appRestart,
			Description: "app:restart <app>",
		},
		{
			Name:        "app:run",
			Usage:       "run a command in a container",
			Action:      appRun,
			Description: "app:run <app> <command>",
		},
		{
			Name:        "app:shell",
			Usage:       "run a bash shell in a container",
			Action:      appShell,
			Description: "app:shell <app>",
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
			Description: "config:set <app> KEY=VALUE [KEY=VALUE ...]",
		},
		{
			Name:        "config:unset",
			Usage:       "unset one or more configuration variables",
			Action:      configUnset,
			Description: "config:unset <app> KEY [KEY ...]",
		},
		{
			Name:        "config:get",
			Usage:       "display the config value for an app",
			Action:      configGet,
			Description: "config:get <app> KEY [KEY ...]",
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
		{
			Name:        "pg:psql",
			Usage:       "connect to database using psql",
			Action:      pgPsql,
			Description: "pg:psql <app>",
		},
		{
			Name:        "stack:init",
			Usage:       "initialize the galaxy infrastructure",
			Action:      stackInit,
			Description: "stack:init <stack_name>",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "template", Usage: "cloudformation template"},
				cli.StringFlag{Name: "keypair", Usage: "ssh keypair for galaxy controller"},
			},
		},
		{
			Name:        "stack:template",
			Usage:       "print the cloudformation template to stdout",
			Action:      stackTemplate,
			Description: "stack:template <stack_name>",
		},
		{
			Name:        "stack:create_pool",
			Usage:       "create a pool stack",
			Action:      stackCreatePool,
			Description: "stack:create_pool",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "base", Usage: "base stack name"},
				cli.StringFlag{Name: "keypair", Usage: "ssh keypair"},
				cli.StringFlag{Name: "ami", Usage: "ami id"},
			},
		},
	}
	app.Run(os.Args)
}
