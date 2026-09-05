package mqttclient

import "testing"

func TestParseDiscoveryMessagePreservesIcon(t *testing.T) {
	disc, removed, err := parseDiscoveryMessage(
		"homeassistant",
		"homeassistant/sensor/battery/soc/config",
		[]byte(`{"name":"Battery SoC","unique_id":"battery_soc","icon":"mdi:battery","device":{"identifiers":"battery","name":"Battery"}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("discovery message was treated as a removal")
	}
	if disc.Entity.Icon != "mdi:battery" {
		t.Fatalf("icon = %q, want %q", disc.Entity.Icon, "mdi:battery")
	}
}

func TestParseDiscoveryMessagePreservesFirmware(t *testing.T) {
	disc, removed, err := parseDiscoveryMessage(
		"homeassistant",
		"homeassistant/sensor/node/temp/config",
		[]byte(`{"name":"Temperature","unique_id":"node_temp","device":{"identifiers":["node"],"sw_version":"2026.08"}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if removed || disc.Device.Firmware != "2026.08" {
		t.Fatalf("firmware = %q, removed = %v", disc.Device.Firmware, removed)
	}
}

func TestParseDiscoveryMessagePreservesCommandMetadata(t *testing.T) {
	disc, removed, err := parseDiscoveryMessage(
		"homeassistant",
		"homeassistant/switch/shelly/relay/config",
		[]byte(`{"name":"Relay","unique_id":"shelly_relay","command_topic":"shellies/shelly/relay/0/set","payload_on":"ON","payload_off":"OFF","device":{"identifiers":["shelly"]}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("discovery message was treated as a removal")
	}
	if disc.Entity.CommandTopic != "shellies/shelly/relay/0/set" || disc.Entity.PayloadOn != "ON" || disc.Entity.PayloadOff != "OFF" {
		t.Fatalf("command metadata = %#v", disc.Entity)
	}
}

func TestParseDiscoveryMessagePreservesNumberMetadata(t *testing.T) {
	disc, removed, err := parseDiscoveryMessage(
		"homeassistant",
		"homeassistant/number/node/interval/config",
		[]byte(`{"name":"Intervall","unique_id":"node_interval","command_topic":"node/interval/set","min":10,"max":60,"step":5,"device":{"identifiers":["node"]}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if removed || disc.Entity.MinValue == nil || *disc.Entity.MinValue != 10 || disc.Entity.MaxValue == nil || *disc.Entity.MaxValue != 60 || disc.Entity.Step == nil || *disc.Entity.Step != 5 {
		t.Fatalf("number metadata = %#v, removed = %v", disc.Entity, removed)
	}
}

func TestParseDiscoveryMessageAllowsMissingIcon(t *testing.T) {
	disc, removed, err := parseDiscoveryMessage(
		"homeassistant",
		"homeassistant/sensor/battery/voltage/config",
		[]byte(`{"name":"Battery Voltage","unique_id":"battery_voltage","device":{"identifiers":["battery"]}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("discovery message was treated as a removal")
	}
	if disc.Entity.Icon != "" {
		t.Fatalf("icon = %q, want empty", disc.Entity.Icon)
	}
}

func TestParseDiscoveryMessageMarksDisabledByDefaultEntityHidden(t *testing.T) {
	disc, removed, err := parseDiscoveryMessage(
		"homeassistant",
		"homeassistant/sensor/node/rssi/config",
		[]byte(`{"name":"Signal","unique_id":"node_rssi","enabled_by_default":false,"device":{"identifiers":["node"]}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("discovery message was treated as a removal")
	}
	if !disc.Entity.DefaultHidden {
		t.Fatal("entity with enabled_by_default=false was not marked hidden")
	}
}

func TestParseDiscoveryMessageKeepsMissingEnabledByDefaultVisible(t *testing.T) {
	disc, _, err := parseDiscoveryMessage(
		"homeassistant",
		"homeassistant/sensor/node/temperature/config",
		[]byte(`{"name":"Temperature","unique_id":"node_temperature","device":{"identifiers":["node"]}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if disc.Entity.DefaultHidden {
		t.Fatal("entity without enabled_by_default was marked hidden")
	}
}

func TestParseDiscoveryMessagePreservesConfigEntityCategory(t *testing.T) {
	disc, removed, err := parseDiscoveryMessage(
		"homeassistant",
		"homeassistant/number/node/interval/config",
		[]byte(`{"name":"Intervall","unique_id":"node_interval","entity_category":"config","device":{"identifiers":["node"]}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("discovery message was treated as a removal")
	}
	if disc.Entity.EntityCategory != "config" {
		t.Fatalf("entity_category = %q, want %q", disc.Entity.EntityCategory, "config")
	}
}

func TestParseDiscoveryMessagePreservesDiagnosticEntityCategory(t *testing.T) {
	disc, removed, err := parseDiscoveryMessage(
		"homeassistant",
		"homeassistant/sensor/node/rssi/config",
		[]byte(`{"name":"Signal","unique_id":"node_rssi","entity_category":"diagnostic","device":{"identifiers":["node"]}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("discovery message was treated as a removal")
	}
	if disc.Entity.EntityCategory != "diagnostic" {
		t.Fatalf("entity_category = %q, want %q", disc.Entity.EntityCategory, "diagnostic")
	}
}

func TestParseDiscoveryMessageAllowsMissingEntityCategory(t *testing.T) {
	disc, _, err := parseDiscoveryMessage(
		"homeassistant",
		"homeassistant/sensor/node/temperature/config",
		[]byte(`{"name":"Temperature","unique_id":"node_temperature","device":{"identifiers":["node"]}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if disc.Entity.EntityCategory != "" {
		t.Fatalf("entity_category = %q, want empty", disc.Entity.EntityCategory)
	}
}

func TestParseDiscoveryMessagePreservesAvailabilityArrayAndMode(t *testing.T) {
	disc, removed, err := parseDiscoveryMessage(
		"homeassistant",
		"homeassistant/sensor/node/temperature/config",
		[]byte(`{"name":"Temperature","unique_id":"node_temperature","availability_mode":"any","availability":[{"topic":"status/node","payload_available":"online"},{"topic":"status/bridge","payload_available":"1"}],"device":{"identifiers":["node"]}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if removed || disc.Entity.AvailabilityMode != "any" || len(disc.Entity.Availability) != 2 {
		t.Fatalf("availability = %#v, removed = %v", disc.Entity, removed)
	}
	if disc.Entity.Availability[1].Topic != "status/bridge" || disc.Entity.Availability[1].PayloadAvailable != "1" {
		t.Fatalf("availability array = %#v", disc.Entity.Availability)
	}
}

func TestParseDiscoveryMessageRejectsAvailabilityWithoutTopic(t *testing.T) {
	_, removed, err := parseDiscoveryMessage(
		"homeassistant",
		"homeassistant/sensor/node/temperature/config",
		[]byte(`{"name":"Temperature","availability":[{"payload_available":"online"}]}`),
	)
	if err == nil || removed {
		t.Fatalf("expected invalid availability error, removed = %v", removed)
	}
}
