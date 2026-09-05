package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func validBridgeConnection() BridgeConnection {
	value := DefaultBridgeConnection()
	value.Enabled = true
	value.Name = "aussenstandort-zu-hauptsystem"
	value.Address = "100.64.1.2"
	value.RemoteClientID = "pi-aussenstandort-bridge"
	value.RemoteUsername = "ha"
	value.Topics = []BridgeTopic{{Pattern: "outstation/#", Direction: "both", QoS: 0}}
	return value
}

func TestStorePersistsBridgeConfigWithDefaults(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	got, err := store.LoadBridge()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Connections) != 0 {
		t.Fatalf("got %d connections before anything is saved, want 0", len(got.Connections))
	}

	value := BridgeConfig{Connections: []BridgeConnection{validBridgeConnection()}}
	if err := store.SaveBridge(value); err != nil {
		t.Fatal(err)
	}
	got, err = store.LoadBridge()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Connections) != 1 || got.Connections[0].Name != value.Connections[0].Name {
		t.Fatalf("got %#v, want the saved value round-tripped", got)
	}

	// A hand-edited or pre-upgrade bridge.json that omits newer fields must
	// fall back to the documented defaults, same reasoning as MQTTConfig.
	partial := []byte(`{"connections": [{"enabled": true, "name": "x", "address": "100.64.1.2", "remote_client_id": "id", "topics": [{"pattern": "a/#", "direction": "out", "qos": 0}]}]}`)
	if err := os.WriteFile(filepath.Join(dir, "bridge.json"), partial, 0600); err != nil {
		t.Fatal(err)
	}
	partialStore := NewStore(dir)
	got, err = partialStore.LoadBridge()
	if err != nil {
		t.Fatal(err)
	}
	connection := got.Connections[0]
	if connection.Port != 1883 || !connection.TryPrivate || !connection.StartTypeAuto || connection.RestartTimeout != 30 || connection.KeepaliveSeconds != 60 {
		t.Fatalf("got %#v, want fields absent from the file defaulted on load", connection)
	}
}

func TestSaveBridgeRejectsUnsafeValues(t *testing.T) {
	store := NewStore(t.TempDir())
	base := validBridgeConnection()

	cases := []struct {
		name    string
		mutate  func(BridgeConnection) BridgeConnection
		wantErr bool
	}{
		{"valid baseline", func(v BridgeConnection) BridgeConnection { return v }, false},
		{"name with space", func(v BridgeConnection) BridgeConnection { v.Name = "my bridge"; return v }, true},
		{"remote client id with space", func(v BridgeConnection) BridgeConnection { v.RemoteClientID = "my id"; return v }, true},
		{"address empty", func(v BridgeConnection) BridgeConnection { v.Address = ""; return v }, true},
		{"address with control character", func(v BridgeConnection) BridgeConnection { v.Address = "100.64.1.2\nlistener 1884"; return v }, true},
		{"address with newline injection attempt", func(v BridgeConnection) BridgeConnection {
			v.Address = "100.64.1.2\nlog_dest file /etc/passwd"
			return v
		}, true},
		{"remote username with control character", func(v BridgeConnection) BridgeConnection { v.RemoteUsername = "ha\r\nuser"; return v }, true},
		{"no topics", func(v BridgeConnection) BridgeConnection { v.Topics = nil; return v }, true},
		{"too many topics", func(v BridgeConnection) BridgeConnection {
			topics := make([]BridgeTopic, 33)
			for i := range topics {
				topics[i] = BridgeTopic{Pattern: "a/#", Direction: "out", QoS: 0}
			}
			v.Topics = topics
			return v
		}, true},
		{"bare wildcard topic", func(v BridgeConnection) BridgeConnection {
			v.Topics = []BridgeTopic{{Pattern: "#", Direction: "both", QoS: 0}}
			return v
		}, true},
		{"hash not in last segment", func(v BridgeConnection) BridgeConnection {
			v.Topics = []BridgeTopic{{Pattern: "a/#/b", Direction: "both", QoS: 0}}
			return v
		}, true},
		{"topic with space", func(v BridgeConnection) BridgeConnection {
			v.Topics = []BridgeTopic{{Pattern: "a b/#", Direction: "both", QoS: 0}}
			return v
		}, true},
		{"invalid direction", func(v BridgeConnection) BridgeConnection {
			v.Topics = []BridgeTopic{{Pattern: "a/#", Direction: "sideways", QoS: 0}}
			return v
		}, true},
		{"qos out of range", func(v BridgeConnection) BridgeConnection {
			v.Topics = []BridgeTopic{{Pattern: "a/#", Direction: "out", QoS: 3}}
			return v
		}, true},
		{"port too high", func(v BridgeConnection) BridgeConnection { v.Port = 70000; return v }, true},
		{"restart timeout too low", func(v BridgeConnection) BridgeConnection { v.RestartTimeout = 1; return v }, true},
		{"keepalive too low", func(v BridgeConnection) BridgeConnection { v.KeepaliveSeconds = 1; return v }, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := store.SaveBridge(BridgeConfig{Connections: []BridgeConnection{testCase.mutate(base)}})
			if testCase.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !testCase.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestSaveBridgeRejectsMoreThanOneConnection(t *testing.T) {
	store := NewStore(t.TempDir())
	base := validBridgeConnection()
	second := base
	second.Name = "second"
	if err := store.SaveBridge(BridgeConfig{Connections: []BridgeConnection{base, second}}); err == nil {
		t.Fatal("expected an error for more than one connection")
	}
}

func TestBridgeRevisionsAndRestore(t *testing.T) {
	store := NewStore(t.TempDir())
	first := validBridgeConnection()
	if err := store.SaveBridge(BridgeConfig{Connections: []BridgeConnection{first}}); err != nil {
		t.Fatal(err)
	}
	second := first
	second.Address = "100.64.1.3"
	if err := store.SaveBridge(BridgeConfig{Connections: []BridgeConnection{second}}); err != nil {
		t.Fatal(err)
	}
	revisions, err := store.BridgeRevisions()
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 {
		t.Fatalf("got %d revisions, want 1 (the pre-second-save state)", len(revisions))
	}
	restored, err := store.RestoreBridge(revisions[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Connections[0].Address != first.Address {
		t.Fatalf("got address %q after restore, want %q", restored.Connections[0].Address, first.Address)
	}
}

func TestBridgeAddressWarning(t *testing.T) {
	cases := []struct {
		address string
		wantOK  bool
	}{
		{"100.64.1.2", true},
		{"192.168.1.5", true},
		{"10.0.0.5", true},
		{"8.8.8.8", false},
		{"not-an-ip", false},
	}
	for _, testCase := range cases {
		got := BridgeAddressWarning(testCase.address)
		if testCase.wantOK && got != "" {
			t.Errorf("BridgeAddressWarning(%q) = %q, want no warning", testCase.address, got)
		}
		if !testCase.wantOK && got == "" {
			t.Errorf("BridgeAddressWarning(%q) = %q, want a warning", testCase.address, got)
		}
	}
}
