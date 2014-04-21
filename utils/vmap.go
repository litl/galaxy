package utils

// VersionedMap is a CRDT where each key contains a version history of prior values.
// The value of the key is the value with the latest version.  VersionMaps can be combined
// such that they always converge to the same values for all keys.
type VersionedMap struct {
	values map[string][]mapEntry
}

type mapEntry struct {
	value   string
	version int64
}

func NewVersionedMap() *VersionedMap {
	return &VersionedMap{
		values: make(map[string][]mapEntry),
	}
}

func (v *VersionedMap) Set(key, value string, version int64) {
	entries := v.values[key]
	v.values[key] = append(entries, mapEntry{
		value:   value,
		version: version,
	})
}

func (v *VersionedMap) UnSet(key string, version int64) {
	entries := v.values[key]
	v.values[key] = append(entries, mapEntry{
		value:   "",
		version: version,
	})
}

func (v *VersionedMap) Get(key string) string {
	entries := v.values[key]
	maxEntry := mapEntry{}
	for _, entry := range entries {
		// value is max(version)
		if entry.version > maxEntry.version {
			maxEntry = entry
		}

		// if there is a conflict, prefer setting a value over unsetting one
		// as well the largest value as a tie-breaker if two sets conflict.
		if entry.version == maxEntry.version && entry.value > maxEntry.value {
			maxEntry = entry
		}

	}
	return maxEntry.value
}

func (v *VersionedMap) Merge(other *VersionedMap) {
	for k, entries := range other.values {
		v.values[k] = append(v.values[k], entries...)
	}
}
