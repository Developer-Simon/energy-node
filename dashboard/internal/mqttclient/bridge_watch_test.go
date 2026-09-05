package mqttclient

import (
	"context"
	"testing"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

// fakeMessage is a minimal mqtt.Message for feeding handleBridgeState
// directly, without a real broker connection - none is available in this
// test environment (see the "Umsetzungsstand" note on Teil A's
// Reconfigure/rollback tests for the same limitation).
type fakeMessage struct {
	topic   string
	payload []byte
}

func (m fakeMessage) Duplicate() bool   { return false }
func (m fakeMessage) Qos() byte         { return 0 }
func (m fakeMessage) Retained() bool    { return true }
func (m fakeMessage) Topic() string     { return m.topic }
func (m fakeMessage) MessageID() uint16 { return 0 }
func (m fakeMessage) Payload() []byte   { return m.payload }
func (m fakeMessage) Ack()              {}

var _ mqtt.Message = fakeMessage{}

func TestSetBridgeWatchBuildsASingleTopicNeverAWildcard(t *testing.T) {
	client := NewWithContext(context.Background(), Config{}, registry.New(), nil)

	client.SetBridgeWatch("pi-aussenstandort-bridge")
	if got := client.bridgeWatchTopic; got != "$SYS/broker/connection/pi-aussenstandort-bridge/state" {
		t.Fatalf("bridgeWatchTopic = %q, want the single connection-state topic", got)
	}
	if got := client.BridgeStatus(); !got.Configured || got.Connected {
		t.Fatalf("BridgeStatus() = %#v, want Configured=true, Connected=false before any message arrives", got)
	}
}

func TestSetBridgeWatchEmptyIDClearsTheWatch(t *testing.T) {
	client := NewWithContext(context.Background(), Config{}, registry.New(), nil)
	client.SetBridgeWatch("pi-aussenstandort-bridge")

	client.SetBridgeWatch("")
	if client.bridgeWatchTopic != "" {
		t.Fatalf("bridgeWatchTopic = %q, want empty after clearing the watch", client.bridgeWatchTopic)
	}
	if got := client.BridgeStatus(); got.Configured {
		t.Fatalf("BridgeStatus() = %#v, want Configured=false after clearing the watch", got)
	}
}

func TestSetBridgeWatchIsANoOpForTheSameID(t *testing.T) {
	client := NewWithContext(context.Background(), Config{}, registry.New(), nil)
	client.SetBridgeWatch("pi-aussenstandort-bridge")
	client.handleBridgeState(nil, fakeMessage{topic: "$SYS/broker/connection/pi-aussenstandort-bridge/state", payload: []byte("1")})

	// Calling SetBridgeWatch again with the identical ID must not reset the
	// state that was just observed - only an actual ID change resets it.
	client.SetBridgeWatch("pi-aussenstandort-bridge")
	if got := client.BridgeStatus(); !got.Connected {
		t.Fatalf("BridgeStatus() = %#v, want Connected to survive a same-ID SetBridgeWatch call", got)
	}
}

func TestHandleBridgeStateParsesConnectedAndDisconnectedPayloads(t *testing.T) {
	client := NewWithContext(context.Background(), Config{}, registry.New(), nil)
	client.SetBridgeWatch("pi-aussenstandort-bridge")

	client.handleBridgeState(nil, fakeMessage{topic: "$SYS/broker/connection/pi-aussenstandort-bridge/state", payload: []byte("1")})
	if got := client.BridgeStatus(); !got.Connected || !got.Configured || got.UpdatedAt.IsZero() {
		t.Fatalf("BridgeStatus() = %#v, want Connected=true after payload \"1\"", got)
	}

	client.handleBridgeState(nil, fakeMessage{topic: "$SYS/broker/connection/pi-aussenstandort-bridge/state", payload: []byte("0")})
	if got := client.BridgeStatus(); got.Connected {
		t.Fatalf("BridgeStatus() = %#v, want Connected=false after payload \"0\"", got)
	}
}

func TestSetBridgeWatchWithoutAConnectionDoesNotPanic(t *testing.T) {
	// No Connect() was called, so client.paho is nil - SetBridgeWatch must
	// still record the intended watch topic (for the eventual onConnect
	// resubscribe) instead of dereferencing a nil paho client.
	client := NewWithContext(context.Background(), Config{}, registry.New(), nil)
	client.SetBridgeWatch("pi-aussenstandort-bridge")
	client.SetBridgeWatch("other-id")
	client.SetBridgeWatch("")
}
