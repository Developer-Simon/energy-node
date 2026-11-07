package diagnostics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	reg.UpdateState("state/temp", []byte(`21.5`), false, now.Add(-15*time.Second))
	store := settings.NewStore(t.TempDir())
	if err := store.SaveSettings(settings.Settings{HealthScoreThreshold: 10, SweepIntervalSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	warnings := NewEngine(reg, store).Evaluate(now)
	for _, warning := range warnings {
		if warning.RuleID == "NoStateUpdate" && warning.EntityID == "temp" {
			return
		}
	}
	t.Fatal("expected a NoStateUpdate warning after more than ten seconds")
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
	item := warning(rule.RuleID(), "missing_topic", SeverityWarning, device.ID, entity, ctx.Now)
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

func TestHealthUsesTheConfiguredThreshold(t *testing.T) {
	now := time.Now().UTC()
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{Device: registry.DeviceInfo{ID: "node"}, Entity: registry.EntityInfo{UniqueID: "temp", ObjectID: "temperature", StateTopic: "state/temp"}})
	reg.UpsertEntity(registry.Discovery{Device: registry.DeviceInfo{ID: "node"}, Entity: registry.EntityInfo{UniqueID: "power", ObjectID: "power", StateTopic: "state/power"}})
	reg.UpdateState("state/temp", []byte("21"), false, now.Add(-5*time.Second))
	reg.UpdateState("state/power", []byte("400"), false, now.Add(-30*time.Second))
	store := settings.NewStore(t.TempDir())
	if err := store.SaveSettings(settings.Settings{HealthScoreThreshold: 30, SweepIntervalSeconds: 300}); err != nil {
		t.Fatal(err)
	}

	health := NewEngine(reg, store).Health(now)
	if len(health) != 1 || health[0].ThresholdSeconds != 30 || health[0].Score != 83 {
		t.Fatalf("health = %#v, want 30-second threshold and score 83", health)
	}
}

func TestHealthCountsPollIntervalEntityLikeAnyOther(t *testing.T) {
	now := time.Now().UTC()
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{Device: registry.DeviceInfo{ID: "node"}, Entity: registry.EntityInfo{UniqueID: "poll", ObjectID: "poll_interval_s", StateTopic: "state/poll"}})
	reg.UpsertEntity(registry.Discovery{Device: registry.DeviceInfo{ID: "node"}, Entity: registry.EntityInfo{UniqueID: "temp", ObjectID: "temperature", StateTopic: "state/temp"}})
	reg.UpdateState("state/poll", []byte("10"), false, now)
	reg.UpdateState("state/temp", []byte("21"), false, now.Add(-20*time.Second))
	store := settings.NewStore(t.TempDir())
	if err := store.SaveSettings(settings.Settings{HealthScoreThreshold: 30, SweepIntervalSeconds: 300}); err != nil {
		t.Fatal(err)
	}

	health := NewEngine(reg, store).Health(now)
	if len(health) != 1 || health[0].ThresholdSeconds != 30 || health[0].Score != 100 {
		t.Fatalf("health = %#v, want threshold 30 and score 100 from the newest entity", health)
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

func TestWrongUnitRuleWarnsForMeasurementsWithoutUnit(t *testing.T) {
	ctx := Context{Now: time.Now().UTC(), NoStateUpdateAfter: time.Minute}

	for _, entity := range []registry.EntityView{
		{UniqueID: "temp", Component: "sensor", StateClass: "measurement"},
		{UniqueID: "volt", Component: "sensor", DeviceClass: "voltage"},
	} {
		device := registry.DeviceView{ID: "zigbee_plug", Entities: []registry.EntityView{entity}}
		warnings := WrongUnitRule{}.Evaluate(device, ctx)
		if len(warnings) != 1 {
			t.Fatalf("%s: got %d warnings, want 1: %#v", entity.UniqueID, len(warnings), warnings)
		}
		if warnings[0].RuleID != "WrongUnit" || warnings[0].Key != "missing_unit" || warnings[0].Severity != SeverityWarning {
			t.Fatalf("%s: got %+v, want a missing_unit warning", entity.UniqueID, warnings[0])
		}
	}

	present := registry.DeviceView{ID: "zigbee_plug", Entities: []registry.EntityView{{
		UniqueID: "temp", Component: "sensor", StateClass: "measurement", UnitOfMeasurement: "°C",
	}}}
	if warnings := (WrongUnitRule{}).Evaluate(present, ctx); len(warnings) != 0 {
		t.Fatalf("got %d warnings for a sensor with unit, want 0: %#v", len(warnings), warnings)
	}

	// Nur Sensoren tragen eine Einheit; ein Schalter ohne Einheit ist korrekt.
	//
	// Blank statt "" fuer UnitOfMeasurement: die Regel trimmt, ein reiner
	// Leerraum-Wert zaehlt also ebenfalls als fehlend.
	switchEntity := registry.DeviceView{ID: "zigbee_plug", Entities: []registry.EntityView{{
		UniqueID: "relay", Component: "switch", UnitOfMeasurement: "  ",
	}}}
	if warnings := (WrongUnitRule{}).Evaluate(switchEntity, ctx); len(warnings) != 0 {
		t.Fatalf("got %d warnings for a non-sensor, want 0: %#v", len(warnings), warnings)
	}
}

// Ein Sensor ohne Einheit, state_class und Einheiten-device_class kann ein
// Textwert sein (IP-Adresse, SSID). Er wird nur eine Info.
func TestWrongUnitRuleMakesUnclassifiedSensorsAnInfo(t *testing.T) {
	ctx := Context{Now: time.Now().UTC(), NoStateUpdateAfter: time.Minute}
	for _, entity := range []registry.EntityView{
		{UniqueID: "ip", Component: "sensor"},
		{UniqueID: "mode", Component: "sensor", DeviceClass: "enum"},
		{UniqueID: "boot", Component: "sensor", DeviceClass: "timestamp", UnitOfMeasurement: " "},
	} {
		device := registry.DeviceView{ID: "zigbee_plug", Entities: []registry.EntityView{entity}}
		warnings := WrongUnitRule{}.Evaluate(device, ctx)
		if len(warnings) != 1 || warnings[0].Key != "unit_unclassified" || warnings[0].Severity != SeverityInfo {
			t.Fatalf("%s: got %#v, want one unit_unclassified info", entity.UniqueID, warnings)
		}
	}
}

func TestWrongUnitRuleSkipsOwnDevices(t *testing.T) {
	ctx := Context{Now: time.Now().UTC(), NoStateUpdateAfter: time.Minute, OwnDevice: true}
	device := registry.DeviceView{ID: "apsystems_ez1_1", Entities: []registry.EntityView{
		{UniqueID: "ip", Component: "sensor"},
		{UniqueID: "volt", Component: "sensor", StateClass: "measurement"},
	}}
	if warnings := (WrongUnitRule{}).Evaluate(device, ctx); len(warnings) != 0 {
		t.Fatalf("got %d warnings for an own device, want 0: %#v", len(warnings), warnings)
	}
}

func TestIsOwnDeviceFollowsTheViaDeviceChain(t *testing.T) {
	via := map[string]string{
		"energy_node":   "",
		"battery_soc":   "energy_node",
		"t2sga55a69":    "battery_soc",
		"zigbee_plug":   "zigbee_bridge",
		"zigbee_bridge": "",
		"loop_a":        "loop_b",
		"loop_b":        "loop_a",
		"orphan":        "missing_parent",
	}
	viaOf := func(id string) (string, bool) {
		parent, ok := via[id]
		return parent, ok
	}
	for id, want := range map[string]bool{
		"energy_node": true,
		"battery_soc": true,
		"t2sga55a69":  true,
		"zigbee_plug": false,
		"loop_a":      false,
		"orphan":      false,
	} {
		if got := isOwnDevice(id, viaOf); got != want {
			t.Errorf("isOwnDevice(%q) = %v, want %v", id, got, want)
		}
	}
}

// Die Engine erkennt eigene Geraete ueber via_device. Infos landen in
// Evaluate, aber weder im Geraete-Modal (Device) noch im Health-Zaehler.
func TestEngineKeepsUnitInfosOnTheDiagnosticsPageOnly(t *testing.T) {
	now := time.Now().UTC()
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "energy_node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "energy_node_ip", Component: "sensor", StateTopic: "node/ip"},
	})
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "apsystems_ez1_1", Name: "EZ1", ViaDevice: "energy_node"},
		Entity: registry.EntityInfo{UniqueID: "apsystems_ez1_1_ip", Component: "sensor", StateTopic: "ez1/ip"},
	})
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "zigbee_plug", Name: "Plug"},
		Entity: registry.EntityInfo{UniqueID: "zigbee_plug_ip", Component: "sensor", StateTopic: "plug/ip"},
	})
	engine := NewEngine(reg, settings.NewStore(t.TempDir()))

	var unitWarnings []Warning
	for _, item := range engine.Evaluate(now) {
		if item.RuleID == "WrongUnit" {
			unitWarnings = append(unitWarnings, item)
		}
	}
	if len(unitWarnings) != 1 || unitWarnings[0].DeviceID != "zigbee_plug" || unitWarnings[0].Severity != SeverityInfo {
		t.Fatalf("got %#v, want one info for zigbee_plug only", unitWarnings)
	}

	for _, item := range engine.Device("zigbee_plug", now) {
		if item.Severity == SeverityInfo {
			t.Fatalf("Device() returned an info: %#v", item)
		}
	}
	all := 0
	for _, item := range engine.Evaluate(now) {
		if item.DeviceID == "zigbee_plug" && item.Severity != SeverityInfo {
			all++
		}
	}
	for _, health := range engine.Health(now) {
		if health.DeviceID == "zigbee_plug" && health.WarningCount != all {
			t.Fatalf("WarningCount = %d, want %d (infos excluded)", health.WarningCount, all)
		}
	}
}

func TestIsOperationalRule(t *testing.T) {
	for _, ruleID := range []string{"Offline", "NoStateUpdate"} {
		if !IsOperationalRule(ruleID) {
			t.Fatalf("IsOperationalRule(%q) = false, want true", ruleID)
		}
	}
	for _, ruleID := range []string{"DiscoveryMismatch", "WrongUnit", "InvalidPayload"} {
		if IsOperationalRule(ruleID) {
			t.Fatalf("IsOperationalRule(%q) = true, want false", ruleID)
		}
	}

	ctx := Context{Now: time.Now().UTC(), NoStateUpdateAfter: time.Minute}
	offline := registry.DeviceView{ID: "node", Entities: []registry.EntityView{{
		UniqueID: "temp", Component: "sensor", HasAvailability: true, Available: false,
	}}}
	warnings := OfflineRule{}.Evaluate(offline, ctx)
	if len(warnings) != 1 {
		t.Fatalf("got %d offline warnings, want 1: %#v", len(warnings), warnings)
	}
	if !warnings[0].Operational {
		t.Fatal("Offline warning has Operational = false, want true")
	}
	raw, err := json.Marshal(warnings[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"operational":true`) {
		t.Fatalf("Offline warning JSON %s lacks \"operational\":true", raw)
	}

	wrongUnit := WrongUnitRule{}.Evaluate(registry.DeviceView{ID: "node", Entities: []registry.EntityView{{
		UniqueID: "temp", Component: "sensor", StateClass: "measurement",
	}}}, ctx)
	if len(wrongUnit) != 1 {
		t.Fatalf("got %d wrong-unit warnings, want 1: %#v", len(wrongUnit), wrongUnit)
	}
	if wrongUnit[0].Operational {
		t.Fatal("WrongUnit warning has Operational = true, want false")
	}
	raw, err = json.Marshal(wrongUnit[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"operational"`) {
		t.Fatalf("WrongUnit warning JSON %s must omit the operational key", raw)
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
