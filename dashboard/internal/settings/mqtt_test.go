package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStorePersistsMQTTConfigWithDefaults(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	got, err := store.LoadMQTT()
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Fatalf("got enabled=true before anything is saved, want false")
	}
	if got.Port != 1883 || got.ClientID != "energy-node-dashboard" || got.KeepaliveSeconds != 30 ||
		!got.CleanSession || got.DiscoveryPrefix != "homeassistant" || got.ConnectTimeoutSec != 10 {
		t.Fatalf("got %#v, want the documented defaults before anything is saved", got)
	}

	value := MQTTConfig{
		Enabled:           true,
		Host:              "mqtt.example.internal",
		Port:              8883,
		ClientID:          "energy-node-dashboard",
		Username:          "dashboard",
		TLS:               true,
		TLSInsecure:       false,
		KeepaliveSeconds:  60,
		CleanSession:      false,
		DiscoveryPrefix:   "homeassistant",
		ConnectTimeoutSec: 5,
	}
	if err := store.SaveMQTT(value); err != nil {
		t.Fatal(err)
	}
	got, err = store.LoadMQTT()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, value) {
		t.Fatalf("got %#v, want the saved value round-tripped", got)
	}

	// A hand-edited or pre-upgrade mqtt.json that omits a newer field (here:
	// everything but host/client_id/enabled) must fall back to the
	// documented default when loaded, not to Go's zero value - this is what
	// makes "port absent from the file" mean 1883 rather than an invalid 0.
	partial := []byte(`{"enabled": true, "host": "broker", "client_id": "id"}`)
	if err := os.WriteFile(filepath.Join(dir, "mqtt.json"), partial, 0600); err != nil {
		t.Fatal(err)
	}
	partialStore := NewStore(dir)
	if got, err = partialStore.LoadMQTT(); err != nil {
		t.Fatal(err)
	} else if got.Port != 1883 || got.KeepaliveSeconds != 30 || !got.CleanSession || got.ConnectTimeoutSec != 10 || got.DiscoveryPrefix != "homeassistant" {
		t.Fatalf("got %#v, want fields absent from the file defaulted on load", got)
	}
}

func TestSaveMQTTRejectsUnsafeValues(t *testing.T) {
	store := NewStore(t.TempDir())
	base := DefaultMQTT()
	base.Host = "broker.local"
	base.ClientID = "dashboard"

	cases := []struct {
		name    string
		mutate  func(MQTTConfig) MQTTConfig
		wantErr bool
	}{
		{"valid baseline", func(v MQTTConfig) MQTTConfig { return v }, false},
		{"port too low", func(v MQTTConfig) MQTTConfig { v.Port = 0; return v }, true},
		{"port too high", func(v MQTTConfig) MQTTConfig { v.Port = 70000; return v }, true},
		{"host with control character", func(v MQTTConfig) MQTTConfig { v.Host = "broker\n.local"; return v }, true},
		{"host with slash", func(v MQTTConfig) MQTTConfig { v.Host = "broker/local"; return v }, true},
		{"client id with space", func(v MQTTConfig) MQTTConfig { v.ClientID = "my dashboard"; return v }, true},
		{"discovery prefix with hash", func(v MQTTConfig) MQTTConfig { v.DiscoveryPrefix = "home#assistant"; return v }, true},
		{"discovery prefix with plus", func(v MQTTConfig) MQTTConfig { v.DiscoveryPrefix = "home+assistant"; return v }, true},
		{"discovery prefix with leading slash", func(v MQTTConfig) MQTTConfig { v.DiscoveryPrefix = "/homeassistant"; return v }, true},
		{"discovery prefix empty", func(v MQTTConfig) MQTTConfig { v.DiscoveryPrefix = ""; return v }, true},
		{"keepalive below minimum", func(v MQTTConfig) MQTTConfig { v.KeepaliveSeconds = 1; return v }, true},
		{"connect timeout above maximum", func(v MQTTConfig) MQTTConfig { v.ConnectTimeoutSec = 120; return v }, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := store.SaveMQTT(testCase.mutate(base))
			if testCase.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !testCase.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestMQTTRevisionsCanBeReadAndRestored(t *testing.T) {
	store := NewStore(t.TempDir())
	first := DefaultMQTT()
	first.Enabled = true
	first.Host = "broker-a"
	first.ClientID = "dashboard"
	second := first
	second.Host = "broker-b"

	if err := store.SaveMQTT(first); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMQTT(second); err != nil {
		t.Fatal(err)
	}
	revisions, err := store.MQTTRevisions()
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 {
		t.Fatalf("got %d revisions, want 1 (the pre-second-save copy of the first save)", len(revisions))
	}

	restored, err := store.RestoreMQTT(revisions[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Host != "broker-a" {
		t.Fatalf("got host %q after restore, want %q", restored.Host, "broker-a")
	}
	current, err := store.LoadMQTT()
	if err != nil {
		t.Fatal(err)
	}
	if current.Host != "broker-a" {
		t.Fatalf("got current host %q after restore, want %q", current.Host, "broker-a")
	}
}

func TestMQTTConfigMetricEnabledDefaultsTrue(t *testing.T) {
	if !DefaultMQTT().MetricEnabled("cpu_temp") {
		t.Fatal("MetricEnabled without a map = false, want true")
	}
	cfg := DefaultMQTT()
	cfg.Metrics = map[string]bool{"cpu_temp": false}
	if cfg.MetricEnabled("cpu_temp") {
		t.Fatal("MetricEnabled(cpu_temp) = true after setting false")
	}
	if !cfg.MetricEnabled("ram") {
		t.Fatal("MetricEnabled(ram) = false although only cpu_temp was disabled")
	}
}

func TestMQTTConfigPublishEnergyDeviceDefaultsTrue(t *testing.T) {
	if !DefaultMQTT().PublishEnergyDevice {
		t.Fatal("DefaultMQTT().PublishEnergyDevice = false, want true")
	}

	dir := t.TempDir()
	// Datei ohne das neue Feld -> muss beim Laden auf true defaulten.
	partial := []byte(`{"enabled": true, "host": "broker", "client_id": "id"}`)
	if err := os.WriteFile(filepath.Join(dir, "mqtt.json"), partial, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := NewStore(dir).LoadMQTT()
	if err != nil {
		t.Fatal(err)
	}
	if !got.PublishEnergyDevice {
		t.Fatalf("PublishEnergyDevice = false after loading a file without the key, want true")
	}

	// Explizit false speichern und laden -> bleibt false.
	off := DefaultMQTT()
	off.Host = "broker.local"
	off.ClientID = "dashboard"
	off.PublishEnergyDevice = false
	store := NewStore(t.TempDir())
	if err := store.SaveMQTT(off); err != nil {
		t.Fatal(err)
	}
	if roundTripped, err := store.LoadMQTT(); err != nil {
		t.Fatal(err)
	} else if roundTripped.PublishEnergyDevice {
		t.Fatal("PublishEnergyDevice = true after saving false, want false")
	}
}
