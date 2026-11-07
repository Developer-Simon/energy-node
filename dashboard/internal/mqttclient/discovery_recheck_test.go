package mqttclient

import (
	"context"
	"strings"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

const (
	recheckTopic   = "homeassistant/sensor/wallbox/power/config"
	recheckPayload = `{"name":"Power","unique_id":"wallbox_power","state_topic":"wallbox/power","device":{"identifiers":["wallbox"],"name":"Wallbox"}}`
	// recheckQuiet is how long a test waits to be sure no re-subscribe follows.
	recheckQuiet = 50 * time.Millisecond
)

// discoveryMessage is a discovery delivery with a chosen retain flag.
type discoveryMessage struct {
	fakeMessage
	retained bool
}

func (m discoveryMessage) Retained() bool { return m.retained }

// recordingPaho reports Subscribe calls for the discovery filter on a channel.
// State topic subscribes of newly registered entities are of no interest here.
type recordingPaho struct {
	fakePaho
	subscribes chan string
}

func (p recordingPaho) Subscribe(filter string, _ byte, _ mqtt.MessageHandler) mqtt.Token {
	if strings.Contains(filter, "+") {
		p.subscribes <- filter
	}
	return fakeToken{}
}

func newRecheckClient(t *testing.T) (*Client, recordingPaho) {
	t.Helper()
	client := NewWithContext(context.Background(), Config{}, registry.New(), nil)
	t.Cleanup(client.Close)
	broker := recordingPaho{fakePaho: fakePaho{connected: true}, subscribes: make(chan string, 8)}
	client.paho = broker
	client.discoveryRecheckDelay = time.Millisecond
	return client, broker
}

func deliverDiscovery(client *Client, retained bool) {
	client.handleDiscovery(nil, discoveryMessage{
		fakeMessage: fakeMessage{topic: recheckTopic, payload: []byte(recheckPayload)},
		retained:    retained,
	})
}

func expectResubscribe(t *testing.T, client *Client, broker recordingPaho) {
	t.Helper()
	select {
	case got := <-broker.subscribes:
		if want := client.discoveryTopicFilter(); got != want {
			t.Fatalf("re-subscribed to %q, want the discovery filter %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no re-subscribe to the discovery filter")
	}
}

func expectNoResubscribe(t *testing.T, broker recordingPaho) {
	t.Helper()
	select {
	case got := <-broker.subscribes:
		t.Fatalf("unexpected re-subscribe to %q", got)
	case <-time.After(recheckQuiet):
	}
}

// A subscriber that is already connected receives retain=0 even when the
// publisher set retain=true. Only a new subscribe makes the broker reveal what
// is really retained, so an unconfirmed live message has to trigger one.
func TestLiveDiscoveryTriggersRetainRecheck(t *testing.T) {
	client, broker := newRecheckClient(t)
	deliverDiscovery(client, false)
	expectResubscribe(t, client, broker)
}

func TestRetainedDiscoveryNeedsNoRecheck(t *testing.T) {
	client, broker := newRecheckClient(t)
	deliverDiscovery(client, true)
	expectNoResubscribe(t, broker)
}

// A service restart republishes discovery that is already confirmed.
func TestLiveDiscoveryOnConfirmedTopicNeedsNoRecheck(t *testing.T) {
	client, broker := newRecheckClient(t)
	deliverDiscovery(client, true)
	deliverDiscovery(client, false)
	expectNoResubscribe(t, broker)
}

// A publisher that really does not retain must not cost one full discovery
// replay per message.
func TestRetainRecheckRunsOncePerTopicAndConnection(t *testing.T) {
	client, broker := newRecheckClient(t)
	deliverDiscovery(client, false)
	expectResubscribe(t, client, broker)

	deliverDiscovery(client, false)
	expectNoResubscribe(t, broker)

	client.resetDiscoveryRecheck()
	deliverDiscovery(client, false)
	expectResubscribe(t, client, broker)
}

// A burst of new topics, as after a service restart, shares one replay.
func TestRetainRecheckBundlesTopics(t *testing.T) {
	client, broker := newRecheckClient(t)
	client.discoveryRecheckDelay = 20 * time.Millisecond
	deliverDiscovery(client, false)
	client.handleDiscovery(nil, discoveryMessage{
		fakeMessage: fakeMessage{
			topic:   "homeassistant/sensor/wallbox/energy/config",
			payload: []byte(`{"name":"Energy","unique_id":"wallbox_energy","state_topic":"wallbox/energy","device":{"identifiers":["wallbox"],"name":"Wallbox"}}`),
		},
	})
	expectResubscribe(t, client, broker)
	expectNoResubscribe(t, broker)
}

func TestRetainRecheckSkippedWhileDisconnected(t *testing.T) {
	client, broker := newRecheckClient(t)
	broker.connected = false
	client.paho = broker
	deliverDiscovery(client, false)
	expectNoResubscribe(t, broker)
}
