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
  * Shuttle - An HTTP/TCP proxy that can be configured through a HTTP based API.

## Dev Setup

You need to have a docker 1.1.2+ env available.  Set that up w/ boot2docker or use the provided
vagrant file.

1. Install (glock)[https://github.com/robfig/glock]
2. make deps
3. make



