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

	"github.com/BurntSushi/toml"
	"github.com/codegangsta/cli"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/registry"
	"github.com/litl/galaxy/runtime"
	"github.com/litl/galaxy/utils"
	"github.com/ryanuber/columnize"

	"github.com/dotcloud/docker/pkg/term"
)

var (
	serviceRuntime  *runtime.ServiceRuntime
	serviceRegistry *registry.ServiceRegistry

	initOnce     sync.Once
	buildVersion string

	// make certain we have a tty for interactive prompts
	tty bool
)

var config struct {
	Host string `toml:"host"`
}

func init() {
	tty = term.IsTerminal(os.Stdin.Fd())
}

// ensure the registry as a redis host, but only once
func initRegistry(c *cli.Context) {

	serviceRegistry = registry.NewServiceRegistry(
		utils.GalaxyEnv(c),
		utils.GalaxyPool(c),
		c.GlobalString("hostIp"),
		uint64(c.Int("ttl")),
		c.GlobalString("sshAddr"),
	)

	serviceRegistry.Connect(utils.GalaxyRedisHost(c))
}

// ensure the registry as a redis host, but only once
func initRuntime(c *cli.Context) {
	serviceRuntime = runtime.NewServiceRuntime(
		serviceRegistry,
		"",
		"",
	)
}

func ensureAppParam(c *cli.Context, command string) string {
	app := c.Args().First()
	if app == "" {
		cli.ShowCommandHelp(c, command)
		log.Fatal("ERROR: app name missing")
	}

	exists, err := appExists(app)
	if err != nil {
		log.Fatalf("ERROR: can't deteremine if %s exists: %s\n", app, err)
	}

	if !exists {
		log.Fatalf("ERROR: %s does not exist. Create it first.\n", app)
	}

	return app
}

func ensureEnvArg(c *cli.Context) {
	if utils.GalaxyEnv(c) == "" {
		log.Fatal("ERROR: env is required.  Pass --env or set GALAXY_ENV")
	}
}

func ensurePoolArg(c *cli.Context) {
	if utils.GalaxyPool(c) == "" {
		log.Fatal("ERROR: pool is required.  Pass --pool or set GALAXY_POOL")
	}
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

	envs := []string{utils.GalaxyEnv(c)}

	if utils.GalaxyEnv(c) == "" {
		var err error
		envs, err = serviceRegistry.ListEnvs()
		if err != nil {
			log.Fatalf("ERROR: %s", err)
			return
		}
	}

	columns := []string{"ENV | NAME | VERSION | PORT | REGISTERED"}

	for _, env := range envs {

		serviceRegistry.Env = env
		appList, err := serviceRegistry.ListApps()
		if err != nil {
			log.Fatalf("ERROR: %s", err)
			return
		}

		for _, app := range appList {
			name := app.Name
			port := app.EnvGet("GALAXY_PORT")
			versionDeployed := app.Version()
			registered := serviceRegistry.CountInstances(name)

			columns = append(columns, strings.Join([]string{
				env,
				name,
				versionDeployed,
				port,
				strconv.Itoa(registered),
			}, " | "))
		}
	}
	output, _ := columnize.SimpleFormat(columns)
	log.Println(output)
}

func appCreate(c *cli.Context) {
	ensureEnvArg(c)
	initRegistry(c)

	app := c.Args().First()
	if app == "" {
		cli.ShowCommandHelp(c, "app:create")
		log.Fatal("ERROR: app name missing")
	}

	// Don't allow deleting runtime hosts entries
	if app == "hosts" {
		return
	}

	created, err := serviceRegistry.CreateApp(app)

	if err != nil {
		log.Fatalf("ERROR: Could not create app: %s\n", err)
		return
	}
	if created {
		log.Printf("Created %s in env %s.\n", app, utils.GalaxyEnv(c))
	} else {
		log.Printf("%s already exists in in env %s.\n", app, utils.GalaxyEnv(c))
	}
}

func appDelete(c *cli.Context) {
	ensureEnvArg(c)
	initRegistry(c)

	app := ensureAppParam(c, "app:delete")

	// Don't allow deleting runtime hosts entries
	if app == "hosts" || app == "pools" {
		return
	}

	deleted, err := serviceRegistry.DeleteApp(app)
	if err != nil {
		log.Fatalf("ERROR: Could not delete app: %s\n", err)
		return
	}
	if deleted {
		log.Printf("Deleted %s from env %s.\n", app, utils.GalaxyEnv(c))
	} else {
		log.Printf("%s does not exists in env %s.\n", app, utils.GalaxyEnv(c))
	}

}

func appDeploy(c *cli.Context) {
	ensureEnvArg(c)
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

	image, err := serviceRuntime.PullImage(version, "", c.Bool("force"))
	if image == nil || err != nil {
		log.Fatalf("ERROR: Unable to pull %s. Has it been released yet?\n", version)
		return
	}

	svcCfg, err := serviceRegistry.GetServiceConfig(app)
	if err != nil {
		log.Fatalf("ERROR: Unable to deploy app: %s.\n", err)
		return
	}

	if svcCfg == nil {
		log.Fatalf("ERROR: App %s does not exist. Create it first.\n", app)
		return
	}

	svcCfg.SetVersion(version)
	svcCfg.SetVersionID(image.ID)

	svcCfg.ClearPorts()
	for k, _ := range image.Config.ExposedPorts {
		svcCfg.AddPort(k.Port(), k.Proto())
	}

	updated, err := serviceRegistry.SetServiceConfig(svcCfg)
	if err != nil {
		log.Fatalf("ERROR: Could not store version: %s\n", err)
		return
	}
	if !updated {
		log.Fatalf("%s NOT deployed.", version)
		return
	}
	log.Printf("Deployed %s.\n", version)
}

func appRestart(c *cli.Context) {
	initRegistry(c)

	app := ensureAppParam(c, "app:restart")

	err := serviceRegistry.NotifyRestart(app)
	if err != nil {
		log.Fatalf("ERROR: Could not restart %s: %s", app, err)
		return
	}
}

func appRun(c *cli.Context) {
	ensureEnvArg(c)
	initRegistry(c)
	initRuntime(c)

	app := ensureAppParam(c, "app:run")

	if len(c.Args()) < 2 {
		log.Fatalf("ERROR: Missing command to run.\n")
		return
	}

	serviceConfig, err := serviceRegistry.GetServiceConfig(app)
	if err != nil {
		log.Fatalf("ERROR: Unable to run command: %s.\n", err)
		return
	}

	_, err = serviceRuntime.RunCommand(serviceConfig, c.Args()[1:])
	if err != nil {
		log.Fatalf("ERROR: Could not start container: %s\n", err)
		return
	}
}

func appShell(c *cli.Context) {
	ensureEnvArg(c)
	initRegistry(c)
	initRuntime(c)

	app := ensureAppParam(c, "app:shell")

	serviceConfig, err := serviceRegistry.GetServiceConfig(app)
	if err != nil {
		log.Fatalf("ERROR: Unable to run command: %s.\n", err)
		return
	}

	err = serviceRuntime.StartInteractive(serviceConfig)
	if err != nil {
		log.Fatalf("ERROR: Could not start container: %s\n", err)
		return
	}
}

func configList(c *cli.Context) {
	ensureEnvArg(c)
	initRegistry(c)
	app := ensureAppParam(c, "config")

	cfg, err := serviceRegistry.GetServiceConfig(app)
	if err != nil {
		log.Fatalf("ERROR: Unable to list config: %s.\n", err)
		return
	}

	if cfg == nil {
		log.Fatalf("ERROR: Unable to list config for %s.\n", app)
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
	ensureEnvArg(c)
	initRegistry(c)
	app := ensureAppParam(c, "config:set")

	args := c.Args().Tail()
	if len(args) == 0 {
		bytes, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			log.Fatalf("ERROR: Unable to read stdin: %s.\n", err)
			return

		}
		args = strings.Split(string(bytes), "\n")
	}

	if len(args) == 0 {
		log.Fatalf("ERROR: No config values specified.\n")
		return
	}

	svcCfg, err := serviceRegistry.GetServiceConfig(app)
	if err != nil {
		log.Fatalf("ERROR: Unable to set config: %s.\n", err)
		return
	}

	if svcCfg == nil {
		svcCfg = registry.NewServiceConfig(app, "")
	}

	updated := false
	for _, arg := range args {

		if strings.TrimSpace(arg) == "" {
			continue
		}

		if !strings.Contains(arg, "=") {
			log.Fatalf("ERROR: bad config variable format: %s\n", arg)
			cli.ShowCommandHelp(c, "config")
			return

		}
		sep := strings.Index(arg, "=")
		k := strings.ToUpper(strings.TrimSpace(arg[0:sep]))
		v := strings.TrimSpace(arg[sep+1:])
		if k == "ENV" {
			log.Warnf("%s cannot be updated.", k)
			continue
		}

		log.Printf("%s=%s\n", k, v)
		svcCfg.EnvSet(k, v)
		updated = true
	}

	if !updated {
		log.Errorf("Configuration NOT changed for %s\n", app)
		return
	}

	updated, err = serviceRegistry.SetServiceConfig(svcCfg)
	if err != nil {
		log.Fatalf("ERROR: Unable to set config: %s.\n", err)
		return
	}

	if !updated {
		log.Errorf("Configuration NOT changed for %s\n", app)
		return
	}
	log.Printf("Configuration changed for %s. v%d\n", app, svcCfg.ID())
}

func configUnset(c *cli.Context) {
	ensureEnvArg(c)
	initRegistry(c)
	app := ensureAppParam(c, "config:unset")

	if len(c.Args().Tail()) == 0 {
		log.Fatalf("ERROR: No config values specified.\n")
		return
	}

	svcCfg, err := serviceRegistry.GetServiceConfig(app)
	if err != nil {
		log.Fatalf("ERROR: Unable to unset config: %s.\n", err)
		return
	}

	updated := false
	for _, arg := range c.Args().Tail() {
		k := strings.ToUpper(strings.TrimSpace(arg))
		if k == "ENV" || svcCfg.EnvGet(k) == "" {
			log.Warnf("%s cannot be unset.", k)
			continue
		}

		log.Printf("%s\n", k)
		svcCfg.EnvSet(strings.ToUpper(arg), "")
		updated = true
	}

	if !updated {
		log.Errorf("Configuration NOT changed for %s\n", app)
		return
	}

	updated, err = serviceRegistry.SetServiceConfig(svcCfg)
	if err != nil {
		log.Errorf("ERROR: Unable to unset config: %s.\n", err)
		return
	}

	if !updated {
		log.Errorf("Configuration NOT changed for %s\n", app)
		return
	}
	log.Printf("Configuration changed for %s. v%d.\n", app, svcCfg.ID())

}

func configGet(c *cli.Context) {
	ensureEnvArg(c)
	initRegistry(c)
	app := ensureAppParam(c, "config:get")

	cfg, err := serviceRegistry.GetServiceConfig(app)
	if err != nil {
		log.Fatalf("ERROR: Unable to get config: %s.\n", err)
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

func poolAssign(c *cli.Context) {
	ensureEnvArg(c)
	ensurePoolArg(c)
	initRegistry(c)

	app := ensureAppParam(c, "pool:assign")

	// Don't allow deleting runtime hosts entries
	if app == "hosts" || app == "pools" {
		return
	}

	exists, err := serviceRegistry.PoolExists()
	if err != nil {
		log.Fatalf("ERROR: Could not assign app: %s\n", err)
		return
	}

	if !exists {
		log.Fatalf("ERROR: Pool %s does not exist.  Create it first.\n", utils.GalaxyPool(c))
		return
	}

	created, err := serviceRegistry.AssignApp(app)

	if err != nil {
		log.Fatalf("ERROR: Could not assign app: %s\n", err)
		return
	}
	if created {
		log.Printf("Assigned %s in env %s to pool %s.\n", app, utils.GalaxyEnv(c), utils.GalaxyPool(c))
	} else {
		log.Printf("%s already assigned to pool %s in env %s.\n", app, utils.GalaxyPool(c), utils.GalaxyEnv(c))
	}
}

func poolUnassign(c *cli.Context) {
	ensureEnvArg(c)
	ensurePoolArg(c)
	initRegistry(c)

	app := c.Args().First()
	if app == "" {
		cli.ShowCommandHelp(c, "pool:assign")
		log.Fatal("ERROR: app name missing")
	}

	// Don't allow deleting runtime hosts entries
	if app == "hosts" || app == "pools" {
		return
	}

	deleted, err := serviceRegistry.UnassignApp(app)
	if err != nil {
		log.Fatalf("ERROR: Could not unassign app: %s\n", err)
		return
	}

	if deleted {
		log.Printf("Unassigned %s in env %s from pool %s\n", app, utils.GalaxyEnv(c), utils.GalaxyPool(c))
	} else {
		log.Printf("%s could not be unassigned.\n", utils.GalaxyPool(c))
	}
}

func poolCreate(c *cli.Context) {
	ensureEnvArg(c)
	ensurePoolArg(c)
	initRegistry(c)
	created, err := serviceRegistry.CreatePool(utils.GalaxyPool(c))
	if err != nil {
		log.Fatalf("ERROR: Could not create pool: %s\n", err)
		return
	}

	if created {
		log.Printf("Pool %s created\n", utils.GalaxyPool(c))
	} else {
		log.Printf("Pool %s already exists\n", utils.GalaxyPool(c))
	}

	ec2host, err := runtime.EC2PublicHostname()
	if err != nil || ec2host == "" {
		log.Debug("not running from AWS, skipping pool creation")
		return
	}

	// now create the cloudformation stack
	// is this fails, the stack can be created separately with
	// stack:create_pool
	stackCreatePool(c)
}

func poolUpdate(c *cli.Context) {
	ensureEnvArg(c)
	ensurePoolArg(c)
	stackUpdatePool(c)
}

func poolList(c *cli.Context) {
	initRegistry(c)

	envs := []string{utils.GalaxyEnv(c)}
	if utils.GalaxyEnv(c) == "" {
		var err error
		envs, err = serviceRegistry.ListEnvs()
		if err != nil {
			log.Fatalf("ERROR: %s\n", err)
		}
	}

	columns := []string{"ENV | POOL | APPS "}

	for _, env := range envs {
		serviceRegistry.Env = env
		pools, err := serviceRegistry.ListPools()
		if err != nil {
			log.Fatalf("ERROR: cannot list pools: %s\n", err)
			return
		}

		if len(pools) == 0 {
			columns = append(columns, strings.Join([]string{
				env,
				"",
				""}, " | "))
			continue
		}

		for name, assigments := range pools {
			columns = append(columns, strings.Join([]string{
				env,
				name,
				strings.Join(assigments, ",")}, " | "))
		}

	}
	output, _ := columnize.SimpleFormat(columns)
	log.Println(output)
}

func poolDelete(c *cli.Context) {
	ensureEnvArg(c)
	ensurePoolArg(c)
	initRegistry(c)
	created, err := serviceRegistry.DeletePool(utils.GalaxyPool(c))
	if err != nil {
		log.Fatalf("ERROR: Could not delete pool: %s\n", err)
		return
	}

	if created {
		log.Printf("Pool %s delete\n", utils.GalaxyPool(c))
		// now delete the Cloudformation Stack
		stackDeletePool(c)

	} else {
		log.Printf("Pool %s has apps assigned. Unassign them first.\n", utils.GalaxyPool(c))
	}
}

func loadConfig() {
	configFile := filepath.Join(cfgDir(), "galaxy.toml")

	_, err := os.Stat(configFile)
	if err == nil {
		if _, err := toml.DecodeFile(configFile, &config); err != nil {
			log.Fatalf("ERROR: Unable to logout: %s\n", err)
			return
		}
	}

}

func pgPsql(c *cli.Context) {
	ensureEnvArg(c)
	initRegistry(c)
	app := ensureAppParam(c, "pg:psql")

	serviceConfig, err := serviceRegistry.GetServiceConfig(app)
	if err != nil {
		log.Fatalf("ERROR: Unable to run command: %s.\n", err)
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

func main() {

	loadConfig()

	// Don't print date, etc..
	log.DefaultLogger.SetFlags(0)

	// declare one superset of flags for stack operations, so we don't pollute the global flags
	// TODO: these need to be broken up into proper sets for each command to
	//       prevent confusing help messages.
	stackFlags := []cli.Flag{
		cli.StringFlag{Name: "base", Usage: "base stack name"},
		cli.StringFlag{Name: "keyname", Usage: "ssh keypair name"},
		cli.StringFlag{Name: "ami", Usage: "ami id"},
		cli.StringFlag{Name: "instance-type", Usage: "instance type"},
		cli.StringFlag{Name: "parameters", Usage: "template parameters in json"},
		cli.StringFlag{Name: "ssl-cert", Usage: "SSL certificate name"},
		cli.StringFlag{Name: "policy", Usage: "stack policy"},
		cli.StringFlag{Name: "region", Usage: "aws region"},
		cli.StringFlag{Name: "template", Usage: "provide a template file"},
		cli.IntFlag{Name: "min-size", Usage: "minimum pool size"},
		cli.IntFlag{Name: "max-size", Usage: "maximum pool size"},
		cli.IntFlag{Name: "desired-size", Usage: "desired pool size"},
		cli.IntFlag{Name: "http-port", Usage: "instance http port"},
		cli.BoolFlag{Name: "print", Usage: "print new template and exit"},
		cli.BoolFlag{Name: "auto-update", Usage: "add an ASG UpdatePolicy"},
		cli.IntFlag{Name: "scale-adj", Usage: "number of instances to add/remove when scaling"},
		cli.IntFlag{Name: "scale-up-delay", Usage: "minutes to wait for scaling up"},
		cli.IntFlag{Name: "scale-down-delay", Usage: "minutes to wait for scaling down"},
		cli.IntFlag{Name: "scale-up-cpu", Usage: "cpu threshold for scaling up"},
		cli.IntFlag{Name: "scale-down-cpu", Usage: "cpu threshold for scaling down"},
	}

	app := cli.NewApp()
	app.Name = "galaxy"
	app.Usage = "galaxy cli"
	app.Version = buildVersion
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "redis", Value: utils.DefaultRedisHost, Usage: "host:port[,host:port,..]"},
		cli.StringFlag{Name: "env", Value: "", Usage: "environment (dev, test, prod, etc.)"},
		cli.StringFlag{Name: "pool", Value: "", Usage: "pool (web, worker, etc.)"},
	}

	app.Commands = []cli.Command{
		{
			Name:        "init",
			Usage:       "initialize the galaxy infrastructure",
			Action:      stackInit,
			Description: "stack:init <stack_name>",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "template", Usage: "template file"},
				cli.StringFlag{Name: "region", Usage: "AWS Region"},
				cli.BoolFlag{Name: "print", Usage: "print template and exit"},
			},
		},
		{
			Name:        "app",
			Usage:       "list the apps currently created",
			Action:      appList,
			Description: "app",
		},
		{
			Name:        "app:backup",
			Usage:       "backup app configs to a file or stdout",
			Action:      appBackup,
			Description: "app:backup [app[,app2]]",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "file", Usage: "backup filename"},
			},
		},
		{
			Name:        "app:restore",
			Usage:       "restore an app's config",
			Action:      appRestore,
			Description: "app:restore [app[,app2]]",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "file", Usage: "backup filename"},
				cli.BoolFlag{Name: "force", Usage: "force overwrite of existing config"},
			},
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
			Name:        "pool:assign",
			Usage:       "assign an app to a pool",
			Action:      poolAssign,
			Description: "pool:assign",
		},
		{
			Name:        "pool:unassign",
			Usage:       "unassign an app from a pool",
			Action:      poolUnassign,
			Description: "pool:unassign",
		},

		{
			Name:        "pool:create",
			Usage:       "create a pool",
			Action:      poolCreate,
			Description: "pool:create",
			Flags:       stackFlags,
		},
		{
			Name:        "pool:update",
			Usage:       "update a pool's stack",
			Action:      poolUpdate,
			Description: "pool:update",
			Flags:       stackFlags,
		},
		{
			Name:        "pool:delete",
			Usage:       "deletes a pool",
			Action:      poolDelete,
			Description: "pool:delete",
			Flags: []cli.Flag{
				cli.BoolFlag{Name: "y", Usage: "skip confirmation"},
			},
		},
		{
			Name:        "pg:psql",
			Usage:       "connect to database using psql",
			Action:      pgPsql,
			Description: "pg:psql <app>",
		},
		{
			Name:        "stack:template",
			Usage:       "print the cloudformation template to stdout",
			Action:      stackTemplate,
			Description: "stack:template <stack_name>",
		},
		{
			Name:        "stack:update",
			Usage:       "update the base stack directly by name. Requires a template.",
			Action:      stackUpdate,
			Description: "stack:update <stack_name>",
			Flags:       stackFlags,
		},
		{
			Name:        "stack:delete",
			Usage:       "delete a stack",
			Action:      stackDelete,
			Description: "stack:delete <stack_name>",
			Flags: []cli.Flag{
				cli.BoolFlag{Name: "y", Usage: "skip confirmation"},
				cli.StringFlag{Name: "region", Usage: "aws region"},
			},
		},
		{
			Name:        "stack:pool_create",
			Usage:       "create a pool stack directly",
			Action:      stackCreatePool,
			Description: "stack:pool_update",
			Flags:       stackFlags,
		},
		{
			Name:        "stack:pool_update",
			Usage:       "update a pool's stack",
			Action:      stackUpdatePool,
			Description: "stack:pool_update",
			Flags:       stackFlags,
		},
		{
			Name:        "stack:events",
			Usage:       "list recent events for a stack",
			Action:      stackListEvents,
			Description: "stack:events",
		},
		{
			Name:        "stack",
			Usage:       "list all stacks",
			Action:      stackList,
			Description: "stack",
			Flags:       stackFlags,
		},
	}
	app.Run(os.Args)
}
