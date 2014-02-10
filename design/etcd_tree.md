The tree in etcd stores configuration and runtime state about deployed
services.

The first level designates and env:
  /dev
  /stage
  /prod

The second level designates a pool name.  All hosts within a pool are
configured identically.  In AWS, the pool name will correspond to an
auto-scaling group.  The "web" pool is special pool that will be tied
to an ELB and make the services within that pool publicly accessible.

  /dev/web
  /dev/worker

The third level contains a service name which holds the services
configuration under a "config" entry and a "hosts" entry with the hosts currently
live and registerred in that pool.  The "version" entry is the docker
image version that should be deployed.  (NOTE: We'll probably have
a current,next,previous_version to make deployment more atomic)

  /dev/web/apiary/version
  /dev/web/apiary/config = {"DATABASE_URL":"postgres://.."}

Under the third level "hosts" entry, there is an entry per host.  The
key is the hostname.  Under the there is an service name entry which
contains two keys: location and config. "location" is JSON dict with four
entries that are created when the service is registered:

  /dev/web/hosts/i-abc1234/apiary/location =
    {"EXTERNAL_IP": "10.0.1.2",
     "EXTERNAL_PORT": 6000,
     "INTERNAL_IP": "172.0.1.2",
     "INTERNAL_PORT": 49123}

  EXTERNAL_IP is the hosts LAN IP.
  EXTERNAL_PORT is the hosts port that this service can be reached on from the
  LAN.
  INTERNAL_IP is the docker container IP for the service.
  INTERNAL_PORT is the docker container port that the service is listening on.

These entries are used by the discovery service to configure the local haproxy
instance to route local ports to external hosts.

"config" is the configuration (environment variables) that were set when the service
was started.  They should be a copy of /dev/web/apiary/config but could be different
if something failed.

Sample tree:

/dev
  /web
    /apiary
      version = registery.w.n/apiary:20140101.1
      config = {"DATABASE_URL": "postgres://...",
                 "PORT": "6000"}
    /hosts
      /i-abc1234
        /apiary/location = {"EXTERNAL_IP": "10.0.1.2",
                            "EXTERNAL_PORT": 6000,
                            "INTERNAL_IP": "172.0.1.2",
                            "INTERNAL_PORT": 49123}
  /worker
    /honeycomb
      version = registry.w.n/honeycomb:20141212.1
      config = {"DATABASE_URL": "postgres://...",
                "PORT = 7000"}
    /grus
      version = registry.w.n/grus:20141212.1
      config = {"DATABASE_URL": "postgres://...",
                "THREADS": "5",
                "PORT", "8000"}
    /hosts
      /i-xyz1234
        /honeycomb/location = {"EXTERNAL_IP": "10.0.1.5",
                               "EXTERNAL_PORT": 7000,
                               "INTERNAL_IP": "172.0.1.4",
                               "INTERNAL_PORT": 34023}
        /grus/location = {"EXTERNAL_IP": "10.0.1.5",
                          "EXTERNAL_PORT", 8000,
                          "INTERNAL_IP": "172.0.1.23",
                          "INTERNAL_PORT": 21235}
/prod
  /...