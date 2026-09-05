package diagnostics

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/devicefilter"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func TestStaleIgnoredDeviceProducesOnlyDedicatedWarning(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{Device: registry.DeviceInfo{ID: "active"}, Entity: registry.EntityInfo{UniqueID: "active_temp", StateTopic: "state/active"}})
	store := devicefilter.NewStore(filepath.Join(t.TempDir(), "ignored_devices.json"))
	if err := store.Ignore(registry.DeviceView{ID: "ignored", Name: "Ignored", Entities: []registry.EntityView{{UniqueID: "ignored_temp", DiscoveryTopic: "homeassistant/sensor/ignored/temp/config"}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkMissing("homeassistant/sensor/ignored/temp/config"); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine(reg, nil)
	engine.SetIgnoredStore(store)
	warnings := engine.Evaluate(nowForTest())
	found := false
	for _, warning := range warnings {
		if warning.DeviceID != "ignored" {
			continue
		}
		if warning.RuleID != "IgnoredDeviceDiscoveryStale" {
			t.Fatalf("unexpected ignored-device warning: %#v", warning)
		}
		found = true
	}
	if !found {
		t.Fatal("stale ignored device did not produce a warning")
	}
	for _, health := range engine.Health(nowForTest()) {
		if health.DeviceID == "ignored" {
			t.Fatal("ignored device appeared in health")
		}
	}
}

func nowForTest() time.Time { return time.Now().UTC() }
