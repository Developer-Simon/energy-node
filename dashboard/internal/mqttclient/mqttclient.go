// Package mqttclient holds the Eclipse Paho Go MQTT client that subscribes
// to Home Assistant MQTT Discovery topics (3-level topic convention, see
// dashboard/mqtt-topics-und-discovery-format.md), parses discovered
// entities, and dynamically subscribes to each entity's state_topic and
// availability_topic. Everything it learns is fed into a
// registry.Registry; this package itself holds no device state.
package mqttclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/Developer-Simon/energy-node-dashboard/internal/devicefilter"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

var errNotADiscoveryTopic = errors.New("mqttclient: not a 3-level homeassistant discovery topic")

// tlsConfig builds the *tls.Config for a TLS broker connection. insecure
// disables certificate verification - only meant for a self-signed broker
// on the local network, never the default.
func tlsConfig(insecure bool) *tls.Config {
	return &tls.Config{InsecureSkipVerify: insecure}
}

// defaultDiscoveryPrefix is used whenever Config.DiscoveryPrefix is empty,
// which keeps every existing deployment (no DiscoveryPrefix set) behaving
// exactly as before this field existed.
const defaultDiscoveryPrefix = "homeassistant"

// Config holds the MQTT broker connection settings, normally sourced from
// energy_node_dashboard.env, or from the dashboard's own mqtt.json
// settings document once Reconfigure is used.
type Config struct {
	Host     string
	Port     string
	Username string
	Password string
	ClientID string

	TLS               bool
	TLSInsecure       bool
	KeepaliveSeconds  int
	CleanSession      bool
	DiscoveryPrefix   string
	ConnectTimeoutSec int

	// AvailabilityTopic wird, wenn gesetzt, als retained MQTT-LWT ("0")
	// registriert und bei jedem erfolgreichen Connect als retained Birth
	// ("1") veröffentlicht. HA-Discovery-Entitäten, die ihr
	// availability_topic hierauf zeigen lassen, gehen damit "unavailable",
	// sobald das Dashboard stirbt. Leer = keine LWT/Birth.
	AvailabilityTopic string

	// Source records where this Config came from ("settings", "env" or
	// "default"); it is not used to build the connection, only copied into
	// Status so the UI can display it without a second source of truth.
	Source string
}

type Status struct {
	Connected          bool      `json:"connected"`
	LastConnectedAt    time.Time `json:"last_connected_at,omitempty"`
	LastDisconnectedAt time.Time `json:"last_disconnected_at,omitempty"`
	LastReloadAt       time.Time `json:"last_reload_at,omitempty"`
	LastReconfiguredAt time.Time `json:"last_reconfigured_at,omitempty"`
	LastError          string    `json:"last_error,omitempty"`
	FailedAttempts     int       `json:"failed_attempts,omitempty"`
	RolledBack         bool      `json:"rolled_back,omitempty"`
	Source             string    `json:"source,omitempty"`
	// BrokerAddress is host:port without credentials, safe to expose in an
	// HTTP response.
	BrokerAddress string `json:"broker_address,omitempty"`
}

type StatusProvider interface {
	Status() Status
}

// OutboundMessage ist eine bei Connect zu veröffentlichende Nachricht.
type OutboundMessage struct {
	Topic   string
	Payload string
	Retain  bool
}

// BridgeConnectionState reports whether the Mosquitto bridge to the
// Hauptsystem is currently connected, as observed on the single
// $SYS/broker/connection/<remote_clientid>/state topic Client subscribes to
// once SetBridgeWatch is called - never a $SYS/# wildcard, see AGENTS.md's
// broker-wide-wildcard invariant.
type BridgeConnectionState struct {
	Configured bool      `json:"configured"`
	Connected  bool      `json:"connected"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}

type DeviceFilter interface {
	IsIgnored(deviceID string) bool
	Ignore(view registry.DeviceView) error
	Unignore(deviceID string) error
	MarkPresent(discovery registry.Discovery) error
	MarkMissing(topic string) error
	DiscoveryTopics(deviceID string) []string
}

type presenceEpoch interface {
	BeginPresenceEpoch() error
	CompletePresenceEpoch() error
}

const discoveryEpochGrace = 500 * time.Millisecond

// subscriptionOp is one queued subscribe/unsubscribe request - see
// subscriptionWorker's doc comment for why both must go through this queue
// rather than being called directly from the Paho router goroutine.
type subscriptionOp struct {
	topic     string
	subscribe bool
}

// Client wraps the Eclipse Paho MQTT client with discovery-driven dynamic
// subscription and feeds everything it learns into a registry.Registry.
// Reconnects (including re-subscribing to every dynamically discovered
// topic) are handled automatically.
type Client struct {
	cfg    Config
	reg    *registry.Registry
	logger *log.Logger
	ctx    context.Context
	cancel context.CancelFunc

	paho mqtt.Client

	mu                sync.Mutex
	reloadMu          sync.Mutex
	epochMu           sync.Mutex
	logOnce           sync.Once
	discoveryEpoch    uint64
	subscribed        map[string]bool
	subscribeQueue    chan subscriptionOp
	stateObserver     func([]registry.StateChange)
	discoveryObserver func(registry.Discovery)
	filter            DeviceFilter
	status            Status
	verbose           bool

	bridgeWatchTopic string
	bridgeState      BridgeConnectionState

	watchTopics  []string
	watchHandler func(string, []byte)

	connectPublisher func() []OutboundMessage
}

// SetVerboseLogging toggles the per-message discovery/state log lines. They
// are off by default outside DASHBOARD_LOG_LEVEL=debug because on a Pi they
// write straight to journald (and from there to the SD card) on every single
// MQTT message.
func (c *Client) SetVerboseLogging(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.verbose = enabled
}

func (c *Client) verboseLogging() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.verbose
}

func (c *Client) SetStateObserver(observer func([]registry.StateChange)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stateObserver = observer
}

func (c *Client) SetDiscoveryObserver(observer func(registry.Discovery)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.discoveryObserver = observer
}

func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// SetConnectPublisher hinterlegt einen Rückruf, dessen Nachrichten am Ende
// jedes erfolgreichen onConnect (also auch nach jedem Reconnect)
// veröffentlicht werden. Dient dem Dashboard dazu, seine eigene
// HA-Discovery erneut retained abzusetzen. nil = kein Publish.
func (c *Client) SetConnectPublisher(fn func() []OutboundMessage) {
	c.mu.Lock()
	c.connectPublisher = fn
	c.mu.Unlock()
}

// DiscoveryPrefix liefert den effektiven HA-Discovery-Präfix (Default
// "homeassistant"), damit Aufrufer Discovery-Topics unter demselben Präfix
// bauen können, unter dem der Client lauscht.
func (c *Client) DiscoveryPrefix() string {
	return c.discoveryPrefix()
}

// SetBridgeWatch (re)subscribes to the one $SYS topic that reports whether
// the Mosquitto bridge to the Hauptsystem is connected -
// $SYS/broker/connection/<remoteClientID>/state, never a $SYS/# wildcard.
// remoteClientID == "" clears the watch (unsubscribes and reports
// Configured: false). Safe to call repeatedly with the same ID - it is a
// no-op unless the ID actually changed. onConnect re-subscribes the current
// topic automatically after any reconnect, same as the dynamic discovery
// topics.
func (c *Client) SetBridgeWatch(remoteClientID string) {
	topic := ""
	if remoteClientID != "" {
		topic = "$SYS/broker/connection/" + remoteClientID + "/state"
	}
	c.mu.Lock()
	previous := c.bridgeWatchTopic
	if previous == topic {
		c.mu.Unlock()
		return
	}
	c.bridgeWatchTopic = topic
	c.bridgeState = BridgeConnectionState{Configured: topic != ""}
	paho := c.paho
	c.mu.Unlock()

	if paho == nil || !paho.IsConnected() {
		return
	}
	if previous != "" {
		if token := paho.Unsubscribe(previous); token.Wait() && token.Error() != nil {
			c.logger.Printf("bridge watch unsubscribe %s failed: %v", previous, token.Error())
		}
	}
	if topic != "" {
		if token := paho.Subscribe(topic, 0, c.handleBridgeState); token.Wait() && token.Error() != nil {
			c.logger.Printf("bridge watch subscribe %s failed: %v", topic, token.Error())
		}
	}
}

// WatchTopics abonniert eine feste Liste roher Topics und leitet jede
// Nachricht an handler weiter. Die Liste wird bei jedem (Re-)Connect neu
// abonniert. Gedacht fuer internal/nodeagent (Bridge-Liveness) - NICHT fuer
// HA-Discovery, die laeuft ueber die Registry.
func (c *Client) WatchTopics(topics []string, handler func(topic string, payload []byte)) {
	c.mu.Lock()
	c.watchTopics = append([]string(nil), topics...)
	c.watchHandler = handler
	paho := c.paho
	c.mu.Unlock()
	c.subscribeWatchTopics(paho)
}

// subscribeWatchTopics (re)subscribes the current watch list on client. It is
// called from onConnect with the callback's client parameter (c.paho is not
// assigned yet at that point, exactly as for the discovery/state/bridge
// re-subscribes) and from WatchTopics with c.paho.
func (c *Client) subscribeWatchTopics(client mqtt.Client) {
	c.mu.Lock()
	topics := append([]string(nil), c.watchTopics...)
	handler := c.watchHandler
	c.mu.Unlock()
	if client == nil || !client.IsConnected() || handler == nil {
		return
	}
	for _, topic := range topics {
		t := topic
		if token := client.Subscribe(t, 0, func(_ mqtt.Client, m mqtt.Message) {
			c.dispatchWatch(m)
		}); token.Wait() && token.Error() != nil {
			c.logger.Printf("watch subscribe %s: %v", t, token.Error())
		}
	}
}

// dispatchWatch forwards one watched message to the registered handler. It is
// the single delivery path for WatchTopics - the Paho subscribe callback and
// the tests both go through here (no broker is available in the test
// environment, same limitation as handleBridgeState).
func (c *Client) dispatchWatch(msg mqtt.Message) {
	c.mu.Lock()
	handler := c.watchHandler
	c.mu.Unlock()
	if handler != nil {
		handler(msg.Topic(), msg.Payload())
	}
}

// BridgeStatus returns the last known state of the watched bridge
// connection topic - see SetBridgeWatch.
func (c *Client) BridgeStatus() BridgeConnectionState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bridgeState
}

// handleBridgeState parses the retained "1"/"0" payload Mosquitto publishes
// on a bridge's $SYS connection-state topic.
func (c *Client) handleBridgeState(_ mqtt.Client, msg mqtt.Message) {
	connected := string(msg.Payload()) == "1"
	c.mu.Lock()
	c.bridgeState = BridgeConnectionState{Configured: true, Connected: connected, UpdatedAt: time.Now().UTC()}
	c.mu.Unlock()
}

// New creates a Client. Call Connect to actually open the MQTT connection.
// A nil logger falls back to the standard library's default logger.
func New(cfg Config, reg *registry.Registry, logger *log.Logger) *Client {
	return NewWithContext(context.Background(), cfg, reg, logger)
}

func NewWithContext(parent context.Context, cfg Config, reg *registry.Registry, logger *log.Logger) *Client {
	return NewWithContextAndFilter(parent, cfg, reg, logger, nil)
}

func NewWithContextAndFilter(parent context.Context, cfg Config, reg *registry.Registry, logger *log.Logger, filter DeviceFilter) *Client {
	if logger == nil {
		logger = log.Default()
	}
	ctx, cancel := context.WithCancel(parent)
	c := &Client{
		cfg:            cfg,
		reg:            reg,
		logger:         logger,
		ctx:            ctx,
		cancel:         cancel,
		subscribed:     make(map[string]bool),
		subscribeQueue: make(chan subscriptionOp, 256),
		filter:         filter,
	}
	go c.subscriptionWorker()
	return c
}

func (c *Client) SetDeviceFilter(filter DeviceFilter) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.filter = filter
}

func (c *Client) deviceFilter() DeviceFilter {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.filter
}

func (c *Client) IgnoreDevice(deviceID string) error {
	filter := c.deviceFilter()
	if filter == nil {
		return errors.New("mqttclient: device filter is not configured")
	}
	view, ok := c.reg.Get(deviceID)
	if !ok {
		return errors.New("mqttclient: device not found")
	}
	if err := filter.Ignore(view); err != nil {
		return err
	}
	orphans, removed := c.reg.RemoveDevice(deviceID)
	if !removed {
		_ = filter.Unignore(deviceID)
		return errors.New("mqttclient: device disappeared during ignore")
	}
	for _, topic := range orphans {
		c.unsubscribeState(topic)
	}
	return nil
}

func (c *Client) UnignoreDevice(deviceID string) error {
	filter := c.deviceFilter()
	if filter == nil {
		return errors.New("mqttclient: device filter is not configured")
	}
	if !filter.IsIgnored(deviceID) {
		return errors.New("mqttclient: device is not ignored")
	}
	var record devicefilter.DeviceRecord
	if store, ok := filter.(*devicefilter.Store); ok {
		record, _ = store.Get(deviceID)
	}
	if err := filter.Unignore(deviceID); err != nil {
		return err
	}
	if err := c.Reload(); err != nil {
		if restorer, ok := filter.(interface {
			Restore(devicefilter.DeviceRecord) error
		}); ok {
			if restoreErr := restorer.Restore(record); restoreErr != nil {
				return fmt.Errorf("%w; ignore rollback failed: %v", err, restoreErr)
			}
		}
		return err
	}
	return nil
}

func (c *Client) DeleteDeviceDiscovery(deviceID string) error {
	if c.paho == nil || !c.paho.IsConnected() {
		return errors.New("mqttclient: cannot delete discovery while disconnected")
	}
	topics := c.reg.DiscoveryTopics(deviceID)
	if len(topics) == 0 {
		if filter := c.deviceFilter(); filter != nil {
			topics = filter.DiscoveryTopics(deviceID)
		}
	}
	if len(topics) == 0 {
		return errors.New("mqttclient: device has no discovery topics")
	}
	for _, topic := range topics {
		token := c.paho.Publish(topic, 0, true, []byte{})
		if !token.WaitTimeout(5 * time.Second) {
			return fmt.Errorf("mqttclient: discovery deletion timed out for %s", topic)
		}
		if err := token.Error(); err != nil {
			return fmt.Errorf("mqttclient: discovery deletion failed for %s: %w", topic, err)
		}
	}
	return nil
}

var _ DeviceFilter = (*devicefilter.Store)(nil)

// subscriptionWorker serialises dynamic (un)subscriptions so the discovery
// message handler can return immediately. Paho's OrderMatters=true dispatches
// incoming PUBLISH packets by calling the message handler synchronously from
// the same single goroutine that also delivers every other incoming packet,
// including SUBACK/UNSUBACK. Calling Subscribe()/Unsubscribe() with
// token.Wait() from within that goroutine (e.g. directly inside
// handleDiscovery) therefore self-deadlocks: the goroutine wants to wait for
// an ACK that only it could ever deliver. Routing both operations through
// this queue keeps the router goroutine from ever blocking on its own ACK.
func (c *Client) subscriptionWorker() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case op := <-c.subscribeQueue:
			if op.subscribe {
				c.subscribeState(op.topic)
			} else {
				c.unsubscribeState(op.topic)
			}
		}
	}
}

// queueSubscription enqueues a subscribe/unsubscribe request without ever
// blocking the caller - important because handleDiscovery calls this from
// the Paho router goroutine, where blocking on a full channel would be just
// as fatal as the direct-call deadlock this queue exists to avoid (see
// subscriptionWorker). The queue only fills up under an unrealistic backlog,
// so the fallback goroutine here is a safety net, not the common path.
func (c *Client) queueSubscription(topic string, subscribe bool) {
	op := subscriptionOp{topic: topic, subscribe: subscribe}
	select {
	case c.subscribeQueue <- op:
	default:
		go func() {
			select {
			case c.subscribeQueue <- op:
			case <-c.ctx.Done():
			}
		}()
	}
}

// Connect opens the MQTT connection and blocks until the initial connection
// succeeds (or times out). Discovery subscription and any previously known
// dynamic subscriptions happen in the OnConnect handler, so they are also
// re-established automatically after a later reconnect.
func (c *Client) Connect() error {
	return c.connectWithTimeout(c.connectTimeout())
}

// getConfig and setConfig guard c.cfg with c.mu so Reconfigure can swap the
// active configuration while onConnect, buildOptions and friends keep
// reading a consistent snapshot from any goroutine.
func (c *Client) getConfig() Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg
}

func (c *Client) setConfig(cfg Config) {
	c.mu.Lock()
	c.cfg = cfg
	c.mu.Unlock()
}

func (c *Client) connectTimeout() time.Duration {
	seconds := c.getConfig().ConnectTimeoutSec
	if seconds <= 0 {
		seconds = 10
	}
	return time.Duration(seconds) * time.Second
}

func (c *Client) discoveryPrefix() string {
	prefix := c.getConfig().DiscoveryPrefix
	if prefix == "" {
		return defaultDiscoveryPrefix
	}
	return prefix
}

// discoveryTopicFilter is the 3-level HA discovery wildcard this client
// subscribes to: <prefix>/{component}/{device_id}/{object_id}/config.
func (c *Client) discoveryTopicFilter() string {
	return c.discoveryPrefix() + "/+/+/+/config"
}

func (c *Client) buildOptions() *mqtt.ClientOptions {
	cfg := c.getConfig()
	scheme := "tcp"
	if cfg.TLS {
		scheme = "ssl"
	}
	keepalive := cfg.KeepaliveSeconds
	if keepalive <= 0 {
		keepalive = 30
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("%s://%s:%s", scheme, cfg.Host, cfg.Port))
	opts.SetClientID(cfg.ClientID)
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
		opts.SetPassword(cfg.Password)
	}
	if cfg.TLS {
		opts.SetTLSConfig(tlsConfig(cfg.TLSInsecure))
	}
	opts.SetCleanSession(cfg.CleanSession)
	opts.SetKeepAlive(time.Duration(keepalive) * time.Second)
	opts.SetAutoReconnect(true)
	opts.SetOnConnectHandler(c.onConnect)
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		c.mu.Lock()
		c.status.Connected = false
		c.status.LastDisconnectedAt = time.Now().UTC()
		c.mu.Unlock()
		c.logger.Printf("connection lost: %v", err)
	})
	opts.SetReconnectingHandler(func(_ mqtt.Client, _ *mqtt.ClientOptions) {
		c.logger.Printf("reconnecting...")
	})
	if cfg.AvailabilityTopic != "" {
		opts.SetBinaryWill(cfg.AvailabilityTopic, []byte("0"), 0, true)
	}
	return opts
}

// connectWithTimeout opens a fresh Paho client with the current c.cfg and
// blocks until the connection succeeds or timeout elapses. It only starts
// logSummaryPeriodically once for the lifetime of the Client (via
// c.logOnce), so a later Reconfigure reconnect does not accumulate one more
// ticking goroutine per call - see the package doc comment on Reconfigure.
func (c *Client) connectWithTimeout(timeout time.Duration) error {
	opts := c.buildOptions()
	paho := mqtt.NewClient(opts)
	token := paho.Connect()
	if !token.WaitTimeout(timeout) {
		return errors.New("mqttclient: connect timed out")
	}
	if err := token.Error(); err != nil {
		return err
	}

	c.mu.Lock()
	c.paho = paho
	c.mu.Unlock()

	c.logOnce.Do(func() { go c.logSummaryPeriodically() })
	return nil
}

// disconnectLocked disconnects the current Paho client without cancelling
// c.ctx, so the subscriptionWorker goroutine keeps running - unlike Close(),
// this is meant to be followed by another connectWithTimeout call as part of
// Reconfigure. The name matches Reload()'s locking convention: callers must
// already hold c.reloadMu.
func (c *Client) disconnectLocked() {
	c.mu.Lock()
	paho := c.paho
	c.mu.Unlock()
	if paho != nil && paho.IsConnected() {
		paho.Disconnect(250)
	}
	c.mu.Lock()
	c.status.Connected = false
	c.status.LastDisconnectedAt = time.Now().UTC()
	c.mu.Unlock()
}

// Reconfigure disconnects the current broker connection and reconnects with
// a new Config, replaying the discovery cache exactly like Reload(). If the
// new configuration fails to connect, it automatically falls back to the
// previous, known-working configuration so the dashboard never ends up
// permanently disconnected because of a typo in a saved setting. The
// returned error is non-nil whenever the new configuration failed, even if
// the fallback succeeded; Status().RolledBack distinguishes the two cases.
func (c *Client) Reconfigure(cfg Config) error {
	c.reloadMu.Lock()
	defer c.reloadMu.Unlock()

	previous := c.getConfig()

	c.disconnectLocked()
	c.setConfig(cfg)
	c.reg.Reset()
	c.mu.Lock()
	c.subscribed = make(map[string]bool)
	c.mu.Unlock()

	if err := c.connectWithTimeout(c.connectTimeout()); err == nil {
		c.mu.Lock()
		c.status.LastReconfiguredAt = time.Now().UTC()
		c.status.LastError = ""
		c.status.RolledBack = false
		c.mu.Unlock()
		return nil
	} else {
		primaryErr := err

		c.disconnectLocked()
		c.setConfig(previous)
		c.reg.Reset()
		c.mu.Lock()
		c.subscribed = make(map[string]bool)
		c.mu.Unlock()

		rollbackErr := c.connectWithTimeout(c.connectTimeout())
		c.mu.Lock()
		c.status.FailedAttempts++
		if rollbackErr != nil {
			c.status.LastError = fmt.Sprintf("%v; rollback failed: %v", primaryErr, rollbackErr)
			c.status.RolledBack = false
			c.mu.Unlock()
			return fmt.Errorf("mqttclient: reconfigure failed: %w; rollback also failed: %v", primaryErr, rollbackErr)
		}
		c.status.LastError = primaryErr.Error()
		c.status.RolledBack = true
		c.mu.Unlock()
		return fmt.Errorf("mqttclient: reconfigure failed, rolled back to previous configuration: %w", primaryErr)
	}
}

// logSummaryPeriodically is a Phase-1 debugging aid: it logs how many
// devices/entities the registry actually holds and how many dynamic
// state/availability topics are subscribed, so a mismatch against the
// number of discovery messages actually seen (see handleDiscovery's
// per-message logging) is visible without instrumenting anything else.
// Safe to remove once discovery is confirmed working end to end against
// all bridges.
func (c *Client) logSummaryPeriodically() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.mu.Lock()
			subscribedCount := len(c.subscribed)
			c.mu.Unlock()

			devices := c.reg.Snapshot()
			entityCount := 0
			for _, d := range devices {
				entityCount += len(d.Entities)
			}
			c.logger.Printf("summary: %d device(s), %d entit(y/ies), %d dynamic topic subscription(s)", len(devices), entityCount, subscribedCount)
		}
	}
}

// Close disconnects from the broker.
func (c *Client) Close() {
	c.cancel()
	if c.paho != nil && c.paho.IsConnected() {
		c.paho.Disconnect(250)
	}
	c.mu.Lock()
	c.status.Connected = false
	c.status.LastDisconnectedAt = time.Now().UTC()
	c.mu.Unlock()
}

// Reload clears the in-memory discovery registry and subscribes again to the
// retained discovery messages.
func (c *Client) Reload() error {
	c.reloadMu.Lock()
	defer c.reloadMu.Unlock()

	if c.paho == nil || !c.paho.IsConnected() {
		return errors.New("mqttclient: cannot reload while disconnected")
	}
	c.reg.Reset()
	c.mu.Lock()
	c.subscribed = make(map[string]bool)
	c.mu.Unlock()
	c.onConnect(c.paho)
	c.mu.Lock()
	c.status.LastReloadAt = time.Now().UTC()
	c.mu.Unlock()
	return nil
}

// ReloadService asks a bridge to reload its JSON configuration through the
// common Slave command topic.
func (c *Client) ReloadService(serviceID string) error {
	if serviceID == "" {
		return errors.New("mqttclient: service ID is empty")
	}
	if c.paho == nil || !c.paho.IsConnected() {
		return errors.New("mqttclient: cannot request reload while disconnected")
	}
	topic := fmt.Sprintf("outstation/%s/config/reload", serviceID)
	token := c.paho.Publish(topic, 0, false, "reload")
	if !token.WaitTimeout(5 * time.Second) {
		return errors.New("mqttclient: reload request timed out")
	}
	return token.Error()
}

// Publish sends a non-retained message to an arbitrary topic. Used both for
// discovered-entity commands and for the dashboard's own energy-balance
// broadcast on outstation/energy_node/energy/balance (see cmd/dashboard/main.go).
func (c *Client) Publish(topic, payload string) error {
	if topic == "" {
		return errors.New("mqttclient: command topic is empty")
	}
	if c.paho == nil || !c.paho.IsConnected() {
		return errors.New("mqttclient: cannot publish while disconnected")
	}
	token := c.paho.Publish(topic, 0, false, payload)
	if !token.WaitTimeout(5 * time.Second) {
		return errors.New("mqttclient: command publish timed out")
	}
	return token.Error()
}

// PublishRetained verhält sich wie Publish, setzt aber das Retain-Flag.
// Genutzt für die eigene HA-Discovery des Dashboards und für den
// Energie-Balance-Broadcast (siehe cmd/dashboard/main.go).
func (c *Client) PublishRetained(topic, payload string) error {
	if topic == "" {
		return errors.New("mqttclient: publish topic is empty")
	}
	if c.paho == nil || !c.paho.IsConnected() {
		return errors.New("mqttclient: cannot publish while disconnected")
	}
	token := c.paho.Publish(topic, 0, true, payload)
	if !token.WaitTimeout(5 * time.Second) {
		return errors.New("mqttclient: retained publish timed out")
	}
	return token.Error()
}

// onConnect (re-)subscribes to the discovery wildcard and to every
// state/availability topic already known from before this (re)connect.
func (c *Client) onConnect(client mqtt.Client) {
	cfg := c.getConfig()
	c.mu.Lock()
	c.status.Connected = true
	c.status.LastConnectedAt = time.Now().UTC()
	c.status.BrokerAddress = cfg.Host + ":" + cfg.Port
	c.status.Source = cfg.Source
	c.mu.Unlock()
	filter := c.discoveryTopicFilter()
	c.logger.Printf("connected, subscribing to %s", filter)
	epochID := c.beginDiscoveryEpoch()
	if token := client.Subscribe(filter, 0, c.handleDiscovery); token.Wait() && token.Error() != nil {
		c.logger.Printf("discovery subscribe failed: %v", token.Error())
	}
	if epochID != 0 {
		time.AfterFunc(discoveryEpochGrace, func() {
			c.completeDiscoveryEpoch(epochID)
		})
	}

	c.mu.Lock()
	topics := make([]string, 0, len(c.subscribed))
	for t := range c.subscribed {
		topics = append(topics, t)
	}
	c.mu.Unlock()

	for _, t := range topics {
		if token := client.Subscribe(t, 0, c.handleState); token.Wait() && token.Error() != nil {
			c.logger.Printf("re-subscribe %s failed: %v", t, token.Error())
		}
	}

	c.mu.Lock()
	bridgeTopic := c.bridgeWatchTopic
	c.mu.Unlock()
	if bridgeTopic != "" {
		if token := client.Subscribe(bridgeTopic, 0, c.handleBridgeState); token.Wait() && token.Error() != nil {
			c.logger.Printf("bridge watch re-subscribe %s failed: %v", bridgeTopic, token.Error())
		}
	}

	c.subscribeWatchTopics(client)

	if cfg.AvailabilityTopic != "" {
		if token := client.Publish(cfg.AvailabilityTopic, 0, true, "1"); token.Wait() && token.Error() != nil {
			c.logger.Printf("availability birth publish failed: %v", token.Error())
		}
	}

	c.mu.Lock()
	publisher := c.connectPublisher
	c.mu.Unlock()
	if publisher != nil {
		for _, msg := range publisher() {
			if token := client.Publish(msg.Topic, 0, msg.Retain, msg.Payload); token.Wait() && token.Error() != nil {
				c.logger.Printf("connect publish to %s failed: %v", msg.Topic, token.Error())
			}
		}
	}
}

func (c *Client) beginDiscoveryEpoch() uint64 {
	filter := c.deviceFilter()
	epochStore, ok := filter.(presenceEpoch)
	if !ok {
		return 0
	}
	if err := epochStore.BeginPresenceEpoch(); err != nil {
		c.logger.Printf("discovery presence epoch start failed: %v", err)
		return 0
	}
	c.epochMu.Lock()
	c.discoveryEpoch++
	epochID := c.discoveryEpoch
	c.epochMu.Unlock()
	return epochID
}

func (c *Client) completeDiscoveryEpoch(epochID uint64) {
	c.epochMu.Lock()
	current := c.discoveryEpoch == epochID
	c.epochMu.Unlock()
	if !current {
		return
	}
	filter := c.deviceFilter()
	epochStore, ok := filter.(presenceEpoch)
	if !ok {
		return
	}
	if err := epochStore.CompletePresenceEpoch(); err != nil {
		c.logger.Printf("discovery presence epoch completion failed: %v", err)
	}
}

func (c *Client) handleDiscovery(_ mqtt.Client, msg mqtt.Message) {
	verbose := c.verboseLogging()
	if verbose {
		c.logger.Printf("discovery message on %s (retained=%v, %d bytes)", msg.Topic(), msg.Retained(), len(msg.Payload()))
	}

	prefix := c.discoveryPrefix()
	disc, isRemoval, err := parseDiscoveryMessage(prefix, msg.Topic(), msg.Payload())
	if err != nil {
		preview := string(msg.Payload())
		if len(preview) > 300 {
			preview = preview[:300] + "..."
		}
		c.reg.RecordDiscoveryError(msg.Topic(), string(msg.Payload()), err.Error(), msg.Retained(), msg.Qos(), "mqtt", time.Now().UTC())
		c.logger.Printf("discovery parse error on %s: %v; payload=%s", msg.Topic(), err, preview)
		return
	}

	if isRemoval {
		orphans, removed := c.reg.RemoveDiscovery(msg.Topic())
		if filter := c.deviceFilter(); filter != nil {
			if err := filter.MarkMissing(msg.Topic()); err != nil {
				c.logger.Printf("ignored discovery removal state for %s failed: %v", msg.Topic(), err)
			}
		}
		uniqueID, topicOK := removalUniqueID(prefix, msg.Topic())
		if !removed && topicOK {
			orphans = c.reg.RemoveEntity(uniqueID)
			removed = len(orphans) > 0
		}
		if !removed {
			c.logger.Printf("discovery removal on %s ignored: no previously registered entity", msg.Topic())
			return
		}
		c.logger.Printf("discovery removal on %s: %d orphan topic(s) unsubscribed", msg.Topic(), len(orphans))
		for _, t := range orphans {
			c.queueSubscription(t, false)
		}
		return
	}
	disc.DiscoveryTopic = msg.Topic()
	disc.DiscoveryRetained = msg.Retained()
	disc.DiscoveryQoS = msg.Qos()
	disc.DiscoverySource = "mqtt"
	if filter := c.deviceFilter(); filter != nil && filter.IsIgnored(disc.Device.ID) {
		if err := filter.MarkPresent(disc); err != nil {
			c.logger.Printf("ignored discovery state for %s failed: %v", msg.Topic(), err)
		}
		c.logger.Printf("discovery ignored: device_id=%s topic=%s", disc.Device.ID, msg.Topic())
		return
	}

	if verbose {
		c.logger.Printf(
			"discovery registered: device_id=%s unique_id=%s component=%s object_id=%s state_topic=%s availability_topic=%s",
			disc.Device.ID, disc.Entity.UniqueID, disc.Entity.Component, disc.Entity.ObjectID,
			disc.Entity.StateTopic, disc.Entity.AvailabilityTopic,
		)
	}

	newTopics, orphanTopics := c.reg.UpsertEntity(disc)
	c.mu.Lock()
	discoveryObserver := c.discoveryObserver
	c.mu.Unlock()
	if discoveryObserver != nil {
		discoveryObserver(disc)
	}
	for _, t := range orphanTopics {
		c.queueSubscription(t, false)
	}
	for _, t := range newTopics {
		c.queueSubscription(t, true)
	}
}

// subscribeState subscribes to a state_topic or availability_topic
// discovered dynamically, skipping topics we're already subscribed to.
func (c *Client) subscribeState(topic string) {
	c.mu.Lock()
	already := c.subscribed[topic]
	c.mu.Unlock()
	if already {
		return
	}

	token := c.paho.Subscribe(topic, 0, c.handleState)
	if !token.WaitTimeout(5 * time.Second) {
		c.logger.Printf("subscribe %s timed out", topic)
		return
	}
	if err := token.Error(); err != nil {
		c.logger.Printf("subscribe %s failed: %v", topic, err)
		return
	}
	c.mu.Lock()
	c.subscribed[topic] = true
	c.mu.Unlock()
}

func (c *Client) unsubscribeState(topic string) {
	c.mu.Lock()
	delete(c.subscribed, topic)
	c.mu.Unlock()
	if c.paho == nil || !c.paho.IsConnected() {
		return
	}

	token := c.paho.Unsubscribe(topic)
	if !token.WaitTimeout(5 * time.Second) {
		c.logger.Printf("unsubscribe %s timed out", topic)
		return
	}
	if err := token.Error(); err != nil {
		c.logger.Printf("unsubscribe %s failed: %v", topic, err)
	}
}

// handleState feeds an incoming message into both UpdateState and
// UpdateAvailability; the registry only applies it to entities whose
// state_topic or availability_topic actually equals this topic, so one
// handler per dynamic topic is sufficient even though, in principle, the
// same topic string could serve as a state topic for one entity and an
// availability topic for another.
func (c *Client) handleState(_ mqtt.Client, msg mqtt.Message) {
	now := time.Now()
	changes := c.reg.UpdateTopicWithQoS(msg.Topic(), msg.Payload(), msg.Retained(), msg.Qos(), now)
	if c.verboseLogging() {
		c.logger.Printf("state message on %s (retained=%v, %d bytes, changed=%v, registry_version=%d)", msg.Topic(), msg.Retained(), len(msg.Payload()), len(changes) > 0, c.reg.Version())
	}
	if len(changes) == 0 {
		return
	}
	c.mu.Lock()
	observer := c.stateObserver
	c.mu.Unlock()
	if observer != nil {
		observer(changes)
	}
}
