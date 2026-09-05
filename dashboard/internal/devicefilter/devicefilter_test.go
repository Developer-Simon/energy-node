package devicefilter

import (
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func TestStoreIgnorePresenceAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ignored_devices.json")
	store := NewStore(path)
	view := registry.DeviceView{ID: "node", Name: "Node", Entities: []registry.EntityView{{
		UniqueID: "node_temp", Name: "Temperature", DiscoveryTopic: "homeassistant/sensor/node/temp/config",
		StateTopic: "node/state", Availability: []registry.AvailabilityInfo{{Topic: "node/status"}},
	}}}
	if err := store.Ignore(view); err != nil {
		t.Fatal(err)
	}
	if !store.IsIgnored("node") {
		t.Fatal("device was not ignored")
	}
	if missing, stale := store.Missing("node"); stale || len(missing) != 0 {
		t.Fatalf("initial presence = missing %v, stale %v", missing, stale)
	}
	if err := store.MarkMissing(view.Entities[0].DiscoveryTopic); err != nil {
		t.Fatal(err)
	}
	if _, stale := store.Missing("node"); !stale {
		t.Fatal("single missing discovery was not stale")
	}
	loaded := NewStore(path)
	if err := loaded.Load(); err != nil {
		t.Fatal(err)
	}
	if summaries := loaded.Summaries(); len(summaries) != 1 || !summaries[0].Stale {
		t.Fatalf("loaded summaries = %#v", summaries)
	}
}

func TestStoreDiscoveryPresenceCanReturn(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "ignored_devices.json"))
	view := registry.DeviceView{ID: "node", Entities: []registry.EntityView{{
		UniqueID: "node_temp", DiscoveryTopic: "homeassistant/sensor/node/temp/config",
	}}}
	if err := store.Ignore(view); err != nil {
		t.Fatal(err)
	}
	discovery := registry.Discovery{Device: registry.DeviceInfo{ID: "node"}, DiscoveryTopic: view.Entities[0].DiscoveryTopic, Entity: registry.EntityInfo{UniqueID: "node_temp", ObjectID: "temp"}}
	if err := store.MarkMissing(discovery.DiscoveryTopic); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkPresent(discovery); err != nil {
		t.Fatal(err)
	}
	if _, stale := store.Missing("node"); stale {
		t.Fatal("present discovery remained stale")
	}
}
