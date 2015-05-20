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

	// TODO: still merging these backends
	// these are brought in from the RegistryBackend
	// Keys
	Keys(key string) ([]string, error)
	Delete(key string) (int, error)
	Expire(key string, ttl uint64) (int, error)
	TTL(key string) (int, error)

	// Maps
	Set(key, field string, value string) (string, error)
	Get(key, field string) (string, error)

	connect()
	reconnect()
}
