package utils

import (
	"testing"
)

func TestSet(t *testing.T) {
	vmap := NewVersionedMap()
	vmap.Set("k1", "v1", 2)
	vmap.Set("k1", "v2", 1)
	vmap.Set("k2", "v2", 1)
	vmap.Set("k2", "v3", 3)
	vmap.Set("k2", "v4", 2)

	if vmap.Get("k1") != "v1" {
		t.Fail()
	}

	if vmap.Get("k2") != "v3" {
		t.Fail()
	}
}

func TestMerge(t *testing.T) {
	vmap1 := NewVersionedMap()
	vmap1.Set("k1", "v1", 1)

	vmap2 := NewVersionedMap()
	vmap2.Set("k1", "v2", 2)

	vmap1.Merge(vmap2)
	vmap2.Merge(vmap1)

	if vmap1.Get("k1") != "v2" {
		t.Fail()
	}
	if vmap2.Get("k1") != "v2" {
		t.Fail()
	}
}

func TestUnset(t *testing.T) {
	vmap := NewVersionedMap()
	vmap.Set("k1", "v1", 1)
	vmap.UnSet("k1", 2)

	if vmap.Get("k1") != "" {
		t.Fail()
	}
}

func TestUnsetConflict(t *testing.T) {
	vmap := NewVersionedMap()
	vmap.Set("k1", "v1", 1)
	vmap.Set("k1", "v2", 2)
	vmap.UnSet("k1", 2)

	if vmap.Get("k1") != "v2" {
		t.Fail()
	}

	vmap = NewVersionedMap()
	vmap.Set("k1", "v1", 1)
	vmap.UnSet("k1", 2)
	vmap.Set("k1", "v2", 2)

	if vmap.Get("k1") != "v2" {
		t.Fail()
	}

	vmap = NewVersionedMap()
	vmap.Set("k1", "v1", 1)
	vmap.UnSet("k1", 2)
	vmap.Set("k1", "v2", 2)
	vmap.Set("k1", "v3", 2)

	if vmap.Get("k1") != "v3" {
		t.Fail()
	}

}
