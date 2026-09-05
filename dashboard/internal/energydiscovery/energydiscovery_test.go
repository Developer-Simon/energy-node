package energydiscovery_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energydiscovery"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func configByObjectID(t *testing.T, prefix, sw string, enabled bool, objectID string) map[string]any {
	t.Helper()
	want := energydiscovery.DiscoveryTopic(prefix, objectID)
	for _, m := range energydiscovery.Configs(prefix, sw, enabled) {
		if m.Topic != want {
			continue
		}
		if len(m.Payload) == 0 {
			return nil
		}
		var got map[string]any
		if err := json.Unmarshal(m.Payload, &got); err != nil {
			t.Fatalf("payload for %s is not valid JSON: %v", objectID, err)
		}
		return got
	}
	t.Fatalf("no config message for object_id %q (topic %q)", objectID, want)
	return nil
}

func TestConfigsMatchGolden(t *testing.T) {
	for _, objectID := range []string{"pv_power", "battery_soc"} {
		got := configByObjectID(t, "homeassistant", "1.2.3-test", true, objectID)
		raw, err := os.ReadFile(filepath.Join("testdata", objectID+".config.json"))
		if err != nil {
			t.Fatal(err)
		}
		var want map[string]any
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			gotPretty, _ := json.MarshalIndent(got, "", "  ")
			t.Fatalf("%s config mismatch:\n got: %s\nwant: %s", objectID, gotPretty, raw)
		}
	}
}

func TestConfigsInvariants(t *testing.T) {
	msgs := energydiscovery.Configs("ha-test", "9.9.9", true)
	if len(msgs) != 7 {
		t.Fatalf("Configs returned %d messages, want 7", len(msgs))
	}
	seen := map[string]bool{}
	for _, m := range msgs {
		if len(m.Payload) == 0 {
			t.Fatalf("enabled=true must not produce an empty payload (topic %s)", m.Topic)
		}
		var p map[string]any
		if err := json.Unmarshal(m.Payload, &p); err != nil {
			t.Fatalf("topic %s: %v", m.Topic, err)
		}
		uid, _ := p["unique_id"].(string)
		if uid == "" || seen[uid] {
			t.Fatalf("topic %s: unique_id %q missing or duplicated", m.Topic, uid)
		}
		seen[uid] = true
		if p["state_topic"] != energydiscovery.StateTopic {
			t.Fatalf("topic %s: state_topic = %v, want %s", m.Topic, p["state_topic"], energydiscovery.StateTopic)
		}
		if p["availability_topic"] != energydiscovery.AvailabilityTopic {
			t.Fatalf("topic %s: availability_topic = %v, want %s", m.Topic, p["availability_topic"], energydiscovery.AvailabilityTopic)
		}
		wantTopic := "ha-test/sensor/dashboard_energy/"
		if len(m.Topic) < len(wantTopic) || m.Topic[:len(wantTopic)] != wantTopic {
			t.Fatalf("topic %s does not start with %s", m.Topic, wantTopic)
		}
	}
}

func TestConfigsDisabledProducesRemovals(t *testing.T) {
	enabled := energydiscovery.Configs("homeassistant", "1", true)
	disabled := energydiscovery.Configs("homeassistant", "1", false)
	if len(enabled) != len(disabled) {
		t.Fatalf("enabled/disabled length mismatch: %d vs %d", len(enabled), len(disabled))
	}
	for i := range enabled {
		if enabled[i].Topic != disabled[i].Topic {
			t.Fatalf("topic %d differs: %s vs %s", i, enabled[i].Topic, disabled[i].Topic)
		}
		if len(disabled[i].Payload) != 0 {
			t.Fatalf("disabled payload %d is not empty: %q", i, disabled[i].Payload)
		}
	}
}

// TestValueTemplatesResolveThroughSharedParser stellt sicher, dass jede
// emittierte value_template-Form von derselben Teilmenge verstanden wird, mit
// der das Dashboard fremde Discovery parst (internal/registry). Andernfalls
// zeigt das Dashboard die eigene Energie-Geräte-Kachel/-Modal nur als rohe
// Nutzlast statt als aufgelösten Wert.
func TestValueTemplatesResolveThroughSharedParser(t *testing.T) {
	reg := registry.New()
	for _, m := range energydiscovery.Configs("homeassistant", "1.2.3-test", true) {
		if len(m.Payload) == 0 {
			t.Fatalf("enabled config %s has empty payload", m.Topic)
		}
		var p struct {
			UniqueID      string `json:"unique_id"`
			ValueTemplate string `json:"value_template"`
		}
		if err := json.Unmarshal(m.Payload, &p); err != nil {
			t.Fatalf("payload %s: %v", m.Topic, err)
		}
		reg.UpsertEntity(registry.Discovery{
			Device: registry.DeviceInfo{ID: energydiscovery.DeviceIdentifier, Name: "Energy Node"},
			Entity: registry.EntityInfo{
				UniqueID:      p.UniqueID,
				ObjectID:      p.UniqueID,
				Name:          p.UniqueID,
				Component:     "sensor",
				StateTopic:    energydiscovery.StateTopic,
				ValueTemplate: p.ValueTemplate,
			},
		})
	}
	seen := 0
	for _, dv := range reg.Snapshot() {
		for _, e := range dv.Entities {
			seen++
			if !e.TemplateSupported {
				t.Errorf("value_template for %q is not understood by the shared parser", e.UniqueID)
			}
		}
	}
	if seen != 7 {
		t.Fatalf("expected 7 entities in the snapshot, got %d", seen)
	}
}

// TestValueTemplateFieldsExistInBalance schützt die value_template-Strings
// davor, auf ein umbenanntes energy.Balance-Feld zu zeigen.
func TestValueTemplateFieldsExistInBalance(t *testing.T) {
	var b energy.Balance
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var balanceKeys map[string]any
	if err := json.Unmarshal(raw, &balanceKeys); err != nil {
		t.Fatal(err)
	}
	// object_id -> erwartetes balance-Feld
	want := map[string]string{
		"pv_power":          "pv",
		"grid_import":       "grid_import",
		"grid_export":       "grid_export",
		"battery_charge":    "battery_charge",
		"battery_discharge": "battery_discharge",
		"house_load":        "load_total",
		"battery_soc":       "battery_soc",
	}
	for objectID, field := range want {
		if _, ok := balanceKeys[field]; !ok {
			t.Fatalf("%s: value_template field %q is not a JSON key of energy.Balance", objectID, field)
		}
		p := configByObjectID(t, "homeassistant", "1", true, objectID)
		tmpl, _ := p["value_template"].(string)
		exp := "{{ value_json.balance['" + field + "'] }}"
		if tmpl != exp {
			t.Fatalf("%s: value_template = %q, want %q", objectID, tmpl, exp)
		}
	}
}
