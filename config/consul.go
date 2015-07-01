package config

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"sync"
	"time"

	"github.com/litl/galaxy/log"

	consul "github.com/hashicorp/consul/api"
)

/*
TODO: logging!

TODO: use CAS operations so that we don't have any races between
      configuration changes

The consul tree looks like:
	galaxy/apps/env/app_name
	galaxy/pools/env/pool_name
	galaxy/hosts/env/pool/host_ip
	galaxy/services/env/pool/host_ip/service_name/container_id

The Methods for ConsulBackend are tentatively defined here to satisfy the
confing.Backend interface, and may not be appropriate
*/
type ConsulBackend struct {
	client *consul.Client

	// We always need a session to set the TTL for keys
	sessionID string

	// stop any backend goroutined
	done chan struct{}

	// filter events we've already seen
	seen *eventCache
}

var UnknownApp = fmt.Errorf("unkown app")

func NewConsulBackend() *ConsulBackend {
	client, err := consul.NewClient(consul.DefaultConfig())
	if err != nil {
		// this shouldn't ever error with the default config
		panic(err)
	}

	node, err := client.Agent().NodeName()
	if err != nil {
		log.Fatal(err)
	}

	// find an existing galaxy session if one exists, or create a new one
	sessions, _, err := client.Session().Node(node, nil)
	if err != nil {
		log.Fatal(err)
	}

	var session *consul.SessionEntry
	for _, s := range sessions {
		if s.Name == "galaxy" {
			session = s
			break
		}
	}

	// we have a session, now make sure we can renew it so it doesn't expire
	// before we start running
	if session != nil {
		session, _, err = client.Session().Renew(session.ID, nil)
		if err != nil {
			log.Debug("error renewing galaxy session:", err)
		}
	}

	// no existing session, so create a new one
	if session == nil {
		session = &consul.SessionEntry{
			Name:     "galaxy",
			Behavior: "delete",
			TTL:      "15s",
		}

		session.ID, _, err = client.Session().Create(session, nil)
		if err != nil {
			// we can't continue without a session for key TTLs
			log.Fatal(err)
		}
	}

	// keep our session alive in the background
	done := make(chan struct{})
	go client.Session().RenewPeriodic("10s", session.ID, nil, done)

	return &ConsulBackend{
		client:    client,
		sessionID: session.ID,
		done:      done,
		seen: &eventCache{
			seen: make(map[string]uint64),
		},
	}
}

// Check that an app exists and has a config
func (c *ConsulBackend) AppExists(app, env string) (bool, error) {
	appCfg, err := c.GetApp(app, env)
	if err != nil {
		if err == UnknownApp {
			return false, nil
		}
		return false, err
	}

	if appCfg == nil {
		return false, nil
	}
	return true, nil
}

// Create and save an empty AppDefinition for a new app
func (c *ConsulBackend) CreateApp(app, env string) (bool, error) {
	kvp := &consul.KVPair{}
	kvp.Key = path.Join("galaxy", "apps", env, app)

	// TODO: intit this in one place
	emptyConfig := &AppDefinition{
		AppName:     app,
		Environment: make(map[string]string),
	}

	var err error
	kvp.Value, err = json.Marshal(emptyConfig)
	if err != nil {
		return false, err
	}

	_, err = c.client.KV().Put(kvp, nil)
	if err != nil {
		return false, err
	}

	return true, nil
}

// List all apps in an environment
func (c *ConsulBackend) ListApps(env string) ([]App, error) {
	key := path.Join("galaxy", "apps", env)
	kvPairs, _, err := c.client.KV().List(key, nil)
	if err != nil {
		return nil, err
	}

	apps := make([]App, len(kvPairs))
	for i, kvp := range kvPairs {
		ad := &AppDefinition{}
		err := json.Unmarshal(kvp.Value, ad)
		if err != nil {
			log.Println("error decoding AppDefinition for %s: %s", kvp.Key, err.Error())
			continue
		}
		ad.ConfigIndex = int64(kvp.ModifyIndex)
		apps[i] = App(ad)
	}
	return apps, nil
}

// Retrieve the current config for an application
func (c *ConsulBackend) GetApp(app, env string) (App, error) {
	key := path.Join("galaxy", "apps", env, app)
	kvp, _, err := c.client.KV().Get(key, nil)
	if err != nil {
		return nil, err
	}

	if kvp == nil {
		return nil, UnknownApp
	}

	ad := &AppDefinition{}
	err = json.Unmarshal(kvp.Value, ad)
	if err != nil {
		return nil, err
	}

	ad.ConfigIndex = int64(kvp.ModifyIndex)
	return ad, nil
}

// Update the current configuration for an app
func (c *ConsulBackend) UpdateApp(app App, env string) (bool, error) {
	ad := app.(*AppDefinition)
	key := path.Join("galaxy", "apps", env, ad.Name())
	kvp := &consul.KVPair{Key: key}

	var err error
	kvp.Value, err = json.Marshal(ad)
	if err != nil {
		return false, err
	}

	_, err = c.client.KV().Put(kvp, nil)
	if err != nil {
		return false, err
	}
	return true, nil
}

// Delete the configuration for an app
// FIXME: Why does this take an App? Everything else takes a string
func (c *ConsulBackend) DeleteApp(app App, env string) (bool, error) {
	key := path.Join("galaxy", "apps", env, app.Name())
	_, err := c.client.KV().Delete(key, nil)
	if err != nil {
		return false, err
	}

	return true, nil
}

// Add a pool assignment for this app, and update the config.
// The pool need not exist, it just won't run until there is a corresponding
// pool.
func (c *ConsulBackend) AssignApp(app, env, pool string) (bool, error) {
	appCfg, err := c.GetApp(app, env)
	if err != nil {
		return false, err
	}

	ad := appCfg.(*AppDefinition)
	// return early if we're already assigned to this pool
	for i := range ad.Assignments {
		if ad.Assignments[i].Pool == pool {
			return true, nil
		}
	}

	// FIXME: we have to hard-code -1 here to match old behavior
	a := AppAssignment{Pool: pool, Instances: -1}
	ad.Assignments = append(ad.Assignments, a)
	return c.UpdateApp(ad, env)
}

// Remove a pool assignment for this app, and update the config
func (c *ConsulBackend) UnassignApp(app, env, pool string) (bool, error) {
	found := false
	appCfg, err := c.GetApp(app, env)
	if err != nil {
		return false, err
	}

	ad := appCfg.(*AppDefinition)
	for i := range ad.Assignments {
		if ad.Assignments[i].Pool == pool {
			// remove the item for the slice
			a := ad.Assignments
			a[i], a = a[len(a)-1], a[:len(a)-1]
			ad.Assignments = a
			found = true
			break
		}
	}

	if !found {
		return false, nil
	}

	ok, err := c.UpdateApp(ad, env)
	return found && ok, err
}

// List apps assigned to a pool
func (c *ConsulBackend) ListAssignments(env, pool string) ([]string, error) {
	apps, err := c.ListApps(env)
	if err != nil {
		return nil, err
	}

	assigned := []string{}
	for _, app := range apps {
		for _, a := range app.(*AppDefinition).Assignments {
			if a.Pool == pool {
				assigned = append(assigned, app.Name())
			}
		}
	}

	return assigned, nil
}

// Create a pool entry
// Pool are just an empty Key/Value pair, to signify that this pool has been
// purposely created.
func (c *ConsulBackend) CreatePool(env, pool string) (bool, error) {
	key := path.Join("galaxy", "pools", env, pool)
	kvp := &consul.KVPair{Key: key}
	_, err := c.client.KV().Put(kvp, nil)
	if err != nil {
		return false, err
	}
	return true, nil
}

// Delete the pool entry
func (c *ConsulBackend) DeletePool(env, pool string) (bool, error) {
	key := path.Join("galaxy", "pools", env, pool)
	_, err := c.client.KV().DeleteTree(key, nil)
	if err != nil {
		return false, err
	}
	return true, nil
}

// List all pools in an environment
func (c *ConsulBackend) ListPools(env string) ([]string, error) {
	prefix := path.Join("galaxy", "pools", env) + "/"
	keys, _, err := c.client.KV().Keys(prefix, "/", nil)
	if err != nil {
		return nil, err
	}

	pools := make([]string, len(keys))
	for i, key := range keys {
		pools[i] = path.Base(key)
	}
	return pools, nil
}

func (c *ConsulBackend) ListEnvs() ([]string, error) {
	prefix := path.Join("galaxy", "apps") + "/"
	keys, _, err := c.client.KV().Keys(prefix, "/", nil)
	if err != nil {
		return nil, err
	}

	envs := make([]string, len(keys))
	for i, key := range keys {
		envs[i] = path.Base(key)
	}

	return envs, nil
}

// TODO: We need to keep the hosts entries for now, to easily correlate the
//       host with the env and pool, and to schedule apps on appropriate hosts.
//       Rename appropriately to reflect that this only adds a host, and
//       there's nothing to update.
func (c *ConsulBackend) UpdateHost(env, pool string, host HostInfo) error {
	key := path.Join("galaxy", "hosts", env, pool, host.HostIP)
	// lookup the SessionID of this host
	kvp, _, err := c.client.KV().Get(key, nil)
	if err != nil {
		return err
	}

	if kvp == nil {
		// new host, add the key and acquire the lock for TTL
		kvp = &consul.KVPair{
			Key:     key,
			Session: c.sessionID,
		}

		if kvp.Value, err = json.Marshal(host); err != nil {
			return err
		}

		if _, err = c.client.KV().Put(kvp, nil); err != nil {
			return err
		}

		if _, _, err = c.client.KV().Acquire(kvp, nil); err != nil {
			return err
		}
	}

	if kvp.Session == "" {
		err := fmt.Errorf("Host %s has no session!", key)
		return err
	}

	return nil
}

func (c *ConsulBackend) ListHosts(env, pool string) ([]HostInfo, error) {
	prefix := path.Join("galaxy", "hosts", env, pool) + "/"
	keys, _, err := c.client.KV().Keys(prefix, "/", nil)
	if err != nil {
		return nil, err
	}

	hosts := make([]HostInfo, len(keys))
	for i, key := range keys {
		hosts[i].HostIP = path.Base(key)
	}
	return hosts, nil
}

func (c *ConsulBackend) DeleteHost(env, pool string, host HostInfo) error {
	key := path.Join("galaxy", "hosts", env, pool, host.HostIP)
	_, err := c.client.KV().Delete(key, nil)
	return err
}

// FIXME: the int return value is useless here, and not used on the redis
//        backend either.
func (c *ConsulBackend) Notify(key, value string) (int, error) {
	event := &consul.UserEvent{
		Name:    key,
		Payload: []byte(value),
	}

	_, _, err := c.client.Event().Fire(event, nil)
	if err != nil {
		return 0, err
	}
	return 1, nil
}

func (c *ConsulBackend) Subscribe(key string) chan string {
	msgs := make(chan string)
	go c.sub(key, msgs)
	return msgs
}

// FIXME: This can't be shut down. Make sure that's not a problem
func (c *ConsulBackend) sub(key string, msgs chan string) {
	var events []*consul.UserEvent
	var meta *consul.QueryMeta
	var err error
	for {
		// No way to handle failure here, just keep trying to get our first set of events.
		// We need a successful query to get the last index to search from.
		events, meta, err = c.client.Event().List(key, nil)
		if err != nil {
			log.Println("Subscribe error:", err)
			time.Sleep(5 * time.Second)
			continue
		}
		// cache all old events
		c.seen.Filter(events)
		break
	}

	lastIndex := meta.LastIndex
	for {
		opts := &consul.QueryOptions{
			WaitIndex: lastIndex,
			WaitTime:  30 * time.Second,
		}
		events, meta, err = c.client.Event().List(key, opts)
		if err != nil {
			log.Printf("Subscribe(%s): %s\n", key, err.Error())
			continue
		}

		if meta.LastIndex == lastIndex {
			// no new events
			continue
		}

		for _, event := range c.seen.Filter(events) {
			msgs <- string(event.Payload)
		}

		lastIndex = meta.LastIndex
	}
}

// Marshal a ServiceRegistry in consul, and associate it with a session so it
// is deleted on expiration.
func (c *ConsulBackend) RegisterService(env, pool string, reg *ServiceRegistration) error {
	key := path.Join("galaxy", "services", env, pool, reg.ExternalIP, reg.Name, reg.ContainerID[0:12])

	// check for an existing value, so we don't try to re-acquire the lock
	existing, _, err := c.client.KV().Get(key, nil)
	if err != nil {
		return err
	}

	if existing != nil && existing.Session == c.sessionID {
		// already registered
		return nil
	}

	kvp := &consul.KVPair{
		Key:     key,
		Session: c.sessionID,
	}

	kvp.Value, err = json.Marshal(reg)
	if err != nil {
		return err
	}

	_, err = c.client.KV().Put(kvp, nil)
	if err != nil {
		return err
	}

	// you need to acquire a lock with the key in order for the key to be
	// associated with the session.
	ok, _, err := c.client.KV().Acquire(kvp, nil)
	if err != nil {
		return err
	}

	if !ok {
		// TODO: is this a re-Acquire, or another Failure
		return fmt.Errorf("Lock failed on Key:%s Session:%s", kvp.Key, kvp.Session)
	}

	return nil
}

// TODO: do we need to return a *ServiceRegistration?
func (c *ConsulBackend) UnregisterService(env, pool, hostIP, name, containerID string) (*ServiceRegistration, error) {
	key := path.Join("galaxy", "services", env, pool, hostIP, name, containerID[0:12])

	registration, err := c.GetServiceRegistration(env, pool, hostIP, name, containerID)
	if err != nil || registration == nil {
		return registration, err
	}

	if registration.ContainerID != containerID {
		return nil, nil
	}

	_, err = c.client.KV().Delete(key, nil)
	if err != nil {
		return registration, err
	}

	return registration, nil
}

func (c *ConsulBackend) GetServiceRegistration(env, pool, hostIP, name, containerID string) (*ServiceRegistration, error) {
	key := path.Join("galaxy", "services", env, pool, hostIP, name, containerID[0:12])

	existingRegistration := ServiceRegistration{
		Path: key,
	}

	kvp, _, err := c.client.KV().Get(key, nil)
	if err != nil {
		return nil, err
	}

	if kvp == nil {
		return nil, nil
	}

	err = json.Unmarshal(kvp.Value, &existingRegistration)
	if err != nil {
		return nil, err
	}

	// FIXME: this is a fake Expires, set to the default TTL.
	// There's no easy way to get the session expiration, and does it really
	// matter, since it can't be longer than TTL?
	existingRegistration.Expires = time.Now().UTC().Add(time.Duration(DefaultTTL) * time.Second)
	return &existingRegistration, nil
}

func (c *ConsulBackend) ListRegistrations(env string) ([]ServiceRegistration, error) {
	prefix := path.Join("galaxy", "services", env)

	kvPairs, _, err := c.client.KV().List(prefix, nil)
	if err != nil {
		return nil, err
	}

	regList := []ServiceRegistration{}
	for _, kvp := range kvPairs {
		svcReg := ServiceRegistration{
			Name: path.Base(kvp.Key),
		}
		err = json.Unmarshal(kvp.Value, &svcReg)
		if err != nil {
			log.Warnf("WARN: Unable to unmarshal JSON for %s: %s", kvp.Key, err)
			continue
		}

		regList = append(regList, svcReg)
	}

	return regList, nil
}

// Required for the interface, but not used by consul
func (c *ConsulBackend) connect()   {}
func (c *ConsulBackend) reconnect() {}

var _ Backend = &ConsulBackend{}

// we need to de-dupe consul events, as up to 256 events may be returned
type eventCache struct {
	sync.Mutex
	// map the ID to the LTime, that way we can check for duplicate IDs, and purge based on LTime
	seen map[string]uint64
}

// Return only events we haven't seen
func (c *eventCache) Filter(events []*consul.UserEvent) []*consul.UserEvent {
	c.Lock()
	defer c.Unlock()

	var newEvents []*consul.UserEvent

	for _, e := range events {
		_, ok := c.seen[e.ID]
		if !ok {
			newEvents = append(newEvents, e)
			c.seen[e.ID] = e.LTime
		}
	}

	// purge the cache occasionally
	// the agent can't return events older what we just saw, so prune the cache
	// to just the most recent events.
	if len(c.seen) > 2*len(events) {
		c.prune(len(events))
	}

	return newEvents
}

// Prune all but the newest `size` events.
func (c *eventCache) prune(size int) {
	events := make([]event, 0)
	for id, ltime := range c.seen {
		events = append(events, event{id: id, ltime: ltime})
	}

	sort.Sort(byLTime(events))

	for _, event := range events[:len(events)-size] {
		delete(c.seen, event.id)
	}
}

type byLTime []event

func (a byLTime) Len() int           { return len(a) }
func (a byLTime) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byLTime) Less(i, j int) bool { return a[i].ltime < a[j].ltime }

type event struct {
	id    string
	ltime uint64
}
