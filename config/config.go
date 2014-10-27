package config

import (
	"github.com/litl/galaxy/registry"
)

type ConfigStore interface {
	Get(app, env string) (*registry.ServiceConfig, error)
	ListAssignments(env, pool string) ([]string, error)
	Watch(env string, stop chan struct{}) chan *registry.ConfigChange
}
