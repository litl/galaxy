galaxy
======

*Woven service platform*

![galaxy](logo.jpg)

===

The project handles deployment of services in docker containers, registration of those
services w/ etcd, discovery and proxying of registered services through etcd and haproxy.

There will be two sub-projects tentatively named: commander and shuttle.  Commander deploys and
registers service containers and shuttle discovers and proxies connections to containers.