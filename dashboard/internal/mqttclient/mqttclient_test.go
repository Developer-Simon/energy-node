package mqttclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func TestDiscoveryPrefixDefaultsToHomeassistant(t *testing.T) {
	client := NewWithContext(context.Background(), Config{}, registry.New(), nil)
	if got := client.discoveryPrefix(); got != "homeassistant" {
		t.Fatalf("discoveryPrefix() = %q, want %q for an empty Config.DiscoveryPrefix", got, "homeassistant")
	}
	if got := client.discoveryTopicFilter(); got != "homeassistant/+/+/+/config" {
		t.Fatalf("discoveryTopicFilter() = %q, want the 3-level homeassistant wildcard", got)
	}
}

func TestDiscoveryPrefixHonoursConfig(t *testing.T) {
	client := NewWithContext(context.Background(), Config{DiscoveryPrefix: "outstation-ha"}, registry.New(), nil)
	if got := client.discoveryPrefix(); got != "outstation-ha" {
		t.Fatalf("discoveryPrefix() = %q, want the configured prefix", got)
	}
	if got := client.discoveryTopicFilter(); got != "outstation-ha/+/+/+/config" {
		t.Fatalf("discoveryTopicFilter() = %q, want the configured prefix in the wildcard", got)
	}
}

func TestParseDiscoveryTopicHonoursConfiguredPrefix(t *testing.T) {
	if _, _, _, ok := parseDiscoveryTopic("homeassistant", "outstation-ha/sensor/device/object/config"); ok {
		t.Fatal("a topic under a different prefix must not match the default prefix")
	}
	component, deviceID, objectID, ok := parseDiscoveryTopic("outstation-ha", "outstation-ha/sensor/device/object/config")
	if !ok || component != "sensor" || deviceID != "device" || objectID != "object" {
		t.Fatalf("parseDiscoveryTopic with matching prefix = (%q,%q,%q,%v), want (sensor,device,object,true)", component, deviceID, objectID, ok)
	}
}

func TestRemovalUniqueIDHonoursConfiguredPrefix(t *testing.T) {
	if _, ok := removalUniqueID("homeassistant", "outstation-ha/sensor/device/object/config"); ok {
		t.Fatal("removalUniqueID must not match a topic under a different prefix")
	}
	uniqueID, ok := removalUniqueID("outstation-ha", "outstation-ha/sensor/device/object/config")
	if !ok || uniqueID != "device_object" {
		t.Fatalf("removalUniqueID = (%q,%v), want (device_object,true)", uniqueID, ok)
	}
}

func TestConnectTimeoutDefaultsWhenUnset(t *testing.T) {
	client := NewWithContext(context.Background(), Config{}, registry.New(), nil)
	if got := client.connectTimeout(); got != 10*time.Second {
		t.Fatalf("connectTimeout() = %v, want 10s default for ConnectTimeoutSec=0", got)
	}
	client.setConfig(Config{ConnectTimeoutSec: 45})
	if got := client.connectTimeout(); got != 45*time.Second {
		t.Fatalf("connectTimeout() = %v, want the configured 45s", got)
	}
}

func TestDisconnectLockedIsSafeWithoutAnActiveConnection(t *testing.T) {
	client := NewWithContext(context.Background(), Config{}, registry.New(), nil)
	// Must not panic even though Connect() was never called (c.paho is nil).
	client.disconnectLocked()
	if client.Status().Connected {
		t.Fatal("Status().Connected = true after disconnectLocked on a never-connected client")
	}
}

func TestClassifyConnectError(t *testing.T) {
	cases := []struct {
		message string
		want    string
	}{
		{"network Error : dial tcp 127.0.0.1:1883: connect: connection refused", "connection_refused"},
		{"not Authorized", "auth_failed"},
		{"Bad user name or password", "auth_failed"},
		{"x509: certificate signed by unknown authority", "tls_failed"},
		{"tls: handshake failure", "tls_failed"},
		{"context deadline exceeded (timed out)", "timeout"},
	}
	for _, testCase := range cases {
		if got := classifyConnectError(errors.New(testCase.message)); got != testCase.want {
			t.Errorf("classifyConnectError(%q) = %q, want %q", testCase.message, got, testCase.want)
		}
	}
}

func TestCredentialStoreSaveLoadDelete(t *testing.T) {
	store := NewCredentialStore(t.TempDir())
	if _, err := store.Load(); err == nil {
		t.Fatal("expected an error loading credentials before any Save")
	}
	if err := store.Save(Credentials{Password: ""}); err == nil {
		t.Fatal("expected Save to reject an empty password")
	}
	if err := store.Save(Credentials{Password: "s3cret"}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "s3cret" {
		t.Fatalf("Load().Password = %q, want %q", got.Password, "s3cret")
	}
	if err := store.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("expected an error loading credentials after Delete")
	}
	// Deleting again (already-absent file) must not be an error.
	if err := store.Delete(); err != nil {
		t.Fatalf("Delete on an already-absent file returned an error: %v", err)
	}
}

func TestBuildOptionsSetsRetainedWillWhenAvailabilityTopicSet(t *testing.T) {
	client := NewWithContext(context.Background(), Config{
		Host: "h", Port: "1883", ClientID: "c",
		AvailabilityTopic: "outstation/dashboard/status/online",
	}, registry.New(), nil)

	opts := client.buildOptions()

	if !opts.WillEnabled {
		t.Fatal("WillEnabled = false, want true when AvailabilityTopic is set")
	}
	if opts.WillTopic != "outstation/dashboard/status/online" {
		t.Fatalf("WillTopic = %q", opts.WillTopic)
	}
	if string(opts.WillPayload) != "0" {
		t.Fatalf("WillPayload = %q, want \"0\"", opts.WillPayload)
	}
	if !opts.WillRetained {
		t.Fatal("WillRetained = false, want true")
	}
}

func TestBuildOptionsNoWillWithoutAvailabilityTopic(t *testing.T) {
	client := NewWithContext(context.Background(), Config{Host: "h", Port: "1883", ClientID: "c"}, registry.New(), nil)
	if client.buildOptions().WillEnabled {
		t.Fatal("WillEnabled = true, want false when AvailabilityTopic is empty")
	}
}

func TestDiscoveryPrefixAccessor(t *testing.T) {
	client := NewWithContext(context.Background(), Config{DiscoveryPrefix: "ha-test"}, registry.New(), nil)
	if got := client.DiscoveryPrefix(); got != "ha-test" {
		t.Fatalf("DiscoveryPrefix() = %q, want \"ha-test\"", got)
	}
	empty := NewWithContext(context.Background(), Config{}, registry.New(), nil)
	if got := empty.DiscoveryPrefix(); got != "homeassistant" {
		t.Fatalf("DiscoveryPrefix() = %q, want default \"homeassistant\"", got)
	}
}

func TestSetConnectPublisherStoresCallback(t *testing.T) {
	client := NewWithContext(context.Background(), Config{}, registry.New(), nil)
	called := false
	client.SetConnectPublisher(func() []OutboundMessage {
		called = true
		return []OutboundMessage{{Topic: "t", Payload: "p", Retain: true}}
	})
	if client.connectPublisher == nil {
		t.Fatal("connectPublisher not stored")
	}
	msgs := client.connectPublisher()
	if !called || len(msgs) != 1 || msgs[0].Topic != "t" || !msgs[0].Retain {
		t.Fatalf("stored callback misbehaves: called=%v msgs=%+v", called, msgs)
	}
}

// TestWatchTopicsPersistsTheListAndHandler - WatchTopics muss die rohe
// Topic-Liste und den Handler festhalten, damit onConnect sie nach jedem
// (Re-)Connect neu abonnieren kann (Muster wie SetBridgeWatch).
func TestWatchTopicsPersistsTheListAndHandler(t *testing.T) {
	client := NewWithContext(context.Background(), Config{}, registry.New(), nil)
	client.WatchTopics(
		[]string{"outstation/apsystems/status/online", "outstation/apsystems/settings/status"},
		func(topic string, payload []byte) {},
	)
	client.mu.Lock()
	got := append([]string(nil), client.watchTopics...)
	hasHandler := client.watchHandler != nil
	client.mu.Unlock()
	if len(got) != 2 || got[0] != "outstation/apsystems/status/online" || got[1] != "outstation/apsystems/settings/status" {
		t.Fatalf("watchTopics = %v, want the two raw topics verbatim", got)
	}
	if !hasHandler {
		t.Fatal("watchHandler not stored")
	}
}

// TestWatchTopicsDeliversMessagesToTheHandler feeds a message through the
// single delivery path (dispatchWatch) the Paho subscribe callback uses -
// no broker is available in this test environment, same limitation as the
// handleBridgeState tests.
func TestWatchTopicsDeliversMessagesToTheHandler(t *testing.T) {
	client := NewWithContext(context.Background(), Config{}, registry.New(), nil)
	got := make(chan string, 1)
	client.WatchTopics([]string{"outstation/apsystems/status/online"}, func(topic string, payload []byte) {
		got <- topic + "=" + string(payload)
	})
	client.dispatchWatch(fakeMessage{topic: "outstation/apsystems/status/online", payload: []byte("1")})
	select {
	case v := <-got:
		if v != "outstation/apsystems/status/online=1" {
			t.Fatalf("got %q", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch handler not called")
	}
}
