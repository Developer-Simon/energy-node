package energydiscovery

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLegacyCleanupClearsDashboardTree(t *testing.T) {
	msgs := LegacyCleanupMessages("homeassistant")
	topics := map[string]bool{}
	for _, m := range msgs {
		if len(m.Payload) != 0 {
			t.Fatalf("cleanup %s carries a payload", m.Topic)
		}
		topics[m.Topic] = true
	}
	for _, want := range []string{
		"outstation/dashboard/status/online",
		"outstation/dashboard/energy/balance",
		"homeassistant/sensor/dashboard_energy/pv_power/config",
	} {
		if !topics[want] {
			t.Errorf("missing cleanup topic %s", want)
		}
	}
}

func TestConfigsUseEnergyNodeIdentity(t *testing.T) {
	for _, m := range Configs("homeassistant", "v1", true) {
		var payload map[string]any
		json.Unmarshal(m.Payload, &payload)
		if payload["state_topic"] != "outstation/energy_node/energy/balance" {
			t.Fatalf("state_topic = %v", payload["state_topic"])
		}
		dev := payload["device"].(map[string]any)
		ids := dev["identifiers"].([]any)
		if ids[0] != "energy_node" {
			t.Fatalf("identifiers = %v", ids)
		}
		if !strings.HasPrefix(payload["unique_id"].(string), "energy_node_") {
			t.Fatalf("unique_id = %v", payload["unique_id"])
		}
	}
}
