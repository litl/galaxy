package config

type RegistryBackend interface {

	// Keys
	Keys(key string) ([]string, error)
	Delete(key string) (int, error)
	Expire(key string, ttl uint64) (int, error)
	TTL(key string) (int, error)

	Connect()
	Reconnect()

	// Maps
	Set(key, field string, value string) (string, error)
	Get(key, field string) (string, error)
}
