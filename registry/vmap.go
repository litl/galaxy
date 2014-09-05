package registry

import (
	"errors"

	"github.com/litl/galaxy/utils"
)

func (r *ServiceRegistry) LoadVMap(key string, dest *utils.VersionedMap) error {
	serialized, err := r.backend.GetAll(key)
	if err != nil {
		return err
	}

	dest.UnmarshalMap(serialized)
	return nil
}

func (r *ServiceRegistry) SaveVMap(key string, vmap *utils.VersionedMap) error {

	serialized := vmap.MarshalMap()
	if len(serialized) == 0 {
		return nil
	}

	created, err := r.backend.SetMulti(key, serialized)

	if err != nil {
		return err
	}

	if created != "OK" {
		return errors.New("not saved")
	}

	r.GcVMap(key, vmap)
	return nil
}

func (r *ServiceRegistry) GcVMap(key string, vmap *utils.VersionedMap) error {
	serialized := vmap.MarshalExpiredMap(5)
	if len(serialized) > 0 {
		keys := []string{}
		for k, _ := range serialized {
			keys = append(keys, k)
		}

		deleted, err := r.backend.DeleteMulti(key, keys...)

		if err != nil {
			return err
		}

		if deleted != 1 {
			return errors.New("not deleted")
		}
	}
	return nil
}
