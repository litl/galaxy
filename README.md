galaxy
======

*Woven service platform*

![galaxy](logo.jpg)

===

The project handles deployment of services in docker containers, registration of those
services using redis, discovery and proxying of registered services.

There are three sub-projects: commander, discovery and shuttle.

  * Commander - deploys service containers.
  * Discovery - registers and discovers services containers through the docker API and redis. It
    configures routes using the Shuttle API.
  * Shuttle - A TCP proxy that can be configured through a HTTP based API.

=== Dev Setup

You need to have a docker 0.9+ env available.  Set that up w/ boot2docker or use the provided
vagrant file.

1. Install vagrant 1.5.2
2. Install virtualbox 4.3.10
3. vagrant up
4. vagrant ssh
5. cd /vagrant
6. godep restore
7. make
8. goreman start


