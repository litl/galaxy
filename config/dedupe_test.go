package config

import (
	"fmt"
	"testing"

	"github.com/hashicorp/consul/api"
)

func TestEventDedupe(t *testing.T) {
	cache := &eventCache{
		seen: make(map[string]uint64),
	}

	events := make([]*api.UserEvent, 0)
	for i := uint64(0); i < 10; i++ {
		events = append(events, &api.UserEvent{ID: fmt.Sprintf("%x", i), LTime: i})
	}

	newEvents := cache.Filter(events)

	if len(events) != len(newEvents) {
		t.Fatal("missing events")
	}

	// add 11 more
	for i := uint64(100); i < 111; i++ {
		events = append(events, &api.UserEvent{ID: fmt.Sprintf("%x", i), LTime: i})
	}

	newEvents = cache.Filter(events)
	if len(newEvents) != 11 {
		t.Fatalf("got %d events out of 11", len(newEvents))
	}

	// remove the old events
	events = events[:0]
	// add 9 more
	for i := uint64(200); i < 209; i++ {
		events = append(events, &api.UserEvent{ID: fmt.Sprintf("%x", i), LTime: i})
	}

	// make sure we only get 9 back
	newEvents = cache.Filter(events)
	if len(newEvents) != 9 {
		t.Fatal("got %d events out of 9", len(newEvents))
	}

	// check for correct events
	for _, event := range newEvents {
		if event.LTime < 200 || event.LTime > 209 {
			t.Fatal("wrong event", event.ID)
		}
	}

	// since we only have 9 events, and the cache has 21, it should have purged
	// the old events
	if len(cache.seen) != 9 {
		t.Fatal("cache should have been purged to the 9 most recent events")
	}
}
