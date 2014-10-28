package config

type ConfigBackend interface {
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
	Subscribe(key string) chan string
	Notify(key, value string) (int, error)

	Connect()
	Reconnect()

	// Maps
	Set(key, field string, value string) (string, error)
	Get(key, field string) (string, error)
	GetAll(key string) (map[string]string, error)
	SetMulti(key string, values map[string]string) (string, error)
	DeleteMulti(key string, fields ...string) (int, error)
}
