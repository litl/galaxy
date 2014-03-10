Tentative list of galaxy commands

====

login - connects to a remote host and configures the local env to send comamands remotely
logout - disconnect from a remote host

pool - list the pools available and what apps are running on them
pool:scale - grows/shrinks the pool and any apps running on the them
pool:create - create a new pool
pool:delete - delete an existing pool
pool:ssh - ssh into a host in the pool


app - list apps
app:deploy - deploy a new version of an app
app:restart - restart an app
app:create - create an app
app:delete - delete an app
app:status - dispaly the various info about an app


config - list config values for an app. (env vars)
config:set - set a config value for a app
config:get - get a config value for a app
config:unset - removes a config value for an app

certs - SSL cert commands... (TBD)

ssh - ssh key related tasks (TBD)
ssh:add
ssh:delete
ssh:update





