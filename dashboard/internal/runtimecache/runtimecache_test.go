package runtimecache

import (
	"os"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func TestStorePersistsChangedValuesAndIgnoresCorruptFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/runtime.json", []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	devices := []registry.DeviceView{{ID: "node", Entities: []registry.EntityView{{UniqueID: "temp", StateTopic: "state/temp", Value: "21.5", HasValue: true, LastSeen: now}}}}
	if err := store.Observe(devices, now); err != nil {
		t.Fatal(err)
	}
	reloaded := NewStore(dir)
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Entries(); len(got) != 1 || got[0].Value != "21.5" {
		t.Fatalf("got %#v", got)
	}
}

func TestStoreRestoresEntityAfterDiscovery(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	now := time.Now().UTC().Truncate(time.Second)
	devices := []registry.DeviceView{{ID: "node", Entities: []registry.EntityView{{UniqueID: "power", StateTopic: "state/power", Value: "21.5", HasValue: true, LastSeen: now}}}}
	if err := store.Observe(devices, now); err != nil {
		t.Fatal(err)
	}
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{Device: registry.DeviceInfo{ID: "node"}, Entity: registry.EntityInfo{UniqueID: "power", StateTopic: "state/power"}})
	if !store.Restore(reg, "node", "power") {
		t.Fatal("expected cache restore")
	}
	entity := reg.Snapshot()[0].Entities[0]
	if entity.Source != "runtime-cache" || entity.Value != "21.5" {
		t.Fatalf("restored entity = %#v", entity)
	}
}

func TestStoreDoesNotWriteForTimestampOnlyChanges(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	first := time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC)
	second := first.Add(time.Minute)
	devices := []registry.DeviceView{{ID: "node", Entities: []registry.EntityView{{UniqueID: "power", StateTopic: "state/power", Value: "21.5", HasValue: true, LastSeen: first, LastTopicAt: first}}}}
	if err := store.Observe(devices, first); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(dir + "/runtime.json")
	if err != nil {
		t.Fatal(err)
	}

	devices[0].Entities[0].LastSeen = second
	devices[0].Entities[0].LastTopicAt = second
	if err := store.Observe(devices, second); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(dir + "/runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("timestamp-only update rewrote cache: before=%s after=%s", before, after)
	}
	entries := store.Entries()
	if len(entries) != 1 || !entries[0].LastSeen.Equal(second) {
		t.Fatalf("in-memory entry did not advance: %#v", entries)
	}
}

func TestStoreObserveMemoryDoesNotWriteToDisk(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	now := time.Now().UTC()
	devices := []registry.DeviceView{{ID: "node", Entities: []registry.EntityView{{UniqueID: "power", StateTopic: "state/power", Value: "21.5", HasValue: true, LastSeen: now}}}}

	store.ObserveMemory(devices)
	if _, err := os.Stat(dir + "/runtime.json"); !os.IsNotExist(err) {
		t.Fatalf("ObserveMemory created runtime.json, stat error = %v", err)
	}
	entries := store.Entries()
	if len(entries) != 1 || entries[0].Value != "21.5" {
		t.Fatalf("in-memory entries = %#v", entries)
	}
}

func TestStoreObserveChangesUpdatesOnlyGivenEntitiesWithoutWritingToDisk(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	now := time.Now().UTC()
	changes := []registry.StateChange{{
		DeviceID: "node", UniqueID: "power", StateTopic: "state/power",
		Value: "21.5", HasValue: true, LastSeen: now, LastTopicAt: now,
	}}

	store.ObserveChanges(changes)
	if _, err := os.Stat(dir + "/runtime.json"); !os.IsNotExist(err) {
		t.Fatalf("ObserveChanges created runtime.json, stat error = %v", err)
	}
	entries := store.Entries()
	if len(entries) != 1 || entries[0].DeviceID != "node" || entries[0].EntityID != "power" || entries[0].Value != "21.5" {
		t.Fatalf("in-memory entries = %#v", entries)
	}

	// A change without a value yet (e.g. purely from an availability
	// message) must not create an entry, matching ObserveMemory's
	// !entity.HasValue skip.
	store.ObserveChanges([]registry.StateChange{{DeviceID: "node", UniqueID: "unseen", HasValue: false}})
	if entries := store.Entries(); len(entries) != 1 {
		t.Fatalf("entries after value-less change = %#v, want unchanged", entries)
	}
}
