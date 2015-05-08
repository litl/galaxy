galaxy
======

*Docker Micro-PaaS*

![galaxy](logo.jpg)

===

galaxy is a micro-paas designed for running 12-factor style, stateless, microservices
within Docker containers while being lightweight and simple to operate.  It handles the deployment, 
configuration and orchestration of Docker containers across multiple hosts.  

It is ideally suited for running Docker containers:
* Alongside existing applications while transitioning to containers
* On clusters of 10's-100's of hosts
* On existing or new infrastructure you are already using
* For HTTP based micro-services

### Features:

* Minimal dependencies (two binaries and redis)
* Automatic service registration, discovery and proxying of registered services.
* Virtual Host HTTP(S) proxying
* Container scheduling and scaling across hosts
* Heroku style config variable interface
* Container contraints (CPU/Mem)

There are two sub-projects: commander and shuttle.

  * Commander - Container deployment and service discovery.
  * [Shuttle](https://github.com/litl/shuttle) - An HTTP/TCP proxy that can be configured through a HTTP based API.

## Getting Started

(These assume that Docker is running on your local host.  If you're
running it remotely, or via something like boot2docker, you'll need to
adjust the `GALAXY_REGISTRY_URL` below and provide the `-host-ip`
argument to `commander agent`.)

To setup a single host environment, run the following:

```
$ docker run -d --name redis -p 6379:6379 redis
$ export GALAXY_REGISTRY_URL=redis://127.0.0.1:6379
$ export GALAXY_ENV=local
$ export GALAXY_POOL=web
$ commander agent
```

To create a new app for _nginx_:

```
$ commander app:create nginx
```

To deploy a latest official nginx image to our _nginx_ app:

```
$ commander app:deploy nginx nginx
```

Finally, we need to assign this app to our default `web` pool:

```
$ commander app:assign nginx
```

You should see nginx started by the `commander agent` process.

## Exposing Services

To expose the nginx app, we need to run shuttle to handle request routing:

Start shuttle:

```
$ shuttle -http 0.0.0.0:8080
```

This starts a shuttle with an admin server on 127.0.0.1:9090 (the
default) and an HTTP server on 0.0.0.0:8080.

Next, stop your old commander agent and restart it, pointing to the
shuttle admin address:

```
$ commander -shuttle-addr 127.0.0.1:9090 agent
```

Assign a service port to nginx:

```
$ commander runtime:set -port 8888 nginx
```

You should now be able to access the nginx app on host port 8888:
```
$ curl localhost:8888
```

Add a virtual host:
```
$ commander runtime:set -vhost my.domain nginx
$ curl -v my.domain:8080
```

## Dev Setup

You need to have a docker 1.4.1+ and golang 1.4. 

1. Install [glock](https://github.com/robfig/glock)
2. make deps
3. make

## License

MIT

