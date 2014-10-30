package config

type Backend interface {
	// Apps
	AppExists(app, env string) (bool, error)
	CreateApp(app, env string) (bool, error)
	ListApps(env string) ([]*AppConfig, error)
	GetApp(app, env string) (*AppConfig, error)
	UpdateApp(svcCfg *AppConfig, env string) (bool, error)
	DeleteApp(svcCfg *AppConfig, env string) (bool, error)

	// Pools
	AssignApp(app, env, pool string) (bool, error)
	UnassignApp(app, env, pool string) (bool, error)
	ListAssignments(env, pool string) ([]string, error)
	CreatePool(env, pool string) (bool, error)
	DeletePool(env, pool string) (bool, error)
	ListPools(env string) ([]string, error)

	// Envs
	ListEnvs() ([]string, error)

	// Host
	UpdateHost(env, pool string, host HostInfo) error
	ListHosts(env, pool string) ([]HostInfo, error)
	DeleteHost(env, pool string, host HostInfo) error

	//Pub/Sub
	Subscribe(key string) chan string
	Notify(key, value string) (int, error)

	Connect()
	Reconnect()
}
