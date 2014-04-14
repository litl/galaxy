shuttle TCP Proxy
=======

Shuttle is a TCP proxy and load balancer, which can be updated live via an HTTP
interface.

## Usage

Shuttle's external dependencies consist of "github.com/gorilla/mux" and
"launchpad.net/gocheck", the latter required only for testing.


Shuttle can be started with a default configuration, as well as its last
configuration state. The -state configuration is updated on changes to the
internal config. If the state config file doesn't exist, the default is loaded.
The default config is never written to by shuttle.

    $ ./shuttle -config default_config.json -state state_config.json -http 127.0.0.1:9090


The current config can be queried via the `/config` endpoint. This returns a
json list of Services and their Backends, which can be saved directly as a
config file. The configuration itself is a list of Services, each of which may
contain a list of backends.

A GET request to the path `/` returns an extended json config containing live
stats from all Services. Individual services can be queried by their name,
`/service_name`, returning just the json stats for that service. Backend stats
can be queried directly as well via the path `service_name/backend_name`.

Issuing a PUT with a json config to the service's endpoint will create, or
replace that service. The listening port will be shutdown during this process,
and existing backends will not be transferred to the new config. Backends can
be included in the service's json, and configured separately.

Issuing a PUT with a json config to the backend's endpoint will create or
replace that backend. Existing connections relying on the old config will
continue to run until the connection is closed.

## TODO

- Connection limits (per service and/or per backend)
- UDP proxy
- Mark backend down after non-check connection failures
- Health check via http, or tcp call/resp pattern.
- Partial config updates without overwriting everything.
- Protocol bridging? e.g. `TCP<->unix`
- better logging
- User switching? (though we couldn't rebind privileged ports after switching)
