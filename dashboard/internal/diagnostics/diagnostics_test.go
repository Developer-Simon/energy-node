package diagnostics

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/devicefilter"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

func TestEngineReportsOfflineAndMissingConfiguration(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{Device: registry.DeviceInfo{ID: "node", Name: "Node"}, Entity: registry.EntityInfo{UniqueID: "temp", Component: "sensor", Name: "Temperatur"}})
	engine := NewEngine(reg, settings.NewStore(t.TempDir()))
	warnings := engine.Evaluate(time.Now())
	if len(warnings) < 3 {
		t.Fatalf("got %d warnings, want missing topic, availability and update", len(warnings))
	}
}

func TestEngineInterpretsHealthThresholdAsSeconds(t *testing.T) {
	now := time.Now().UTC()
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "temp", Component: "sensor", StateTopic: "state/temp", UnitOfMeasurement: "C"},
	})
	reg.UpdateState("state/temp", []byte(`21.5`), false, now.Add(-3*time.Second))
	store := settings.NewStore(t.TempDir())
	if err := store.SaveSettings(settings.Settings{HealthScoreThreshold: 2, SweepIntervalSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	warnings := NewEngine(reg, store).Evaluate(now)
	for _, warning := range warnings {
		if warning.RuleID == "NoStateUpdate" && warning.EntityID == "temp" {
			return
		}
	}
	t.Fatal("expected a NoStateUpdate warning after more than two seconds")
}

func TestRulesAreIndividuallyEvaluable(t *testing.T) {
	device := registry.DeviceView{
		ID: "node",
		Entities: []registry.EntityView{{
			UniqueID:  "temp",
			Component: "sensor",
		}},
	}
	ctx := Context{Now: time.Now().UTC(), NoStateUpdateAfter: time.Minute}
	rules := []DiagnosticRule{
		MissingTopicRule{},
		DuplicateUniqueIDRule{},
		DiscoveryMismatchRule{},
		DiscoveryRetainedRule{},
		MissingAvailabilityRule{},
		WrongUnitRule{},
		NoStateUpdateRule{},
		OfflineRule{},
		InvalidPayloadRule{},
	}
	for _, rule := range rules {
		if rule.RuleID() == "" {
			t.Fatalf("rule %T has no rule ID", rule)
		}
		_ = rule.Evaluate(device, ctx)
	}
}

func TestInvalidPayloadContainsDetails(t *testing.T) {
	now := time.Now().UTC()
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node"},
		Entity: registry.EntityInfo{UniqueID: "temp", StateTopic: "state/temp"},
	})
	reg.UpdateState("state/temp", []byte("   "), false, now)

	warnings := NewEngine(reg, nil).Evaluate(now)
	for _, item := range warnings {
		if item.RuleID == "InvalidPayload" {
			if item.Details["value"] != "   " || item.Details["source"] != "live" {
				t.Fatalf("invalid payload details = %#v", item.Details)
			}
			return
		}
	}
	t.Fatal("expected an InvalidPayload warning")
}

func TestInvalidDiscoveryPayloadIsReportedWithDetails(t *testing.T) {
	now := time.Now().UTC()
	reg := registry.New()
	reg.RecordDiscoveryError("homeassistant/sensor/node/temp/config", "{", "unexpected end of JSON input", true, 1, "mqtt", now)

	warnings := NewEngine(reg, nil).Evaluate(now)
	for _, item := range warnings {
		if item.RuleID == "InvalidPayload" && item.Details["kind"] == "discovery" {
			if item.Topic != "homeassistant/sensor/node/temp/config" || item.Details["payload"] != "{" {
				t.Fatalf("invalid discovery details = %#v", item)
			}
			return
		}
	}
	t.Fatal("expected an InvalidPayload warning for discovery")
}

type duplicateWarningRule struct{}

func (duplicateWarningRule) RuleID() string { return "DuplicateTest" }

func (rule duplicateWarningRule) Evaluate(device registry.DeviceView, ctx Context) []Warning {
	entity := device.Entities[0]
	item := warning(rule.RuleID(), SeverityWarning, device.ID, entity, "same", "same", ctx.Now)
	return []Warning{item, item}
}

func TestEngineDeduplicatesWarnings(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node"},
		Entity: registry.EntityInfo{UniqueID: "temp", StateTopic: "state/temp"},
	})
	engine := &Engine{Registry: reg, Rules: []DiagnosticRule{duplicateWarningRule{}}}
	warnings := engine.Evaluate(time.Now().UTC())
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want one deduplicated warning", len(warnings))
	}
}

func TestEngineReportsDuplicateUniqueIDDetails(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "a-node"},
		Entity: registry.EntityInfo{UniqueID: "shared", StateTopic: "state/a"},
	})
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "z-node"},
		Entity: registry.EntityInfo{UniqueID: "shared", StateTopic: "state/z"},
	})

	warnings := NewEngine(reg, nil).Evaluate(time.Now().UTC())
	seen := 0
	for _, item := range warnings {
		if item.RuleID != "DuplicateUniqueID" {
			continue
		}
		seen++
		if item.Details["unique_id"] != "shared" {
			t.Fatalf("duplicate details = %#v", item.Details)
		}
	}
	if seen != 2 {
		t.Fatalf("got %d duplicate warnings, want one per affected device", seen)
	}
}

func TestDiscoveryRulesReportMismatchAndRetainState(t *testing.T) {
	now := time.Now().UTC()
	device := registry.DeviceView{
		ID:   "node",
		Name: "Node",
		Entities: []registry.EntityView{{
			UniqueID:          "temp",
			Component:         "sensor",
			ObjectID:          "temperature",
			Name:              "Temperature",
			StateTopic:        "state/temp",
			DiscoveryTopic:    "homeassistant/sensor/node/temperature/config",
			DiscoveryRetained: false,
			DiscoveryJSON:     `{"unique_id":"other","name":"Temperature","state_topic":"state/temp","device":{"name":"Node"}}`,
		}},
	}
	ctx := Context{Now: now}
	mismatch := DiscoveryMismatchRule{}.Evaluate(device, ctx)
	if len(mismatch) != 1 || mismatch[0].Details["field"] != "unique_id" {
		t.Fatalf("mismatch warnings = %#v", mismatch)
	}
	retained := DiscoveryRetainedRule{}.Evaluate(device, ctx)
	if len(retained) != 1 || retained[0].Details["retained"] != false {
		t.Fatalf("retain warnings = %#v", retained)
	}
}

func TestHealthUsesThreePollIntervals(t *testing.T) {
	now := time.Now().UTC()
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{Device: registry.DeviceInfo{ID: "node"}, Entity: registry.EntityInfo{UniqueID: "poll", ObjectID: "poll_interval_s", StateTopic: "state/poll"}})
	reg.UpsertEntity(registry.Discovery{Device: registry.DeviceInfo{ID: "node"}, Entity: registry.EntityInfo{UniqueID: "temp", ObjectID: "temperature", StateTopic: "state/temp"}})
	reg.UpdateState("state/poll", []byte("10"), false, now)
	reg.UpdateState("state/temp", []byte("21"), false, now.Add(-5*time.Second))

	health := NewEngine(reg, nil).Health(now)
	if len(health) != 1 || health[0].ThresholdSeconds != 30 || health[0].Score != 83 {
		t.Fatalf("health = %#v, want 30-second threshold and score 83", health)
	}
}

func TestMissingTopicRuleWarnsOnlyWithoutStateTopic(t *testing.T) {
	ctx := Context{Now: time.Now().UTC(), NoStateUpdateAfter: time.Minute}

	missing := registry.DeviceView{ID: "node", Entities: []registry.EntityView{{
		UniqueID: "temp", Component: "sensor", StateTopic: "",
	}}}
	warnings := MissingTopicRule{}.Evaluate(missing, ctx)
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings for a missing state topic, want 1: %#v", len(warnings), warnings)
	}
	if warnings[0].RuleID != "MissingTopic" {
		t.Fatalf("rule ID %q, want MissingTopic", warnings[0].RuleID)
	}
	if warnings[0].Severity != SeverityCritical {
		t.Fatalf("severity %v, want %v", warnings[0].Severity, SeverityCritical)
	}

	present := registry.DeviceView{ID: "node", Entities: []registry.EntityView{{
		UniqueID: "temp", Component: "sensor", StateTopic: "werkstatt/node/temp",
	}}}
	if warnings := (MissingTopicRule{}).Evaluate(present, ctx); len(warnings) != 0 {
		t.Fatalf("got %d warnings for a present state topic, want 0: %#v", len(warnings), warnings)
	}
}

func TestMissingAvailabilityRuleWarnsOnlyWithoutAvailabilityTopic(t *testing.T) {
	ctx := Context{Now: time.Now().UTC(), NoStateUpdateAfter: time.Minute}

	missing := registry.DeviceView{ID: "node", Entities: []registry.EntityView{{
		UniqueID: "temp", Component: "sensor", StateTopic: "werkstatt/node/temp",
	}}}
	warnings := MissingAvailabilityRule{}.Evaluate(missing, ctx)
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings for a missing availability topic, want 1: %#v", len(warnings), warnings)
	}
	if warnings[0].RuleID != "MissingAvailability" {
		t.Fatalf("rule ID %q, want MissingAvailability", warnings[0].RuleID)
	}
	if warnings[0].Severity != SeverityWarning {
		t.Fatalf("severity %v, want %v", warnings[0].Severity, SeverityWarning)
	}

	present := registry.DeviceView{ID: "node", Entities: []registry.EntityView{{
		UniqueID:          "temp",
		Component:         "sensor",
		StateTopic:        "werkstatt/node/temp",
		AvailabilityTopic: "werkstatt/node/status",
	}}}
	if warnings := (MissingAvailabilityRule{}).Evaluate(present, ctx); len(warnings) != 0 {
		t.Fatalf("got %d warnings for a present availability topic, want 0: %#v", len(warnings), warnings)
	}
}

func TestMissingAvailabilityRuleExemptsConnectivityEntity(t *testing.T) {
	ctx := Context{Now: time.Now().UTC(), NoStateUpdateAfter: time.Minute}

	online := registry.DeviceView{ID: "t2sga55a69", Entities: []registry.EntityView{{
		UniqueID:    "t2sga55a69_online",
		Component:   "binary_sensor",
		DeviceClass: "connectivity",
		StateTopic:  "outstation/t2sga55a69/status/online",
	}}}
	if warnings := (MissingAvailabilityRule{}).Evaluate(online, ctx); len(warnings) != 0 {
		t.Fatalf("got %d warnings for the connectivity availability entity, want 0: %#v", len(warnings), warnings)
	}

	// Ein gewoehnlicher binary_sensor ohne connectivity bleibt betroffen.
	other := registry.DeviceView{ID: "t2sga55a69", Entities: []registry.EntityView{{
		UniqueID: "t2sga55a69_fault", Component: "binary_sensor", DeviceClass: "problem",
	}}}
	if warnings := (MissingAvailabilityRule{}).Evaluate(other, ctx); len(warnings) != 1 {
		t.Fatalf("got %d warnings for a non-connectivity binary_sensor, want 1: %#v", len(warnings), warnings)
	}
}

func TestWrongUnitRuleWarnsOnlyForSensorsWithoutUnit(t *testing.T) {
	ctx := Context{Now: time.Now().UTC(), NoStateUpdateAfter: time.Minute}

	missing := registry.DeviceView{ID: "node", Entities: []registry.EntityView{{
		UniqueID: "temp", Component: "sensor", UnitOfMeasurement: "",
	}}}
	warnings := WrongUnitRule{}.Evaluate(missing, ctx)
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings for a sensor without unit, want 1: %#v", len(warnings), warnings)
	}
	if warnings[0].RuleID != "WrongUnit" {
		t.Fatalf("rule ID %q, want WrongUnit", warnings[0].RuleID)
	}
	if warnings[0].Severity != SeverityWarning {
		t.Fatalf("severity %v, want %v", warnings[0].Severity, SeverityWarning)
	}

	present := registry.DeviceView{ID: "node", Entities: []registry.EntityView{{
		UniqueID: "temp", Component: "sensor", UnitOfMeasurement: "°C",
	}}}
	if warnings := (WrongUnitRule{}).Evaluate(present, ctx); len(warnings) != 0 {
		t.Fatalf("got %d warnings for a sensor with unit, want 0: %#v", len(warnings), warnings)
	}

	// Nur Sensoren tragen eine Einheit; ein Schalter ohne Einheit ist korrekt.
	//
	// Blank statt "" fuer UnitOfMeasurement: die Regel trimmt, ein reiner
	// Leerraum-Wert zaehlt also ebenfalls als fehlend.
	switchEntity := registry.DeviceView{ID: "node", Entities: []registry.EntityView{{
		UniqueID: "relay", Component: "switch", UnitOfMeasurement: "  ",
	}}}
	if warnings := (WrongUnitRule{}).Evaluate(switchEntity, ctx); len(warnings) != 0 {
		t.Fatalf("got %d warnings for a non-sensor, want 0: %#v", len(warnings), warnings)
	}
}

func writeConfigFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".schema.json"), []byte(`{"type":"array"}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestConfiguredDeviceMissingReportsUnknownID(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "shelly_devices", `[{"id":"batterie_spannung","name":"Shelly Uni"}]`)
	snapshot := registry.DiagnosticsSnapshot{}
	ctx := SnapshotContext{
		Now:       time.Now().UTC(),
		Configs:   config.NewManager(dir),
		StartedAt: time.Now().UTC().Add(-time.Hour),
	}
	warnings := ConfiguredDeviceMissingRule{}.Evaluate(snapshot, ctx)
	if len(warnings) != 1 || warnings[0].DeviceID != "batterie_spannung" || warnings[0].Details["config_file"] != "shelly_devices.json" {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestConfiguredDeviceMissingSkipsKnownDevice(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "shelly_devices", `[{"id":"batterie_spannung","name":"Shelly Uni"}]`)
	snapshot := registry.DiagnosticsSnapshot{Devices: []registry.DeviceView{{ID: "batterie_spannung"}}}
	ctx := SnapshotContext{
		Now:       time.Now().UTC(),
		Configs:   config.NewManager(dir),
		StartedAt: time.Now().UTC().Add(-time.Hour),
	}
	warnings := ConfiguredDeviceMissingRule{}.Evaluate(snapshot, ctx)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
}

func TestConfiguredDeviceMissingRespectsStartupGrace(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "shelly_devices", `[{"id":"batterie_spannung","name":"Shelly Uni"}]`)
	snapshot := registry.DiagnosticsSnapshot{}
	now := time.Now().UTC()
	ctx := SnapshotContext{Now: now, Configs: config.NewManager(dir), StartedAt: now.Add(-5 * time.Second)}
	warnings := ConfiguredDeviceMissingRule{}.Evaluate(snapshot, ctx)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none during startup grace", warnings)
	}
}

func TestConfiguredDeviceMissingSkipsIgnoredDevice(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "shelly_devices", `[{"id":"batterie_spannung","name":"Shelly Uni"}]`)
	filterDir := t.TempDir()
	ignored := devicefilter.NewStore(filterDir)
	if err := ignored.Ignore(registry.DeviceView{ID: "batterie_spannung", Name: "Shelly Uni"}); err != nil {
		t.Fatal(err)
	}
	snapshot := registry.DiagnosticsSnapshot{}
	ctx := SnapshotContext{
		Now:       time.Now().UTC(),
		Configs:   config.NewManager(dir),
		Ignored:   ignored,
		StartedAt: time.Now().UTC().Add(-time.Hour),
	}
	warnings := ConfiguredDeviceMissingRule{}.Evaluate(snapshot, ctx)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none for ignored device", warnings)
	}
}

func TestConfiguredDeviceMissingSkipsNonArrayConfig(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "automation_rules", `{"version":1,"settings":{},"rules":[]}`)
	snapshot := registry.DiagnosticsSnapshot{}
	ctx := SnapshotContext{
		Now:       time.Now().UTC(),
		Configs:   config.NewManager(dir),
		StartedAt: time.Now().UTC().Add(-time.Hour),
	}
	warnings := ConfiguredDeviceMissingRule{}.Evaluate(snapshot, ctx)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none for a top-level object config", warnings)
	}
}
