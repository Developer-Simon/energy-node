package mqttclient

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// TestResult is the outcome of TestConnection - see its doc comment.
type TestResult struct {
	OK                    bool   `json:"ok"`
	DurationMS            int64  `json:"duration_ms,omitempty"`
	DiscoveryMessagesSeen int    `json:"discovery_messages_seen,omitempty"`
	Broker                string `json:"broker"`
	ErrorCode             string `json:"error_code,omitempty"`
	Message               string `json:"message,omitempty"`
}

// discoveryObservationWindow is how long TestConnection waits for retained
// discovery messages to arrive after subscribing, before disconnecting
// again. Retained messages are delivered by the broker immediately after
// SUBACK, so this only needs to cover network round-trip time, not a real
// observation period.
const discoveryObservationWindow = 300 * time.Millisecond

// TestConnection opens a short-lived, separate connection to verify a
// candidate Config before it is saved (or reconnected to). It never touches
// the caller's own Client/paho connection: it uses its own client ID
// (<client_id>-test) so the broker does not evict the production session,
// and it always disconnects again before returning.
func TestConnection(cfg Config, timeout time.Duration) TestResult {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	broker := cfg.Host + ":" + cfg.Port
	start := time.Now()

	clientID := cfg.ClientID
	if clientID == "" {
		clientID = "energy-node-dashboard"
	}

	opts := mqtt.NewClientOptions()
	scheme := "tcp"
	if cfg.TLS {
		scheme = "ssl"
	}
	opts.AddBroker(fmt.Sprintf("%s://%s:%s", scheme, cfg.Host, cfg.Port))
	opts.SetClientID(clientID + "-test")
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
		opts.SetPassword(cfg.Password)
	}
	if cfg.TLS {
		opts.SetTLSConfig(tlsConfig(cfg.TLSInsecure))
	}
	opts.SetAutoReconnect(false)
	opts.SetConnectRetry(false)
	opts.SetCleanSession(true)

	client := mqtt.NewClient(opts)
	token := client.Connect()
	if !token.WaitTimeout(timeout) {
		return TestResult{OK: false, Broker: broker, ErrorCode: "timeout", Message: "Verbindungsaufbau hat das Zeitlimit überschritten"}
	}
	if err := token.Error(); err != nil {
		return TestResult{OK: false, Broker: broker, ErrorCode: classifyConnectError(err), Message: err.Error()}
	}
	defer client.Disconnect(250)

	prefix := cfg.DiscoveryPrefix
	if prefix == "" {
		prefix = defaultDiscoveryPrefix
	}
	filter := prefix + "/+/+/+/config"
	var seen int32
	subscribeToken := client.Subscribe(filter, 0, func(_ mqtt.Client, _ mqtt.Message) {
		atomic.AddInt32(&seen, 1)
	})
	subscribeToken.WaitTimeout(timeout)
	time.Sleep(discoveryObservationWindow)

	return TestResult{
		OK:                    true,
		DurationMS:            time.Since(start).Milliseconds(),
		DiscoveryMessagesSeen: int(atomic.LoadInt32(&seen)),
		Broker:                broker,
	}
}

// classifyConnectError turns Paho's mostly-unstructured connection errors
// into the small, stable set of error codes the settings UI can show a
// tailored message for. Paho does not expose a typed error here, so this is
// a best-effort string match against the messages it actually produces.
func classifyConnectError(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "tls"), strings.Contains(message, "certificate"), strings.Contains(message, "x509"):
		return "tls_failed"
	case strings.Contains(message, "not authorized"), strings.Contains(message, "bad user name or password"), strings.Contains(message, "unauthorized"):
		return "auth_failed"
	case strings.Contains(message, "refused"):
		return "connection_refused"
	case strings.Contains(message, "timeout"), strings.Contains(message, "timed out"):
		return "timeout"
	default:
		return "connection_refused"
	}
}
