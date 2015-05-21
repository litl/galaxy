package config

import (
	"fmt"
	"strconv"
)

// AppDefintiion contains all the configuration needed to run a container
// in the galaxy environment.
type AppDefinition struct {
	// Version of this structure in the config storage as of the last operation
	// In consul, this would correspond to `ModifyIndex`
	ConfigIndex int64

	// ("Name" is taken by the interface getter)
	AppName string

	// Image is the specific docker image to be run.
	Image string

	// Docker Image ID
	// If "Image" does not contain a tag, or uses "latest", we need a way to
	// know what version we're running.
	ImageID string

	// PortMappings defines how ports are mapped from the host to the docker
	// container.
	PortMappings map[string]PortMapping

	// Hosts entries to insert into /etc/hosts inside the container
	Hosts []HostsEntry

	// A set of custom DNS servers for the container
	DNS []string

	// Entry point arguments for the container
	EntryPoint []string

	// Command arguments for the container
	Command []string

	// The environment passed to the container
	Environment map[string]string

	// Resources are assigned per logical group, e.g. Pool
	// TODO: This seems awkward -- apps don't know about the env they are
	//       assigned to, but they need to know about the pools.
	//       This is needed while refactoring though, as all the resource
	//       limits are assigned through the config, and rely on the pool.
	Assignments []AppAssignment
}

type HostsEntry struct {
	Address string
	Host    string
}

type PortMapping struct {
	// HostPort is the port that will be bound to the host, directly or through
	// a proxy.
	HostPort string

	// ContainerPort is the port exposed in the docker image
	ContainerPort string

	// Network defines the transport used for this port. TCP is the default if
	// not set.
	Network string

	// Hostnames that can can be routed to this HostPort via a virtual host
	// http handler
	Hostnames []string

	// Predefined error pages to return if a backend returns an error, or is
	// unavailable when access through an http virtual host.
	ErrorPages map[int]string
}

// AppAssignment provides the location and resource limits for an app to run
type AppAssignment struct {
	//  We currently only assign to Pools
	Pool string

	// Name of assigned App
	App string

	// Docker CPU share constraint: 0-1024
	// The default is 0, meaning unconstrained.
	CPU int

	// Docker Memory limit (<number><optional unit>, where unit = b, k, m or g)
	Memory string

	// MemorySwap is the total memory limit (memory + swap, format:
	// <number><optional unit>, where unit = b, k, m or g)
	MemorySwap string

	// Number of instances to run across all hosts in this grouping
	Instances int

	// Minimum number of instances to keep running during a deploy or restart.
	// Default is 1 if Instances is > 1, else 0.
	MinInstances int
}

//
// Below are all methods to make an AppDefinition implement the existing App interface

// FIXME: We may need to save after every operation for now, since things may
//        depend on the ID() updating automatically, which was tied to the
//        underlying VMap of the redis config.
func (a *AppDefinition) Name() string {
	return a.AppName
}

func (a *AppDefinition) Env() map[string]string {
	return a.Environment
}

func (a *AppDefinition) EnvSet(key, value string) {
	a.Environment[key] = value
}

func (a *AppDefinition) EnvGet(key string) string {
	return a.Environment[key]
}

func (a *AppDefinition) Version() string {
	return a.Image
}

func (a *AppDefinition) SetVersion(version string) {
	a.Image = version
}

func (a *AppDefinition) VersionID() string {
	return a.ImageID
}

func (a *AppDefinition) SetVersionID(versionID string) {
	a.ImageID = versionID
}

func (a *AppDefinition) ID() int64 {
	return a.ConfigIndex
}

func (a *AppDefinition) ContainerName() string {
	return fmt.Sprintf("%s_%s", a.Name(), a.ID())
}

func (a *AppDefinition) SetProcesses(pool string, count int) {
	i := a.assignment(pool)
	a.Assignments[i].Instances = count
}

func (a *AppDefinition) GetProcesses(pool string) int {
	i := a.assignment(pool)
	return a.Assignments[i].Instances
}

func (a *AppDefinition) RuntimePools() []string {
	pools := []string{}
	for _, as := range a.Assignments {
		pools = append(pools, as.Pool)
	}
	return pools
}

func (a *AppDefinition) SetMemory(pool string, mem string) {
	i := a.assignment(pool)
	a.Assignments[i].Memory = mem
}

func (a *AppDefinition) GetMemory(pool string) string {
	i := a.assignment(pool)
	return a.Assignments[i].Memory
}

func (a *AppDefinition) SetCPUShares(pool string, cpu string) {
	i := a.assignment(pool)
	a.Assignments[i].CPU, _ = strconv.Atoi(cpu)
}

func (a *AppDefinition) GetCPUShares(pool string) string {
	i := a.assignment(pool)
	return strconv.Itoa(a.Assignments[i].CPU)
}

// TODO: This is to make it easier to refactor in this new config.
//       Might want to rework this once we define what the semantics of the
//       Assignments are.
//
// assignment returns the index of the assignment we're looking for, adding a
// new one if it doesn't exist.
func (a *AppDefinition) assignment(pool string) int {
	for i := range a.Assignments {
		if a.Assignments[i].Pool == pool {
			return i
		}
	}
	a.Assignments = append(a.Assignments, AppAssignment{Pool: pool})
	return len(a.Assignments) - 1
}
