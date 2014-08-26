package registry

type RegistryBackend interface {
	// Sets
	AddMember(key, value string) (int, error)
	RemoveMember(key, value string) (int, error)
	Members(key string) ([]string, error)

	// Keys
	Keys(key string) ([]string, error)

	//Pub/Sub
	Notify(key, value string) (int, error)
}
