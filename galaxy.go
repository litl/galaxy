package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/litl/galaxy/commander"
	gconfig "github.com/litl/galaxy/config"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/runtime"
	"github.com/litl/galaxy/utils"

	"github.com/BurntSushi/toml"
	"github.com/codegangsta/cli"
	"github.com/ryanuber/columnize"
)

var (
	serviceRuntime *runtime.ServiceRuntime
	configStore    *gconfig.Store

	initOnce     sync.Once
	buildVersion string
)

var config struct {
	Host string `toml:"host"`
}

func initStore(c *cli.Context) {
	configStore = gconfig.NewStore(uint64(c.Int("ttl")))
	configStore.Connect(utils.GalaxyRedisHost(c))
}

// ensure the registry as a redis host, but only once
func initRuntime(c *cli.Context) {
	serviceRuntime = runtime.NewServiceRuntime(
		configStore,
		"",
		"127.0.0.1",
	)
}

func ensureAppParam(c *cli.Context, command string) string {
	app := c.Args().First()
	if app == "" {
		cli.ShowCommandHelp(c, command)
		log.Fatal("ERROR: app name missing")
	}

	exists, err := appExists(app, utils.GalaxyEnv(c))
	if err != nil {
		log.Fatalf("ERROR: can't deteremine if %s exists: %s", app, err)
	}

	if !exists {
		log.Fatalf("ERROR: %s does not exist. Create it first.", app)
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

func appExists(app, env string) (bool, error) {
	return configStore.AppExists(app, env)
}

func appList(c *cli.Context) {
	initStore(c)
	err := commander.AppList(configStore, utils.GalaxyEnv(c))
	if err != nil {
		log.Fatalf("ERROR: %s", err)
	}
}

func appCreate(c *cli.Context) {
	ensureEnvArg(c)
	initStore(c)

	app := c.Args().First()
	if app == "" {
		cli.ShowCommandHelp(c, "app:create")
		log.Fatal("ERROR: app name missing")
	}

	err := commander.AppCreate(configStore, app, utils.GalaxyEnv(c))
	if err != nil {
		log.Fatalf("ERROR: %s", err)
	}
}

func appDelete(c *cli.Context) {
	ensureEnvArg(c)
	initStore(c)

	app := ensureAppParam(c, "app:delete")

	err := commander.AppDelete(configStore, app, utils.GalaxyEnv(c))
	if err != nil {
		log.Fatalf("ERROR: %s", err)
	}
}

func appDeploy(c *cli.Context) {
	ensureEnvArg(c)
	initStore(c)
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

	err := commander.AppDeploy(configStore, serviceRuntime, app, utils.GalaxyEnv(c), version)
	if err != nil {
		log.Fatalf("ERROR: %s", err)
	}
}

func appRestart(c *cli.Context) {
	initStore(c)

	app := ensureAppParam(c, "app:restart")

	err := commander.AppRestart(configStore, app, utils.GalaxyEnv(c))
	if err != nil {
		log.Fatalf("ERROR: %s", err)
	}
}

func appRun(c *cli.Context) {
	ensureEnvArg(c)
	initStore(c)
	initRuntime(c)

	app := ensureAppParam(c, "app:run")

	if len(c.Args()) < 2 {
		log.Fatalf("ERROR: Missing command to run.")
		return
	}

	err := commander.AppRun(configStore, serviceRuntime, app, utils.GalaxyEnv(c), c.Args()[1:])
	if err != nil {
		log.Fatalf("ERROR: %s", err)
	}
}

func appShell(c *cli.Context) {
	ensureEnvArg(c)
	initStore(c)
	initRuntime(c)

	app := ensureAppParam(c, "app:shell")

	err := commander.AppShell(configStore, serviceRuntime, app,
		utils.GalaxyEnv(c), utils.GalaxyPool(c))
	if err != nil {
		log.Fatalf("ERROR: %s", err)
	}
}

func configList(c *cli.Context) {
	ensureEnvArg(c)
	initStore(c)
	app := ensureAppParam(c, "config")

	err := commander.ConfigList(configStore, app, utils.GalaxyEnv(c))
	if err != nil {
		log.Fatalf("ERROR: Unable to list config: %s.", err)
		return
	}
}

func configSet(c *cli.Context) {
	ensureEnvArg(c)
	initStore(c)
	app := ensureAppParam(c, "config:set")

	args := c.Args().Tail()
	err := commander.ConfigSet(configStore, app, utils.GalaxyEnv(c), args)

	if err != nil {
		log.Fatalf("ERROR: Unable to update config: %s.", err)
		return
	}
}

func configUnset(c *cli.Context) {
	ensureEnvArg(c)
	initStore(c)
	app := ensureAppParam(c, "config:unset")

	err := commander.ConfigUnset(configStore, app, utils.GalaxyEnv(c), c.Args().Tail())
	if err != nil {
		log.Fatalf("ERROR: Unable to unset config: %s.", err)
		return
	}
}

func configGet(c *cli.Context) {
	ensureEnvArg(c)
	initStore(c)
	app := ensureAppParam(c, "config:get")

	err := commander.ConfigGet(configStore, app, utils.GalaxyEnv(c), c.Args().Tail())

	if err != nil {
		log.Fatalf("ERROR: Unable to get config: %s.", err)
		return
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
	initStore(c)

	app := ensureAppParam(c, "pool:assign")

	err := commander.AppAssign(configStore, app, utils.GalaxyEnv(c), utils.GalaxyPool(c))
	if err != nil {
		log.Fatalf("ERROR: %s", err)
	}
}

func poolUnassign(c *cli.Context) {
	ensureEnvArg(c)
	ensurePoolArg(c)
	initStore(c)

	app := c.Args().First()
	if app == "" {
		cli.ShowCommandHelp(c, "pool:assign")
		log.Fatal("ERROR: app name missing")
	}

	err := commander.AppUnassign(configStore, app, utils.GalaxyEnv(c), utils.GalaxyPool(c))
	if err != nil {
		log.Fatalf("ERROR: %s", err)
	}
}

func poolCreate(c *cli.Context) {
	ensureEnvArg(c)
	ensurePoolArg(c)
	initStore(c)
	created, err := configStore.CreatePool(utils.GalaxyPool(c), utils.GalaxyEnv(c))
	if err != nil {
		log.Fatalf("ERROR: Could not create pool: %s", err)
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
	initStore(c)

	envs := []string{utils.GalaxyEnv(c)}
	if utils.GalaxyEnv(c) == "" {
		var err error
		envs, err = configStore.ListEnvs()
		if err != nil {
			log.Fatalf("ERROR: %s", err)
		}
	}

	columns := []string{"ENV | POOL | APPS "}

	for _, env := range envs {
		pools, err := configStore.ListPools(env)
		if err != nil {
			log.Fatalf("ERROR: cannot list pools: %s", err)
			return
		}

		if len(pools) == 0 {
			columns = append(columns, strings.Join([]string{
				env,
				"",
				""}, " | "))
			continue
		}

		for _, pool := range pools {

			assigments, err := configStore.ListAssignments(env, pool)
			if err != nil {
				log.Fatalf("ERROR: cannot list pool assignments: %s", err)
			}

			columns = append(columns, strings.Join([]string{
				env,
				pool,
				strings.Join(assigments, ",")}, " | "))
		}

	}
	output, _ := columnize.SimpleFormat(columns)
	log.Println(output)
}

func poolDelete(c *cli.Context) {
	ensureEnvArg(c)
	ensurePoolArg(c)
	initStore(c)
	empty, err := configStore.DeletePool(utils.GalaxyPool(c), utils.GalaxyEnv(c))
	if err != nil {
		log.Fatalf("ERROR: Could not delete pool: %s", err)
		return
	}

	if empty {
		log.Printf("Pool %s deleted\n", utils.GalaxyPool(c))
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
			log.Fatalf("ERROR: Unable to logout: %s", err)
			return
		}
	}

}

func pgPsql(c *cli.Context) {
	ensureEnvArg(c)
	initStore(c)
	app := ensureAppParam(c, "pg:psql")

	appCfg, err := configStore.GetApp(app, utils.GalaxyEnv(c))
	if err != nil {
		log.Fatalf("ERROR: Unable to run command: %s.", err)
		return
	}

	database_url := appCfg.Env()["DATABASE_URL"]
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

	// Don't print date, etc. and print to stdout
	log.DefaultLogger = log.New(os.Stdout, "", log.INFO)
	log.DefaultLogger.SetFlags(0)

	// declare one superset of flags for stack operations, so we don't pollute the global flags
	// TODO: these need to be broken up into proper sets for each command to
	//       prevent confusing help messages.
	stackFlags := []cli.Flag{
		cli.StringFlag{Name: "base", Usage: "base stack name"},
		cli.StringFlag{Name: "keyname", Usage: "ssh keypair name"},
		cli.StringFlag{Name: "ami", Usage: "ami id"},
		cli.StringFlag{Name: "instance-type", Usage: "instance type"},
		cli.IntFlag{Name: "volume-size", Usage: "stack instance volume size in GB", Value: 100},
		cli.StringFlag{Name: "parameters", Usage: "template parameters in json"},
		cli.StringFlag{Name: "ssl-cert", Usage: "SSL certificate name"},
		cli.StringFlag{Name: "policy", Usage: "stack policy"},
		cli.StringFlag{Name: "region", Usage: "aws region"},
		cli.IntFlag{Name: "availability-zones", Usage: "number of availability zones to run a pool in"},
		cli.BoolFlag{Name: "elb", Usage: "add an ELB when creating a stack"},
		cli.StringFlag{Name: "http-health-check", Usage: "ELB health check address", Value: "HTTP:9090/_config"},
		cli.IntFlag{Name: "http-port", Usage: "instance http port for ELB listeners"},
		cli.StringFlag{Name: "template", Usage: "provide a template file"},
		cli.IntFlag{Name: "min-size", Usage: "minimum pool size"},
		cli.IntFlag{Name: "max-size", Usage: "maximum pool size"},
		cli.IntFlag{Name: "desired-size", Usage: "desired pool size"},
		cli.BoolFlag{Name: "print", Usage: "print new template and exit [noop]"},
		cli.BoolFlag{Name: "auto-update", Usage: "add an ASG UpdatePolicy"},
		cli.IntFlag{Name: "scale-adj", Usage: "number of instances to add/remove when scaling"},
		cli.IntFlag{Name: "scale-up-delay", Usage: "minutes to wait for scaling up"},
		cli.IntFlag{Name: "scale-down-delay", Usage: "minutes to wait for scaling down"},
		cli.IntFlag{Name: "scale-up-cpu", Usage: "cpu threshold for scaling up"},
		cli.IntFlag{Name: "scale-down-cpu", Usage: "cpu threshold for scaling down"},
		cli.IntFlag{Name: "update-min", Usage: "minimum instances in service during auto-update", Value: 1},
		cli.IntFlag{Name: "update-batch", Usage: "max instance instances to auto-update at once", Value: 1},
		cli.DurationFlag{Name: "update-pause", Usage: "Pause time between auto-update actions (0s-5m30s)", Value: 5 * time.Minute},
	}

	app := cli.NewApp()
	app.Name = "galaxy"
	app.Usage = "galaxy cli"
	app.Version = buildVersion
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "registry", Value: "", Usage: "host:port[,host:port,..]"},
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
			Description: "stack:pool_create",
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
