package mqttclient

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

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
		AvailabilityTopic: "outstation/energy_node/status/online",
	}, registry.New(), nil)

	opts := client.buildOptions()

	if !opts.WillEnabled {
		t.Fatal("WillEnabled = false, want true when AvailabilityTopic is set")
	}
	if opts.WillTopic != "outstation/energy_node/status/online" {
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

// fakePaho only answers IsConnected and Disconnect. The embedded nil interface makes any
// other method call panic, which is what we want: the retry loop must not
// touch the broker client beyond that check.
type fakePaho struct {
	mqtt.Client
	connected bool
}

func (f fakePaho) IsConnected() bool { return f.connected }

// Disconnect is reached from Close on a connected fake. It is a no-op here.
func (f fakePaho) Disconnect(uint) {}

func newRetryTestClient(delay time.Duration) *Client {
	client := NewWithContext(context.Background(), Config{}, registry.New(), nil)
	client.retryDelay = func(int) time.Duration { return delay }
	return client
}

func waitForCount(t *testing.T, counter *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for counter.Load() < want {
		if time.Now().After(deadline) {
			t.Fatalf("attempt called %d times, want at least %d", counter.Load(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestStartConnectingReturnsTrueOnFirstSuccess(t *testing.T) {
	client := newRetryTestClient(time.Millisecond)
	var calls atomic.Int32
	client.attempt = func() error {
		calls.Add(1)
		return nil
	}

	if !client.StartConnecting() {
		t.Fatal("StartConnecting() = false, want true on first success")
	}
	if got := client.Status().FailedAttempts; got != 0 {
		t.Fatalf("FailedAttempts = %d, want 0", got)
	}
	time.Sleep(30 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("attempt called %d times, want exactly 1 (no retry loop on success)", got)
	}
}

func TestStartConnectingRetriesUntilSuccess(t *testing.T) {
	client := newRetryTestClient(time.Millisecond)
	var calls atomic.Int32
	client.attempt = func() error {
		if calls.Add(1) <= 2 {
			return errors.New("not Authorized")
		}
		return nil
	}

	if client.StartConnecting() {
		t.Fatal("StartConnecting() = true, want false when the first attempt fails")
	}
	if got := client.Status().LastError; got != "not Authorized" {
		t.Fatalf("LastError = %q, want %q", got, "not Authorized")
	}
	waitForCount(t, &calls, 3)
	if got := calls.Load(); got != 3 {
		t.Fatalf("attempt called %d times, want exactly 3", got)
	}
	deadline := time.Now().Add(2 * time.Second)
	for client.Status().LastError != "" {
		if time.Now().After(deadline) {
			t.Fatal("LastError not cleared after the successful retry")
		}
		time.Sleep(time.Millisecond)
	}
	if got := client.Status().FailedAttempts; got != 2 {
		t.Fatalf("FailedAttempts = %d, want 2", got)
	}
	client.Close()
}

func TestStartConnectingStopsOnClose(t *testing.T) {
	client := newRetryTestClient(20 * time.Millisecond)
	var calls atomic.Int32
	client.attempt = func() error {
		calls.Add(1)
		return errors.New("connection refused")
	}

	client.StartConnecting()
	waitForCount(t, &calls, 2)

	client.Close()
	// One in-flight attempt may still finish after Close; afterwards the
	// counter must stop moving.
	time.Sleep(60 * time.Millisecond)
	before := calls.Load()
	time.Sleep(100 * time.Millisecond)
	if after := calls.Load(); after != before {
		t.Fatalf("attempt still running after Close: %d -> %d calls", before, after)
	}
}

func TestStartConnectingSkipsAttemptWhenAlreadyConnected(t *testing.T) {
	client := newRetryTestClient(time.Millisecond)
	var calls atomic.Int32
	client.attempt = func() error {
		calls.Add(1)
		return errors.New("not Authorized")
	}
	// Connected through another path before the loop's next round.
	client.mu.Lock()
	client.paho = fakePaho{connected: true}
	client.status.Connected = true
	client.mu.Unlock()

	if client.StartConnecting() {
		t.Fatal("StartConnecting() = true, want false for the failing first attempt")
	}
	// Give the loop several rounds to (not) run.
	time.Sleep(100 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("attempt called %d times, want 1 (only the synchronous first try)", got)
	}
	client.Close()
}

func TestStartConnectingAttemptsRunUnderReloadMu(t *testing.T) {
	client := newRetryTestClient(time.Millisecond)
	var calls atomic.Int32
	client.attempt = func() error {
		n := calls.Add(1)
		if n == 1 {
			return errors.New("not Authorized")
		}
		if client.reloadMu.TryLock() {
			client.reloadMu.Unlock()
			t.Error("retry attempt ran without holding reloadMu")
		}
		return nil
	}

	client.reloadMu.Lock()
	client.StartConnecting()
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		client.reloadMu.Unlock()
		t.Fatalf("retry ran while reloadMu was held: %d calls", got)
	}
	client.reloadMu.Unlock()

	waitForCount(t, &calls, 2)
	client.Close()
}

// scriptedAttempt replaces c.attempt for the Reconfigure tests. While err is
// set, every try fails with it. Once err is nil, a try leaves the Client in
// the state a successful connect leaves behind: a connected fake paho.
type scriptedAttempt struct {
	mu    sync.Mutex
	err   error
	calls int
}

func (s *scriptedAttempt) install(client *Client) {
	client.attempt = func() error {
		s.mu.Lock()
		s.calls++
		err := s.err
		s.mu.Unlock()
		if err != nil {
			return err
		}
		client.mu.Lock()
		client.paho = fakePaho{connected: true}
		client.mu.Unlock()
		return nil
	}
}

func (s *scriptedAttempt) setErr(err error) {
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
}

func (s *scriptedAttempt) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func waitForAttempts(t *testing.T, s *scriptedAttempt, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for s.count() < want {
		if time.Now().After(deadline) {
			t.Fatalf("attempt called %d times, want at least %d", s.count(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestReconfigureAfterFailedStartStopsRetryLoop(t *testing.T) {
	client := newRetryTestClient(time.Millisecond)
	script := &scriptedAttempt{err: errors.New("not Authorized")}
	script.install(client)

	if client.StartConnecting() {
		t.Fatal("StartConnecting() = true, want false for the failing first attempt")
	}
	waitForAttempts(t, script, 2)

	script.setErr(nil)
	if err := client.Reconfigure(Config{Host: "good"}); err != nil {
		t.Fatalf("Reconfigure() error = %v, want nil", err)
	}
	// The loop may have succeeded just before Reconfigure, or its next round
	// sees the connected fake paho. Either way no attempt may run afterwards.
	afterReconfigure := script.count()
	time.Sleep(50 * time.Millisecond)
	if got := script.count(); got != afterReconfigure {
		t.Fatalf("attempt still running after a successful Reconfigure: %d -> %d calls", afterReconfigure, got)
	}
	if got := client.Status().LastError; got != "" {
		t.Fatalf("LastError = %q, want empty after a successful Reconfigure", got)
	}
	client.Close()
}

func TestReconfigureFailureAfterFailedStartKeepsRetrying(t *testing.T) {
	client := newRetryTestClient(time.Millisecond)
	script := &scriptedAttempt{err: errors.New("not Authorized")}
	script.install(client)

	client.StartConnecting()
	waitForAttempts(t, script, 2)

	script.setErr(errors.New("bad host"))
	err := client.Reconfigure(Config{Host: "typo"})
	if err == nil {
		t.Fatal("Reconfigure() error = nil, want the connect error")
	}
	if strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("Reconfigure() error = %q, want no rollback without a working configuration", err)
	}
	status := client.Status()
	if status.RolledBack {
		t.Fatal("RolledBack = true, want false when there was never a working configuration")
	}
	if !strings.Contains(status.LastError, "bad host") {
		t.Fatalf("LastError = %q, want the new configuration's error", status.LastError)
	}
	before := script.count()
	waitForAttempts(t, script, before+2)
	client.Close()
}

func TestDefaultRetryDelayDoublesAndCapsAtOneMinute(t *testing.T) {
	cases := map[int]time.Duration{
		1:  5 * time.Second,
		2:  10 * time.Second,
		3:  20 * time.Second,
		4:  40 * time.Second,
		5:  60 * time.Second,
		50: 60 * time.Second,
	}
	for attempt, want := range cases {
		if got := defaultRetryDelay(attempt); got != want {
			t.Errorf("defaultRetryDelay(%d) = %v, want %v", attempt, got, want)
		}
	}
}
