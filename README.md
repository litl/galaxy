galaxy
======

*Woven service platform*

![galaxy](logo.jpg)

===

The project handles deployment of services in docker containers, registration of those
services w/ etcd, discovery and proxying of registered services through etcd and haproxy.

There will be two sub-projects tentatively named: commander and shuttle.  Commander deploys and
registers service containers and shuttle discovers and proxies connections to containers.

=== Dev Setup

You need to have a docker 0.8 env available.  Set that up w/ boot2docker, vagrant, etc..

1.  Start etcd
`docker run -i -p 127.0.0.1:4001:4001 -p 7001:7001 -d -t coreos/etcd`
2. Add a service VERSION
`curl -v -L http://127.0.0.1:4001/v2/keys/dev/web/beanstalkd/VERSION -XPUT -d value="registry.wovops.net/beanstalkd:latest"`
3. Add service env vars
`curl -v -L http://172.17.0.3:4001/v2/keys/dev/web/beanstalkd/PORT -XPUT -d value="12345"`
4. Run commander in foreground
`go install github.com/litl/galaxy/commander && commander`