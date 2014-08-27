package registry

import (
	"github.com/litl/galaxy/utils"
)

type RegistryBackend interface {
	// Sets
	AddMember(key, value string) (int, error)
	RemoveMember(key, value string) (int, error)
	Members(key string) ([]string, error)

	// Keys
	Keys(key string) ([]string, error)
	Delete(key string) (int, error)
	Expire(key string, ttl uint64) (int, error)
	Ttl(key string) (int, error)

	//Pub/Sub
	Notify(key, value string) (int, error)

	Connect()
	Reconnect()

	// Maps
	Set(key, field string, value []byte) (string, error)
	Get(key, field string) ([]byte, error)

	// FIXME: jwilder - this interface is strange
	// VMap
	LoadVMap(key string, dest *utils.VersionedMap) error
	SaveVMap(key string, vmap *utils.VersionedMap) error
	GcVMap(key string, vmap *utils.VersionedMap) error
}
