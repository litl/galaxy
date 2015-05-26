package config

type Backend interface {
	// Apps
	AppExists(app, env string) (bool, error)
	CreateApp(app, env string) (bool, error)
	ListApps(env string) ([]App, error)
	GetApp(app, env string) (App, error)
	UpdateApp(svcCfg App, env string) (bool, error)
	DeleteApp(svcCfg App, env string) (bool, error)

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

	// Registration
	RegisterService(env, pool string, reg *ServiceRegistration) error
	UnregisterService(env, pool, hostIP, name, containerID string) (*ServiceRegistration, error)
	GetServiceRegistration(env, pool, hostIP, name, containerID string) (*ServiceRegistration, error)
	ListRegistrations(env string) ([]ServiceRegistration, error)

	connect()
	reconnect()
}
