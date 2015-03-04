galaxy
======

*Docker Micro-PaaS*

![galaxy](logo.jpg)

===

The project handles the deployment, configuration and orchestration of Docker containers across
multiple hosts.  It is designed for running 12-factor style, stateless, microservices
within Docker containers while being lightweight and simple to operate.

### Features:

* Minimal dependencies (two binaries and redis)
* Automatic service registration, discovery and proxying of registered services.
* Virtual Host HTTP(S) proxying
* Container scheduling and scaling across hosts
* Heroku style config variable interface
* Container contraints (CPU/Mem)

There are two sub-projects: commander and shuttle.

  * Commander - Container deployment and service discovery.
  * Shuttle - An HTTP/TCP proxy that can be configured through a HTTP based API.

## Dev Setup

You need to have a docker 1.1.2+ env available.  Set that up w/ boot2docker or use the provided
vagrant file.

1. Install [glock](https://github.com/robfig/glock)
2. make deps
3. make

## License

MIT

