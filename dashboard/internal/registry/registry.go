// Package registry holds the in-memory DeviceRegistry: devices and their
// discovery-derived entities, plus the live state/availability that the
// mqttclient package feeds in as MQTT messages arrive. All access goes
// through a single sync.RWMutex; Snapshot/Get return plain value copies so
// callers (HTTP API, web UI) never touch the lock.
package registry

import (
	"encoding/json"
	"fmt"
	"hash"
	"hash/fnv"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DeviceInfo is the discovery "device" block for one entity, as parsed from
// a Home Assistant MQTT Discovery payload.
type DeviceInfo struct {
	ID           string // device.identifiers[0], falls back to the discovery topic's device_id segment
	Name         string
	Manufacturer string
	Model        string
	Firmware     string
	ViaDevice    string
}

const maxLastMessagePayload = 4096

// MQTTMessage is the bounded last-message snapshot exposed for device detail
// views. It deliberately stores no unbounded runtime history.
type MQTTMessage struct {
	Topic    string    `json:"topic"`
	Payload  string    `json:"payload"`
	At       time.Time `json:"at"`
	Retained bool      `json:"retained"`
	QoS      byte      `json:"qos"`
}

type DeviceRelation struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	Kind string `json:"kind"`
}

// AvailabilityInfo describes one availability topic from MQTT Discovery.
type AvailabilityInfo struct {
	Topic               string `json:"topic"`
	PayloadAvailable    string `json:"payload_available,omitempty"`
	PayloadNotAvailable string `json:"payload_not_available,omitempty"`
}

// EntityInfo is the entity-level discovery metadata for one unique_id.
type EntityInfo struct {
	UniqueID            string
	Component           string // sensor, binary_sensor, switch, number, ...
	ObjectID            string
	Name                string
	Icon                string
	StateTopic          string
	AvailabilityTopic   string
	CommandTopic        string
	PayloadOn           string
	PayloadOff          string
	PayloadAvailable    string
	PayloadNotAvailable string
	Availability        []AvailabilityInfo
	AvailabilityMode    string
	ValueTemplate       string
	UnitOfMeasurement   string
	DeviceClass         string
	MinValue            *float64
	MaxValue            *float64
	Step                *float64
	DefaultHidden       bool
	EntityCategory      string
}

// Discovery bundles a device and one of its entities, as produced by the
// mqttclient discovery parser for a single discovery message.
type Discovery struct {
	Device            DeviceInfo
	Entity            EntityInfo
	RawJSON           string
	DiscoveryTopic    string
	DiscoveryRetained bool
	DiscoveryQoS      byte
	DiscoverySource   string
}

// DiscoveryError is the latest parse failure observed for a discovery topic.
type DiscoveryError struct {
	Topic    string
	Payload  string
	Error    string
	Retained bool
	QoS      byte
	Source   string
	At       time.Time
}

type entityState struct {
	info              EntityInfo
	valueTemplate     *parsedValueTemplate
	discoveryJSON     string
	discoveryTopic    string
	discoveryRetained bool
	discoveryQoS      byte
	discoverySource   string

	value           string
	hasValue        bool
	available       bool
	hasAvailability bool
	lastSeen        time.Time
	lastTopicAt     time.Time
	lastRetained    bool
	lastMessage     *MQTTMessage
	// lastStateMessage is the last message seen on this entity's state topic.
	// lastMessage tracks whichever topic (state or availability) carried the
	// most recent message, so it is not a reliable source for the state
	// payload once an availability heartbeat lands - see TopicSample.
	lastStateMessage *MQTTMessage
	source           string
	payloadValid     bool
	payloadError     string
	hasPayloadState  bool
	availability     map[string]bool
	availabilityAt   map[string]time.Time

	// pending tracks a command that was published but not yet confirmed by a
	// live MQTT state message (see BeginPendingCommand). value/hasValue are
	// deliberately left untouched while a command is pending, so the last
	// confirmed state keeps being reported until either a real state message
	// resolves it (success) or ExpirePendingCommands times it out.
	pending           bool
	pendingValue      string
	pendingSince      time.Time
	pendingDeadline   time.Time
	lastCommandResult string // "" | "success" | "timeout"
	lastCommandAt     time.Time
}

type entityRef struct {
	deviceID string
	uniqueID string
}

type device struct {
	info     DeviceInfo
	entities map[string]*entityState // keyed by unique_id
}

// Registry is the in-memory device/entity store (see Implementierungsplan,
// Abschnitt Registry: "ein sync.RWMutex, Snapshot-Funktion fuer Reads").
type Registry struct {
	mu      sync.RWMutex
	version atomic.Uint64

	devices           map[string]*device         // keyed by DeviceInfo.ID
	byUnique          map[string]string          // unique_id -> device ID
	uniqueDevices     map[string]map[string]bool // unique_id -> device IDs
	byTopic           map[string]map[string]bool // topic -> set of unique_ids using it (state or availability)
	byDiscovery       map[string]entityRef       // discovery topic -> previously registered entity
	discoveryErrors   map[string]DiscoveryError
	relationOverrides []RelationOverride // manually added relations, layered on top of via_device (see SetRelationOverrides)
}

// RelationOverride is a manually created device relation (e.g. "connect as
// sub-device") that augments the via_device-derived tree without replacing
// it. The Device-Map feature persists these via internal/settings;
// SetRelationOverrides lets the HTTP layer inject the current set without
// this package depending on internal/settings (mirrors
// energy.Resolver.SetOverrides).
type RelationOverride struct {
	ID       string
	ChildID  string
	ParentID string
	Kind     string
}

// New creates an empty Registry.
func New() *Registry {
	return &Registry{
		devices:         make(map[string]*device),
		byUnique:        make(map[string]string),
		uniqueDevices:   make(map[string]map[string]bool),
		byTopic:         make(map[string]map[string]bool),
		byDiscovery:     make(map[string]entityRef),
		discoveryErrors: make(map[string]DiscoveryError),
	}
}

// SetRelationOverrides replaces the current set of manually added device
// relations. Callers pass the full set each time (not a diff), same as
// energy.Resolver.SetOverrides.
func (r *Registry) SetRelationOverrides(overrides []RelationOverride) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.relationOverrides = append([]RelationOverride(nil), overrides...)
}

// Reset removes all discovery-derived runtime state. The MQTT client can
// repopulate the registry by subscribing to retained discovery topics.
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.devices = make(map[string]*device)
	r.byUnique = make(map[string]string)
	r.uniqueDevices = make(map[string]map[string]bool)
	r.byTopic = make(map[string]map[string]bool)
	r.byDiscovery = make(map[string]entityRef)
	r.discoveryErrors = make(map[string]DiscoveryError)
	r.version.Add(1)
}

// Version changes only when discovery, live value, availability, or
// diagnostics state changes. Consumers can use it to coalesce refreshes.
func (r *Registry) Version() uint64 { return r.version.Load() }

// RecordDiscoveryError keeps the latest invalid payload per discovery topic
// so diagnostics can report broker input failures without growing forever.
func (r *Registry) RecordDiscoveryError(topic, payload, parseError string, retained bool, qos byte, source string, at time.Time) {
	if len(payload) > 4096 {
		payload = payload[:4096] + "..."
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.discoveryErrors[topic] = DiscoveryError{Topic: topic, Payload: payload, Error: parseError, Retained: retained, QoS: qos, Source: source, At: at}
	r.version.Add(1)
}

// DiscoveryErrors returns the latest parse failures in deterministic order.
func (r *Registry) DiscoveryErrors() []DiscoveryError {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]DiscoveryError, 0, len(r.discoveryErrors))
	for _, issue := range r.discoveryErrors {
		result = append(result, issue)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Topic < result[j].Topic })
	return result
}

// UpsertEntity registers or replaces an entity's discovery metadata. It
// returns the state_topic/availability_topic values that are newly needed,
// i.e. not already tracked for any other entity - the mqttclient uses this
// to decide which topics it still has to subscribe to.
func (r *Registry) UpsertEntity(d Discovery) (newTopics, orphanTopics []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d.Entity = normalizeEntityInfo(d.Entity)

	if d.DiscoveryTopic != "" {
		delete(r.discoveryErrors, d.DiscoveryTopic)
		if previous, ok := r.byDiscovery[d.DiscoveryTopic]; ok && (previous.deviceID != d.Device.ID || previous.uniqueID != d.Entity.UniqueID) {
			orphanTopics = append(orphanTopics, r.removeEntityLocked(previous.deviceID, previous.uniqueID)...)
		}
	}

	dev, ok := r.devices[d.Device.ID]
	if !ok {
		dev = &device{info: d.Device, entities: make(map[string]*entityState)}
		r.devices[d.Device.ID] = dev
	} else {
		dev.info = d.Device
	}

	es, existed := dev.entities[d.Entity.UniqueID]
	if !existed {
		es = &entityState{}
		dev.entities[d.Entity.UniqueID] = es
	} else {
		orphanTopics = append(orphanTopics, r.removeTopicMappingsLocked(es.info, d.Entity.UniqueID)...)
		if es.discoveryTopic != "" && es.discoveryTopic != d.DiscoveryTopic {
			delete(r.byDiscovery, es.discoveryTopic)
		}
	}
	es.info = d.Entity
	es.valueTemplate = compileValueTemplate(d.Entity.ValueTemplate)
	es.discoveryJSON = d.RawJSON
	es.discoveryTopic = d.DiscoveryTopic
	es.discoveryRetained = d.DiscoveryRetained
	es.discoveryQoS = d.DiscoveryQoS
	es.discoverySource = d.DiscoverySource
	if d.DiscoveryTopic != "" {
		r.byDiscovery[d.DiscoveryTopic] = entityRef{deviceID: d.Device.ID, uniqueID: d.Entity.UniqueID}
	}
	devices := r.uniqueDevices[d.Entity.UniqueID]
	if devices == nil {
		devices = make(map[string]bool)
		r.uniqueDevices[d.Entity.UniqueID] = devices
	}
	devices[d.Device.ID] = true
	r.byUnique[d.Entity.UniqueID] = primaryDevice(devices)

	for _, topic := range entityTopics(d.Entity) {
		if topic == "" {
			continue
		}
		set, exists := r.byTopic[topic]
		isNew := !exists || len(set) == 0
		if !exists {
			set = make(map[string]bool)
			r.byTopic[topic] = set
		}
		set[d.Entity.UniqueID] = true
		if isNew {
			newTopics = append(newTopics, topic)
		}
	}
	newTopicSet := make(map[string]bool, len(newTopics))
	for _, topic := range newTopics {
		newTopicSet[topic] = true
	}
	filteredOrphans := make([]string, 0, len(orphanTopics))
	for _, topic := range orphanTopics {
		if !newTopicSet[topic] {
			filteredOrphans = append(filteredOrphans, topic)
		}
	}
	r.version.Add(1)
	return newTopics, uniqueStrings(filteredOrphans)
}

// RemoveEntity removes an entity (HA discovery removal convention: an empty
// retained payload on its discovery topic). It returns topics that no
// entity references anymore, so the caller can unsubscribe them.
func (r *Registry) RemoveEntity(uniqueID string) (orphanTopics []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	deviceID, ok := r.byUnique[uniqueID]
	if !ok {
		return nil
	}
	orphanTopics = r.removeEntityLocked(deviceID, uniqueID)
	r.version.Add(1)
	return orphanTopics
}

// RemoveDiscovery removes the entity previously registered on a discovery
// topic. Unlike deriving a unique_id from the topic, this also works when the
// discovery payload supplied an explicit unique_id.
func (r *Registry) RemoveDiscovery(topic string) (orphanTopics []string, removed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ref, ok := r.byDiscovery[topic]
	if !ok {
		return nil, false
	}
	delete(r.byDiscovery, topic)
	orphanTopics = r.removeEntityLocked(ref.deviceID, ref.uniqueID)
	r.version.Add(1)
	return orphanTopics, true
}

// RemoveDevice removes every discovery entity belonging to a device while
// keeping the operation atomic from registry readers' point of view.
func (r *Registry) RemoveDevice(deviceID string) (orphanTopics []string, removed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	dev, ok := r.devices[deviceID]
	if !ok {
		return nil, false
	}
	for uniqueID := range dev.entities {
		orphanTopics = append(orphanTopics, r.removeEntityLocked(deviceID, uniqueID)...)
	}
	delete(r.devices, deviceID)
	r.version.Add(1)
	return uniqueStrings(orphanTopics), true
}

func (r *Registry) removeEntityLocked(deviceID, uniqueID string) (orphanTopics []string) {
	dev, ok := r.devices[deviceID]
	if !ok {
		return nil
	}
	es, ok := dev.entities[uniqueID]
	if !ok {
		return nil
	}

	orphanTopics = append(orphanTopics, r.removeTopicMappingsLocked(es.info, uniqueID)...)

	delete(dev.entities, uniqueID)
	if devices := r.uniqueDevices[uniqueID]; devices != nil {
		delete(devices, deviceID)
		if len(devices) == 0 {
			delete(r.uniqueDevices, uniqueID)
			delete(r.byUnique, uniqueID)
		} else {
			r.byUnique[uniqueID] = primaryDevice(devices)
		}
	}
	if len(dev.entities) == 0 {
		delete(r.devices, deviceID)
	}
	return orphanTopics
}

func (r *Registry) removeTopicMappingsLocked(info EntityInfo, uniqueID string) (orphanTopics []string) {
	for _, topic := range entityTopics(info) {
		if topic == "" {
			continue
		}
		if set, ok := r.byTopic[topic]; ok {
			delete(set, uniqueID)
			if len(set) == 0 {
				delete(r.byTopic, topic)
				orphanTopics = append(orphanTopics, topic)
			}
		}
	}
	return orphanTopics
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizeEntityInfo(info EntityInfo) EntityInfo {
	if len(info.Availability) == 0 && info.AvailabilityTopic != "" {
		info.Availability = []AvailabilityInfo{{
			Topic:               info.AvailabilityTopic,
			PayloadAvailable:    info.PayloadAvailable,
			PayloadNotAvailable: info.PayloadNotAvailable,
		}}
	}
	if len(info.Availability) > 0 {
		if info.AvailabilityTopic == "" {
			info.AvailabilityTopic = info.Availability[0].Topic
		}
		if info.PayloadAvailable == "" {
			info.PayloadAvailable = info.Availability[0].PayloadAvailable
		}
		if info.PayloadNotAvailable == "" {
			info.PayloadNotAvailable = info.Availability[0].PayloadNotAvailable
		}
	}
	return info
}

func entityTopics(info EntityInfo) []string {
	result := make([]string, 0, 1+len(info.Availability))
	seen := make(map[string]bool)
	if info.StateTopic != "" {
		result = append(result, info.StateTopic)
		seen[info.StateTopic] = true
	}
	for _, availability := range info.Availability {
		if availability.Topic != "" && !seen[availability.Topic] {
			result = append(result, availability.Topic)
			seen[availability.Topic] = true
		}
	}
	return result
}

func availabilityForTopic(info EntityInfo, topic string) (AvailabilityInfo, bool) {
	for _, availability := range info.Availability {
		if availability.Topic == topic {
			return availability, true
		}
	}
	return AvailabilityInfo{}, false
}

func aggregateAvailability(info EntityInfo, states map[string]bool, seen map[string]time.Time, latestTopic string) bool {
	if info.AvailabilityMode == "latest" {
		latestAt := time.Time{}
		latest := latestTopic
		for topic, at := range seen {
			if at.After(latestAt) {
				latestAt = at
				latest = topic
			}
		}
		return states[latest]
	}
	if info.AvailabilityMode == "any" {
		for _, availability := range info.Availability {
			if states[availability.Topic] {
				return true
			}
		}
		return false
	}
	for _, availability := range info.Availability {
		if !states[availability.Topic] {
			return false
		}
	}
	return len(info.Availability) > 0
}

// StateChange describes one entity whose persisted value or availability
// actually changed as a result of a single incoming MQTT message. Update
// methods that can touch several entities per call (many entities can share
// a state_topic or availability_topic) return the precise set that changed,
// so callers like the runtime cache observer don't need a full Snapshot()
// just to find the one or two entities a single message affected.
type StateChange struct {
	DeviceID        string
	UniqueID        string
	StateTopic      string
	Value           string
	HasValue        bool
	LastSeen        time.Time
	LastTopicAt     time.Time
	Available       bool
	HasAvailability bool
	Payload         string
}

func (r *Registry) stateChangeLocked(uniqueID string, es *entityState) StateChange {
	payload := ""
	if es.lastMessage != nil {
		payload = es.lastMessage.Payload
	}
	return StateChange{
		DeviceID:        r.byUnique[uniqueID],
		UniqueID:        uniqueID,
		StateTopic:      es.info.StateTopic,
		Value:           es.value,
		HasValue:        es.hasValue,
		LastSeen:        es.lastSeen,
		LastTopicAt:     es.lastTopicAt,
		Available:       es.available,
		HasAvailability: es.hasAvailability,
		Payload:         payload,
	}
}

// UpdateState applies an incoming MQTT message on a state_topic to every
// entity whose state_topic matches it exactly.
func (r *Registry) UpdateState(topic string, payload []byte, retained bool, seenAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if changes := r.updateStateLocked(topic, payload, retained, 0, seenAt); len(changes) > 0 {
		r.version.Add(1)
	}
}

// BeginPendingCommand records that a command was published for uniqueID and
// is awaiting confirmation. It deliberately does not touch the entity's
// value/source (see P1.3: a pending write must not be shown as a successful
// one) - resolution happens either via updateStateLocked, when a live state
// message arrives while the command is pending, or via ExpirePendingCommands
// once the deadline passes. ok is false when the entity is unknown; conflict
// is true when another command is already pending and hasn't timed out yet,
// in which case the caller should reject the new command rather than
// override the running one.
func (r *Registry) BeginPendingCommand(uniqueID, value string, now time.Time, timeout time.Duration) (ok bool, conflict bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	es := r.entityByUniqueLocked(uniqueID)
	if es == nil {
		return false, false
	}
	if es.pending && now.Before(es.pendingDeadline) {
		return false, true
	}
	es.pending = true
	es.pendingValue = value
	es.pendingSince = now
	es.pendingDeadline = now.Add(timeout)
	r.version.Add(1)
	return true, false
}

// CancelPendingCommand clears a pending reservation made by
// BeginPendingCommand when the publish that was supposed to follow it never
// happened (e.g. the MQTT publish itself failed), so the entity doesn't sit
// blocked as "pending" for a command that was never actually sent.
func (r *Registry) CancelPendingCommand(uniqueID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	es := r.entityByUniqueLocked(uniqueID)
	if es == nil || !es.pending {
		return
	}
	es.pending = false
	r.version.Add(1)
}

// ExpirePendingCommands resolves every command still pending past its
// deadline as a timeout, so callers holding a Snapshot/EntityView see it
// even without a following MQTT message. It returns the entities that
// changed, the same way UpdateTopic does, so a caller can drive the SSE
// version bump consistently.
func (r *Registry) ExpirePendingCommands(now time.Time) []StateChange {
	r.mu.Lock()
	defer r.mu.Unlock()
	var changes []StateChange
	for uniqueID, deviceID := range r.byUnique {
		dev, ok := r.devices[deviceID]
		if !ok {
			continue
		}
		es, ok := dev.entities[uniqueID]
		if !ok || !es.pending || now.Before(es.pendingDeadline) {
			continue
		}
		es.pending = false
		es.lastCommandResult = "timeout"
		es.lastCommandAt = now
		changes = append(changes, r.stateChangeLocked(uniqueID, es))
	}
	if len(changes) > 0 {
		r.version.Add(1)
	}
	return changes
}

// UpdateTopic applies state and availability changes from one MQTT message
// while holding the registry lock only once. It returns the entities whose
// persisted value or availability changed; timestamp-only updates are
// omitted.
func (r *Registry) UpdateTopic(topic string, payload []byte, retained bool, seenAt time.Time) []StateChange {
	return r.UpdateTopicWithQoS(topic, payload, retained, 0, seenAt)
}

// UpdateTopicWithQoS applies a state or availability message and preserves
// its transport metadata for the device detail snapshot.
func (r *Registry) UpdateTopicWithQoS(topic string, payload []byte, retained bool, qos byte, seenAt time.Time) []StateChange {
	r.mu.Lock()
	defer r.mu.Unlock()
	changes := r.updateStateLocked(topic, payload, retained, qos, seenAt)
	changes = append(changes, r.updateAvailabilityLocked(topic, payload, retained, qos, seenAt)...)
	if len(changes) > 0 {
		r.version.Add(1)
	}
	return changes
}

func (r *Registry) updateStateLocked(topic string, payload []byte, retained bool, qos byte, seenAt time.Time) []StateChange {
	var changes []StateChange
	trimmedPayload := strings.TrimSpace(string(payload))
	needsJSON := strings.HasPrefix(trimmedPayload, "{") || strings.HasPrefix(trimmedPayload, "[")
	if !needsJSON {
		for uniqueID := range r.byTopic[topic] {
			es := r.entityByUniqueLocked(uniqueID)
			if es != nil && es.info.StateTopic == topic && es.valueTemplate != nil {
				needsJSON = true
				break
			}
		}
	}
	var parsedPayload any
	var parseErr error
	if needsJSON {
		parsedPayload, parseErr = decodePayload(payload)
	}
	for uniqueID := range r.byTopic[topic] {
		es := r.entityByUniqueLocked(uniqueID)
		if es == nil || es.info.StateTopic != topic {
			continue
		}
		es.lastMessage = updateLastMessage(es.lastMessage, topic, payload, retained, qos, seenAt)
		es.lastStateMessage = updateLastMessage(es.lastStateMessage, topic, payload, retained, qos, seenAt)
		if es.hasAvailability && !es.available {
			continue
		}
		value := extractValueParsed(payload, es.valueTemplate, parsedPayload, parseErr)
		valueChanged := !es.hasValue || es.value != value
		es.value = value
		es.hasValue = true
		es.payloadValid, es.payloadError = validatePayloadParsed(payload, es.valueTemplate, parsedPayload, parseErr)
		es.hasPayloadState = true
		es.lastSeen = seenAt
		es.lastTopicAt = seenAt
		es.lastRetained = retained
		if retained {
			es.source = "mqtt-replay"
		} else {
			es.source = "live"
		}
		// A live state message resolves an outstanding pending command
		// regardless of whether the reported value matches what was
		// commanded - the device's own report is authoritative, and
		// formatting can legitimately differ (e.g. "23" vs "23.0").
		pendingResolved := es.pending && !retained
		if pendingResolved {
			es.pending = false
			es.lastCommandResult = "success"
			es.lastCommandAt = seenAt
		}
		if valueChanged || pendingResolved {
			changes = append(changes, r.stateChangeLocked(uniqueID, es))
		}
	}
	return changes
}

// RestoreState hydrates a discovered entity from the runtime cache. The next
// fresh state message replaces this value and its source automatically.
func (r *Registry) RestoreState(deviceID, uniqueID, value string, available, hasAvailability bool, lastSeen time.Time) bool {
	return r.RestoreStateAt(deviceID, uniqueID, value, available, hasAvailability, lastSeen, lastSeen, "")
}

// RestoreStateAt hydrates a discovered entity with cached state and the last
// timestamp observed on either its state or availability topic.
func (r *Registry) RestoreStateAt(deviceID, uniqueID, value string, available, hasAvailability bool, lastSeen, lastTopicAt time.Time, payload string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	dev, ok := r.devices[deviceID]
	if !ok {
		return false
	}
	es, ok := dev.entities[uniqueID]
	if !ok {
		return false
	}
	es.value = value
	es.hasValue = true
	es.available = available
	es.hasAvailability = hasAvailability
	es.lastSeen = lastSeen
	es.lastTopicAt = lastTopicAt
	es.lastRetained = true
	es.payloadValid = true
	es.payloadError = ""
	es.hasPayloadState = false
	// Nur seeden, wenn noch keine echte MQTT-Nachricht da war - Restore darf
	// eine bereits laufende Live-Verbindung nicht mit einem veralteten
	// Cache-Stand ueberschreiben (siehe AGENTS.md: restaurierte Werte sind
	// non-live, bis eine frische Nachricht sie ersetzt).
	if payload != "" && es.lastMessage == nil {
		es.lastMessage = &MQTTMessage{
			Topic: es.info.StateTopic, Payload: payload, At: lastTopicAt, Retained: true,
		}
		es.lastStateMessage = cloneMQTTMessage(es.lastMessage)
		es.source = "runtime-cache"
	} else if es.source == "" {
		es.source = "runtime-cache"
	}
	return true
}

// UpdateAvailability applies an incoming MQTT message on an
// availability_topic to every entity whose availability_topic matches it.
func (r *Registry) UpdateAvailability(topic string, payload []byte, retained bool, seenAt time.Time) {
	r.UpdateAvailabilityWithQoS(topic, payload, retained, 0, seenAt)
}

// UpdateAvailabilityWithQoS applies an availability message and preserves its
// transport metadata for the device detail snapshot.
func (r *Registry) UpdateAvailabilityWithQoS(topic string, payload []byte, retained bool, qos byte, seenAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if changes := r.updateAvailabilityLocked(topic, payload, retained, qos, seenAt); len(changes) > 0 {
		r.version.Add(1)
	}
}

func (r *Registry) updateAvailabilityLocked(topic string, payload []byte, retained bool, qos byte, seenAt time.Time) []StateChange {
	var changes []StateChange
	for uniqueID := range r.byTopic[topic] {
		es := r.entityByUniqueLocked(uniqueID)
		if es == nil {
			continue
		}
		availability, ok := availabilityForTopic(es.info, topic)
		if !ok {
			continue
		}
		es.lastMessage = updateLastMessage(es.lastMessage, topic, payload, retained, qos, seenAt)
		if es.availability == nil {
			es.availability = make(map[string]bool)
		}
		if es.availabilityAt == nil {
			es.availabilityAt = make(map[string]time.Time)
		}
		p := string(payload)
		availablePayload := availability.PayloadAvailable
		if availablePayload == "" {
			availablePayload = "online"
		}
		es.availability[topic] = p == availablePayload
		es.availabilityAt[topic] = seenAt
		previousHasAvailability := es.hasAvailability
		previousAvailable := es.available
		es.hasAvailability = true
		es.available = aggregateAvailability(es.info, es.availability, es.availabilityAt, topic)
		es.lastTopicAt = seenAt
		if !previousHasAvailability || previousAvailable != es.available {
			changes = append(changes, r.stateChangeLocked(uniqueID, es))
		}
	}
	return changes
}

// entityByUniqueLocked must be called with r.mu held.
func (r *Registry) entityByUniqueLocked(uniqueID string) *entityState {
	deviceID, ok := r.byUnique[uniqueID]
	if !ok {
		return nil
	}
	dev, ok := r.devices[deviceID]
	if !ok {
		return nil
	}
	return dev.entities[uniqueID]
}

// --- read views for the HTTP API and web UI ---

// EntityView is a read-only snapshot of one entity's discovery metadata plus
// its current live state.
type EntityView struct {
	UniqueID            string             `json:"unique_id"`
	Component           string             `json:"component"`
	ObjectID            string             `json:"object_id"`
	Name                string             `json:"name"`
	DiscoveryJSON       string             `json:"discovery_json,omitempty"`
	Icon                string             `json:"icon,omitempty"`
	StateTopic          string             `json:"state_topic,omitempty"`
	AvailabilityTopic   string             `json:"availability_topic,omitempty"`
	Availability        []AvailabilityInfo `json:"availability,omitempty"`
	AvailabilityMode    string             `json:"availability_mode,omitempty"`
	Commandable         bool               `json:"commandable,omitempty"`
	PayloadOn           string             `json:"payload_on,omitempty"`
	PayloadOff          string             `json:"payload_off,omitempty"`
	PayloadAvailable    string             `json:"payload_available,omitempty"`
	PayloadNotAvailable string             `json:"payload_not_available,omitempty"`
	ValueTemplate       string             `json:"value_template,omitempty"`
	// TemplateSupported sagt, ob der Automations-Dienst den Wert dieser
	// Entitaet aus ihrem State-Topic ziehen kann. Die Entscheidung faellt
	// hier und nur hier, damit die Template-Regex nicht ein drittes Mal
	// (Go, Python, Browser) implementiert wird.
	TemplateSupported bool         `json:"template_supported"`
	UnitOfMeasurement string       `json:"unit_of_measurement,omitempty"`
	DeviceClass       string       `json:"device_class,omitempty"`
	MinValue          *float64     `json:"min_value,omitempty"`
	MaxValue          *float64     `json:"max_value,omitempty"`
	Step              *float64     `json:"step,omitempty"`
	DefaultHidden     bool         `json:"default_hidden,omitempty"`
	EntityCategory    string       `json:"entity_category,omitempty"`
	HiddenOnTile      bool         `json:"hidden_on_tile,omitempty"`
	Value             string       `json:"value"`
	HasValue          bool         `json:"has_value"`
	Available         bool         `json:"available"`
	HasAvailability   bool         `json:"has_availability"`
	LastSeen          time.Time    `json:"last_seen"`
	LastTopicAt       time.Time    `json:"last_topic_at,omitempty"`
	AgeSeconds        int64        `json:"age_seconds,omitempty"`
	Source            string       `json:"source"`
	Freshness         string       `json:"freshness"`
	CommandTopic      string       `json:"command_topic,omitempty"`
	DiscoveryTopic    string       `json:"discovery_topic,omitempty"`
	DiscoveryRetained bool         `json:"discovery_retained"`
	DiscoveryQoS      byte         `json:"discovery_qos"`
	DiscoverySource   string       `json:"discovery_source,omitempty"`
	LastMessage       *MQTTMessage `json:"last_message,omitempty"`
	PayloadValid      bool         `json:"payload_valid"`
	PayloadError      string       `json:"payload_error,omitempty"`
	// Stale is true when the most recent update for this entity came from a
	// retained/replayed MQTT message rather than a fresh live publish (see
	// Auswertung des Retain-Flags fuer Replay-Erkennung, Implementierungsplan
	// Phase 1). The UI uses this the same way it would a runtime-cache
	// value: shown, but visibly marked as not-necessarily-current.
	Stale bool `json:"stale"`
	// Pending is true while a command was published for this entity but not
	// yet confirmed by a live MQTT state message or timed out (see
	// BeginPendingCommand). Value/Source above still reflect the last
	// confirmed state, not the pending write - the UI must not display the
	// pending write as if it had already succeeded.
	Pending         bool      `json:"pending"`
	PendingValue    string    `json:"pending_value,omitempty"`
	PendingSince    time.Time `json:"pending_since,omitempty"`
	PendingDeadline time.Time `json:"pending_deadline,omitempty"`
	// LastCommandResult is the outcome of the most recently resolved pending
	// command ("success" or "timeout"), kept until the next command starts.
	LastCommandResult string    `json:"last_command_result,omitempty"`
	LastCommandAt     time.Time `json:"last_command_at,omitempty"`
}

// ValueView ist die kleine, schnell wechselnde Haelfte von EntityView: genau
// die Felder, die sich zwischen zwei Layoutwechseln aendern koennen. Der
// Rest - Name, Einheit, device_class, Icon - haengt an der Discovery und
// steht bereits im gerenderten HTML.
//
// Der Zuschnitt ist der Grund, warum der SSE-Push ueberhaupt tragbar ist:
// /api/v1/devices liefert am Livesystem 448 KB, davon allein 252 KB
// discovery_json und last_message; diese sieben Felder wiegen bei 188
// Entitaeten grob 11 KB. Siehe Spec Abschnitt 2, Weg (c).
type ValueView struct {
	Value             string `json:"value"`
	HasValue          bool   `json:"has_value"`
	Available         bool   `json:"available"`
	HasAvailability   bool   `json:"has_availability"`
	Stale             bool   `json:"stale"`
	Pending           bool   `json:"pending"`
	LastCommandResult string `json:"last_command_result,omitempty"`
}

// ValueViews bildet alle Entitaeten eines Snapshots auf ihre UniqueID ab.
// Eine Entitaet ohne UniqueID ist im Browser nicht adressierbar und wird
// uebersprungen, statt den leeren Schluessel zu belegen.
func ValueViews(devices []DeviceView) map[string]ValueView {
	values := make(map[string]ValueView)
	for _, device := range devices {
		for _, entity := range device.Entities {
			if entity.UniqueID == "" {
				continue
			}
			values[entity.UniqueID] = ValueView{
				Value:             entity.Value,
				HasValue:          entity.HasValue,
				Available:         entity.Available,
				HasAvailability:   entity.HasAvailability,
				Stale:             entity.Stale,
				Pending:           entity.Pending,
				LastCommandResult: entity.LastCommandResult,
			}
		}
	}
	return values
}

// StructureFingerprint verdichtet alles, woraus die Templates *Struktur*
// machen, zu einem kurzen Hex-String: welche Geraete es gibt, welche
// Entitaeten daran haengen, wie sie heissen, welche Komponente und
// device_class sie haben, welche Einheit sie tragen und ob sie schaltbar
// sind. Genau diese Felder entscheiden ueber Zeilen, Icons, Regler und -
// ueber classifyEntityCategory - ueber HiddenOnTile.
//
// Der Browser vergleicht ihn mit dem Wert, mit dem sein Fragment gerendert
// wurde (data-structure). Gleich heisst: es hat sich nur ein Messwert
// bewegt, den zieht device-tile-values.js nach. Ungleich heisst: Discovery
// hat etwas veraendert - dafuer ist der Fragment-Tausch da.
//
// Als Hex-String und nicht als Zahl, weil ein uint64 jenseits 2^53 beim
// JSON-Parsen in JavaScript Stellen verliert; zwei verschiedene Strukturen
// koennten dann gleich aussehen.
//
// Kachel-Einstellungen stehen bewusst nicht drin, obwohl sie ueber
// HiddenOnTile mitentscheiden: sie feuern schon heute
// config-tile-setting-changed und loesen darueber einen Tausch aus.
//
// Die Reihenfolge ist verlaesslich, weil snapshotLocked() Geraete nach ID
// und Entitaeten nach ObjectID sortiert. Ohne das spraenge der Fingerabdruck
// grundlos und der Tausch waere zurueck.
func StructureFingerprint(devices []DeviceView) string {
	sum := fnv.New64a()
	for _, device := range devices {
		writeStructureField(sum, device.ID)
		for _, entity := range device.Entities {
			writeStructureField(sum, entity.UniqueID)
			writeStructureField(sum, entity.Name)
			writeStructureField(sum, entity.Component)
			writeStructureField(sum, entity.DeviceClass)
			writeStructureField(sum, entity.UnitOfMeasurement)
			writeStructureField(sum, strconv.FormatBool(entity.Commandable))
		}
	}
	return strconv.FormatUint(sum.Sum64(), 16)
}

// Das Nullbyte verhindert, dass zwei verschiedene Zerlegungen derselben
// Zeichenkette denselben Hash ergeben ("ab"+"" gegen "a"+"b").
func writeStructureField(sum hash.Hash64, value string) {
	sum.Write([]byte(value))
	sum.Write([]byte{0})
}

// CommandInfo describes the safe operation exposed by a commandable entity.
type CommandInfo struct {
	Component  string
	Topic      string
	PayloadOn  string
	PayloadOff string
	Value      string
	HasValue   bool
	MinValue   *float64
	MaxValue   *float64
	Step       *float64
}

// Command returns the command metadata for a commandable entity.
func (r *Registry) Command(uniqueID string) (CommandInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	es := r.entityByUniqueLocked(uniqueID)
	if es == nil || !isCommandable(es.info) {
		return CommandInfo{}, false
	}
	return CommandInfo{
		Component:  es.info.Component,
		Topic:      es.info.CommandTopic,
		PayloadOn:  es.info.PayloadOn,
		PayloadOff: es.info.PayloadOff,
		Value:      es.value,
		HasValue:   es.hasValue,
		MinValue:   es.info.MinValue,
		MaxValue:   es.info.MaxValue,
		Step:       es.info.Step,
	}, true
}

// EntityValue ist der reduzierte Wertabzug, den die Verlaufs-Aufzeichnung
// braucht: aktueller Wert, Einheit und Staleness, ohne den Geraetebaum zu
// kopieren. Snapshot() wuerde bei jedem Poll jedes Geraet materialisieren -
// auf der Anlage 448 KB, siehe Kommentar an Counts() - fuer am Ende eine
// Handvoll angefragter Entitaeten.
type EntityValue struct {
	UniqueID        string
	Value           string
	HasValue        bool
	Unit            string
	Stale           bool
	Available       bool
	HasAvailability bool
}

// Values liefert die Werte der angefragten Entitaeten in der Reihenfolge der
// Anfrage. Unbekannte IDs werden ausgelassen, nicht als Leersatz gemeldet.
func (r *Registry) Values(uniqueIDs []string) []EntityValue {
	r.mu.RLock()
	defer r.mu.RUnlock()

	values := make([]EntityValue, 0, len(uniqueIDs))
	for _, uniqueID := range uniqueIDs {
		es := r.entityByUniqueLocked(uniqueID)
		if es == nil {
			continue
		}
		values = append(values, EntityValue{
			UniqueID:        uniqueID,
			Value:           es.value,
			HasValue:        es.hasValue,
			Unit:            es.info.UnitOfMeasurement,
			Stale:           es.lastRetained,
			Available:       es.available,
			HasAvailability: es.hasAvailability,
		})
	}
	return values
}

// DeviceView is a read-only snapshot of one device and its entities.
type DeviceView struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Manufacturer  string           `json:"manufacturer,omitempty"`
	Model         string           `json:"model,omitempty"`
	Firmware      string           `json:"firmware,omitempty"`
	ViaDevice     string           `json:"via_device,omitempty"`
	Relations     []DeviceRelation `json:"relations,omitempty"`
	LastUpdated   time.Time        `json:"last_updated,omitempty"`
	DiscoveryJSON []string         `json:"discovery_json,omitempty"`
	Entities      []EntityView     `json:"entities"`
}

type DiagnosticsSnapshot struct {
	Devices            []DeviceView
	DiscoveryErrors    []DiscoveryError
	DuplicateUniqueIDs map[string][]string
}

// Counts returns the number of known devices and entities without
// materialising a single view. The diagnostics summary only needs these two
// numbers; going through Snapshot() would deep-copy every device tree - 448 KB
// and 188 entities on the live system - just to call len() on the result.
// snapshotLocked() filters nothing out, so counting straight from the maps
// stays in step with it.
func (r *Registry) Counts() (devices, entities int) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, dev := range r.devices {
		entities += len(dev.entities)
	}
	return len(r.devices), entities
}

// Snapshot returns a stable copy of all known devices, sorted by device ID.
func (r *Registry) Snapshot() []DeviceView {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.snapshotLocked()
}

func (r *Registry) DiagnosticsSnapshot() DiagnosticsSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := DiagnosticsSnapshot{
		Devices:            r.snapshotLocked(),
		DiscoveryErrors:    make([]DiscoveryError, 0, len(r.discoveryErrors)),
		DuplicateUniqueIDs: make(map[string][]string),
	}
	for _, issue := range r.discoveryErrors {
		result.DiscoveryErrors = append(result.DiscoveryErrors, issue)
	}
	sort.Slice(result.DiscoveryErrors, func(i, j int) bool {
		return result.DiscoveryErrors[i].Topic < result.DiscoveryErrors[j].Topic
	})
	for uniqueID, devices := range r.uniqueDevices {
		if len(devices) < 2 {
			continue
		}
		ids := make([]string, 0, len(devices))
		for deviceID := range devices {
			ids = append(ids, deviceID)
		}
		sort.Strings(ids)
		result.DuplicateUniqueIDs[uniqueID] = ids
	}
	return result
}

// snapshotLocked builds every device's relations from a single childrenByParent
// pass instead of relationsLocked's per-device scan over all devices, which
// would make a full Snapshot() cost O(devices^2).
func (r *Registry) snapshotLocked() []DeviceView {
	childrenByParent := make(map[string][]DeviceRelation, len(r.devices))
	seenChild := make(map[string]map[string]bool, len(r.devices))
	addChild := func(parentID string, relation DeviceRelation) {
		if parentID == "" || relation.ID == parentID {
			return
		}
		if seenChild[parentID] == nil {
			seenChild[parentID] = map[string]bool{}
		}
		if seenChild[parentID][relation.ID] {
			return
		}
		seenChild[parentID][relation.ID] = true
		childrenByParent[parentID] = append(childrenByParent[parentID], relation)
	}
	for id, dev := range r.devices {
		if parentID := dev.info.ViaDevice; parentID != "" && parentID != id {
			addChild(parentID, DeviceRelation{ID: id, Name: dev.info.Name, Kind: "child"})
		}
	}
	for _, override := range r.relationOverrides {
		if override.ChildID == "" || override.ParentID == "" || override.ChildID == override.ParentID {
			continue
		}
		name := ""
		if dev, exists := r.devices[override.ChildID]; exists {
			name = dev.info.Name
		}
		addChild(override.ParentID, DeviceRelation{ID: override.ChildID, Name: name, Kind: "child"})
	}
	views := make([]DeviceView, 0, len(r.devices))
	for id, dev := range r.devices {
		view := toView(id, dev)
		view.Relations = r.sortedRelationsLocked(id, dev, childrenByParent[id])
		views = append(views, view)
	}
	sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	return views
}

func (r *Registry) relationsLocked(deviceID string) []DeviceRelation {
	current, ok := r.devices[deviceID]
	if !ok {
		return []DeviceRelation{}
	}
	seen := map[string]bool{}
	var children []DeviceRelation
	addChild := func(id, name string) {
		if id == "" || id == deviceID || seen[id] {
			return
		}
		seen[id] = true
		children = append(children, DeviceRelation{ID: id, Name: name, Kind: "child"})
	}
	for id, dev := range r.devices {
		if id != deviceID && dev.info.ViaDevice == deviceID {
			addChild(id, dev.info.Name)
		}
	}
	for _, override := range r.relationOverrides {
		if override.ParentID == deviceID {
			name := ""
			if dev, exists := r.devices[override.ChildID]; exists {
				name = dev.info.Name
			}
			addChild(override.ChildID, name)
		}
	}
	return r.sortedRelationsLocked(deviceID, current, children)
}

func (r *Registry) sortedRelationsLocked(deviceID string, dev *device, children []DeviceRelation) []DeviceRelation {
	result := make([]DeviceRelation, 0, len(children)+1)
	seenParent := map[string]bool{}
	addParent := func(parentID string) {
		if parentID == "" || parentID == deviceID || seenParent[parentID] {
			return
		}
		seenParent[parentID] = true
		parentName := ""
		if parent, exists := r.devices[parentID]; exists {
			parentName = parent.info.Name
		}
		result = append(result, DeviceRelation{ID: parentID, Name: parentName, Kind: "parent"})
	}
	addParent(dev.info.ViaDevice)
	for _, override := range r.relationOverrides {
		if override.ChildID == deviceID {
			addParent(override.ParentID)
		}
	}
	result = append(result, children...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return result[i].ID < result[j].ID
	})
	return result
}

// DeviceIDForUnique returns the device a unique_id currently belongs to,
// without paying for a full Snapshot() when only the owning device ID is
// needed (e.g. to file a command-history entry).
func (r *Registry) DeviceIDForUnique(uniqueID string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	deviceID, ok := r.byUnique[uniqueID]
	return deviceID, ok
}

// Get returns a single device's snapshot by ID.
func (r *Registry) Get(deviceID string) (DeviceView, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	dev, ok := r.devices[deviceID]
	if !ok {
		return DeviceView{}, false
	}
	view := toView(deviceID, dev)
	view.Relations = r.relationsLocked(deviceID)
	return view, true
}

// Topics returns all state and availability topics currently known from MQTT
// discovery, sorted for stable selector rendering.
func (r *Registry) Topics() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	topics := make([]string, 0, len(r.byTopic))
	for topic := range r.byTopic {
		topics = append(topics, topic)
	}
	sort.Strings(topics)
	return topics
}

// TopicSample is one known topic together with the payload last seen on it.
// Used by the configuration form to suggest JSON keys for foreign topics.
// At is a pointer because `omitempty` does not apply to a zero time.Time - a
// topic that never carried a message would otherwise report year 1.
type TopicSample struct {
	Topic    string     `json:"topic"`
	Known    bool       `json:"known"`
	Payload  string     `json:"payload,omitempty"`
	At       *time.Time `json:"at,omitempty"`
	Device   string     `json:"device,omitempty"`
	DeviceID string     `json:"device_id,omitempty"`
	Source   string     `json:"source,omitempty"`
}

// sampleForTopicLocked baut den TopicSample-Eintrag fuer genau ein Topic -
// gemeinsamer Kern von TopicSamples() (alle bekannten Topics) und
// TopicSamplesFor() (eine angefragte Teilmenge, inklusive unbekannter
// Topics). Caller haelt r.mu bereits.
func (r *Registry) sampleForTopicLocked(topic string) TopicSample {
	uniqueIDs, known := r.byTopic[topic]
	sample := TopicSample{Topic: topic, Known: known}
	if !known {
		return sample
	}
	ids := make([]string, 0, len(uniqueIDs))
	for uniqueID := range uniqueIDs {
		ids = append(ids, uniqueID)
	}
	sort.Strings(ids)
	for _, uniqueID := range ids {
		deviceID, ok := r.byUnique[uniqueID]
		if !ok {
			continue
		}
		dev, ok := r.devices[deviceID]
		if !ok {
			continue
		}
		if sample.DeviceID == "" {
			sample.DeviceID = deviceID
			sample.Device = dev.info.Name
			if sample.Device == "" {
				sample.Device = deviceID
			}
		}
		es := dev.entities[uniqueID]
		if es == nil || es.lastStateMessage == nil || es.lastStateMessage.Topic != topic {
			continue
		}
		at := es.lastStateMessage.At
		sample.Payload = es.lastStateMessage.Payload
		sample.At = &at
		source := es.source
		if source == "" {
			source = "live"
		}
		sample.Source = source
		break
	}
	return sample
}

// TopicSamples returns every topic from Topics() with its last payload, if one
// has arrived. Only the state topic branch of updateStateLocked records
// lastStateMessage, so an availability-only topic never carries a payload and a
// state topic keeps its payload even after an availability heartbeat.
func (r *Registry) TopicSamples() []TopicSample {
	r.mu.RLock()
	defer r.mu.RUnlock()
	topics := make([]string, 0, len(r.byTopic))
	for topic := range r.byTopic {
		topics = append(topics, topic)
	}
	sort.Strings(topics)
	samples := make([]TopicSample, 0, len(topics))
	for _, topic := range topics {
		samples = append(samples, r.sampleForTopicLocked(topic))
	}
	return samples
}

// TopicSamplesFor liefert genau einen Eintrag je angefragtem Topic, in
// Anfragereihenfolge - Known:false statt eines fehlenden Eintrags, wenn das
// Topic in dieser Anlage nicht bekannt ist. Fuer gezielte Abfragen (siehe
// config.page.js, Task 8) statt der vollen, potenziell grossen Liste.
func (r *Registry) TopicSamplesFor(topics []string) []TopicSample {
	r.mu.RLock()
	defer r.mu.RUnlock()
	samples := make([]TopicSample, 0, len(topics))
	for _, topic := range topics {
		samples = append(samples, r.sampleForTopicLocked(topic))
	}
	return samples
}

// DiscoveryTopics returns all discovery topics belonging to one active device.
func (r *Registry) DiscoveryTopics(deviceID string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	dev, ok := r.devices[deviceID]
	if !ok {
		return []string{}
	}
	topics := make([]string, 0, len(dev.entities))
	for _, entity := range dev.entities {
		if entity.discoveryTopic != "" {
			topics = append(topics, entity.discoveryTopic)
		}
	}
	return uniqueStrings(topics)
}

// DuplicateUniqueIDs returns the unique IDs that are registered for more than
// one device, together with their deterministically sorted device IDs.
func (r *Registry) DuplicateUniqueIDs() map[string][]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string][]string)
	for uniqueID, devices := range r.uniqueDevices {
		if len(devices) < 2 {
			continue
		}
		ids := make([]string, 0, len(devices))
		for deviceID := range devices {
			ids = append(ids, deviceID)
		}
		sort.Strings(ids)
		result[uniqueID] = ids
	}
	return result
}

func toView(id string, dev *device) DeviceView {
	dv := DeviceView{
		ID:            id,
		Name:          dev.info.Name,
		Manufacturer:  dev.info.Manufacturer,
		Model:         dev.info.Model,
		Firmware:      dev.info.Firmware,
		ViaDevice:     dev.info.ViaDevice,
		DiscoveryJSON: make([]string, 0, len(dev.entities)),
		Entities:      make([]EntityView, 0, len(dev.entities)),
	}
	for _, es := range dev.entities {
		if es.discoveryJSON != "" {
			dv.DiscoveryJSON = append(dv.DiscoveryJSON, es.discoveryJSON)
		}
		source := es.source
		if source == "" {
			source = "live"
		}
		freshness := "missing"
		if es.hasValue {
			freshness = "fresh"
			if es.lastRetained {
				freshness = "stale"
			}
		}
		ageSeconds := int64(0)
		if !es.lastSeen.IsZero() {
			age := time.Since(es.lastSeen)
			if age > 0 {
				ageSeconds = int64(age / time.Second)
			}
		}
		// Lazily expired here (rather than relying solely on
		// ExpirePendingCommands' periodic sweep) so a Snapshot taken right
		// after the deadline never reports a stale "still pending" view.
		pending := es.pending && time.Now().Before(es.pendingDeadline)
		dv.Entities = append(dv.Entities, EntityView{
			UniqueID:            es.info.UniqueID,
			Component:           es.info.Component,
			ObjectID:            es.info.ObjectID,
			Name:                es.info.Name,
			DiscoveryJSON:       es.discoveryJSON,
			Icon:                es.info.Icon,
			StateTopic:          es.info.StateTopic,
			AvailabilityTopic:   es.info.AvailabilityTopic,
			Availability:        append([]AvailabilityInfo(nil), es.info.Availability...),
			AvailabilityMode:    es.info.AvailabilityMode,
			Commandable:         isCommandable(es.info),
			PayloadOn:           es.info.PayloadOn,
			PayloadOff:          es.info.PayloadOff,
			PayloadAvailable:    es.info.PayloadAvailable,
			PayloadNotAvailable: es.info.PayloadNotAvailable,
			ValueTemplate:       es.info.ValueTemplate,
			TemplateSupported:   es.info.ValueTemplate == "" || compileValueTemplate(es.info.ValueTemplate) != nil,
			UnitOfMeasurement:   es.info.UnitOfMeasurement,
			DeviceClass:         es.info.DeviceClass,
			MinValue:            es.info.MinValue,
			MaxValue:            es.info.MaxValue,
			Step:                es.info.Step,
			DefaultHidden:       es.info.DefaultHidden,
			EntityCategory:      es.info.EntityCategory,
			Value:               es.value,
			HasValue:            es.hasValue,
			Available:           es.available,
			HasAvailability:     es.hasAvailability,
			LastSeen:            es.lastSeen,
			LastTopicAt:         es.lastTopicAt,
			AgeSeconds:          ageSeconds,
			Source:              source,
			Freshness:           freshness,
			CommandTopic:        es.info.CommandTopic,
			DiscoveryTopic:      es.discoveryTopic,
			DiscoveryRetained:   es.discoveryRetained,
			DiscoveryQoS:        es.discoveryQoS,
			DiscoverySource:     es.discoverySource,
			LastMessage:         cloneMQTTMessage(es.lastMessage),
			PayloadValid:        es.payloadValid,
			PayloadError:        es.payloadError,
			Stale:               es.lastRetained,
			Pending:             pending,
			PendingValue:        es.pendingValue,
			PendingSince:        es.pendingSince,
			PendingDeadline:     es.pendingDeadline,
			LastCommandResult:   es.lastCommandResult,
			LastCommandAt:       es.lastCommandAt,
		})
		if es.lastTopicAt.After(dv.LastUpdated) {
			dv.LastUpdated = es.lastTopicAt
		}
	}
	sort.Strings(dv.DiscoveryJSON)
	sort.Slice(dv.Entities, func(i, j int) bool { return dv.Entities[i].ObjectID < dv.Entities[j].ObjectID })
	return dv
}

func newMQTTMessage(topic string, payload []byte, retained bool, qos byte, at time.Time) *MQTTMessage {
	value := string(payload)
	if len(value) > maxLastMessagePayload {
		value = value[:maxLastMessagePayload] + "..."
	}
	return &MQTTMessage{Topic: topic, Payload: value, At: at, Retained: retained, QoS: qos}
}

// updateLastMessage reuses the existing *MQTTMessage (just advancing At) when
// this publish is identical to the one already stored, instead of
// allocating a new struct for every single MQTT message - including the
// common case of a heartbeat/availability topic republishing the same
// payload. Payloads at or beyond the truncation limit always allocate fresh
// since the stored value may no longer reflect the exact bytes.
func updateLastMessage(existing *MQTTMessage, topic string, payload []byte, retained bool, qos byte, at time.Time) *MQTTMessage {
	if existing != nil && existing.Topic == topic && existing.Retained == retained && existing.QoS == qos &&
		len(payload) <= maxLastMessagePayload && existing.Payload == string(payload) {
		existing.At = at
		return existing
	}
	return newMQTTMessage(topic, payload, retained, qos, at)
}

func cloneMQTTMessage(value *MQTTMessage) *MQTTMessage {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func primaryDevice(devices map[string]bool) string {
	ids := make([]string, 0, len(devices))
	for deviceID := range devices {
		ids = append(ids, deviceID)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func isCommandable(info EntityInfo) bool {
	if info.CommandTopic == "" {
		return false
	}
	if info.Component == "switch" {
		return info.PayloadOn != "" && info.PayloadOff != ""
	}
	return info.Component == "number"
}

func validatePayload(payload []byte, valueTemplate string) (bool, string) {
	return validatePayloadParsed(payload, compileValueTemplate(valueTemplate), nil, nil)
}

func validatePayloadParsed(payload []byte, valueTemplate *parsedValueTemplate, parsedPayload any, parseErr error) (bool, string) {
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" {
		return false, "empty payload"
	}
	if valueTemplate != nil {
		parsed, ok := parsedPayload.(map[string]any)
		if parseErr != nil || !ok {
			return false, "value template requires valid JSON"
		}
		if _, ok := lookupJSONPath(parsed, valueTemplate.path); !ok && !valueTemplate.defaultZero {
			return false, "value template field is missing"
		}
		return true, ""
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		if parseErr != nil {
			return false, "invalid JSON payload"
		}
	}
	return true, ""
}

// extractValue applies the subset of Home Assistant value_template syntax
// used by the bridges, including the node's nested service update templates.
func extractValue(payload []byte, valueTemplate string) string {
	parsedTemplate := compileValueTemplate(valueTemplate)
	if parsedTemplate == nil {
		return string(payload)
	}
	parsedPayload, parseErr := decodePayload(payload)
	return extractValueParsed(payload, parsedTemplate, parsedPayload, parseErr)
}

func extractValueParsed(payload []byte, valueTemplate *parsedValueTemplate, parsedPayload any, parseErr error) string {
	if valueTemplate == nil {
		return string(payload)
	}
	parsed, ok := parsedPayload.(map[string]any)
	if parseErr != nil || !ok {
		return string(payload)
	}
	value, ok := lookupJSONPath(parsed, valueTemplate.path)
	if !ok || value == nil {
		if !valueTemplate.defaultZero {
			return ""
		}
		value = float64(0)
	}
	if valueTemplate.whenTrue != "" {
		if isTruthyJSONValue(value) {
			return valueTemplate.whenTrue
		}
		return valueTemplate.whenFalse
	}
	if valueTemplate.integer {
		value = integerValue(value)
	}
	if valueTemplate.timestampLocal {
		numeric, ok := numericValue(value)
		if !ok {
			return ""
		}
		return time.Unix(int64(numeric), 0).Local().Format(time.RFC3339)
	}
	return fmt.Sprintf("%v", value)
}

type parsedValueTemplate struct {
	path                 []string
	whenTrue, whenFalse  string
	defaultZero, integer bool
	timestampLocal       bool
}

var booleanValueJSONFieldPattern = regexp.MustCompile(`^\{\{\s*'([^']*)'\s+if\s+value_json\.([A-Za-z0-9_]+)\s+else\s+'([^']*)'\s*\}\}$`)
var valueJSONPathPattern = regexp.MustCompile(`^\{\{\s*value_json\.([A-Za-z0-9_]+)(?:\[['"]([^'"]+)['"]\])?(?:\s*\|\s*default\(0\))?(?:\s*\|\s*int)?(?:\s*\|\s*timestamp_local)?\s*\}\}$`)

func parseValueTemplate(valueTemplate string) (parsedValueTemplate, bool) {
	if valueTemplate == "" {
		return parsedValueTemplate{}, false
	}
	trimmed := strings.TrimSpace(valueTemplate)
	if m := booleanValueJSONFieldPattern.FindStringSubmatch(trimmed); m != nil {
		return parsedValueTemplate{path: []string{m[2]}, whenTrue: m[1], whenFalse: m[3]}, true
	}
	if m := valueJSONPathPattern.FindStringSubmatch(trimmed); m != nil {
		path := []string{m[1]}
		if m[2] != "" {
			path = append(path, m[2])
		}
		return parsedValueTemplate{
			path:           path,
			defaultZero:    strings.Contains(trimmed, "| default(0)"),
			integer:        strings.Contains(trimmed, "| int"),
			timestampLocal: strings.Contains(trimmed, "| timestamp_local"),
		}, true
	}
	return parsedValueTemplate{}, false
}

func compileValueTemplate(valueTemplate string) *parsedValueTemplate {
	parsed, ok := parseValueTemplate(valueTemplate)
	if !ok {
		return nil
	}
	return &parsed
}

func decodePayload(payload []byte) (any, error) {
	var parsed any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func lookupJSONPath(payload map[string]any, path []string) (any, bool) {
	var current any = payload
	for _, segment := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func integerValue(value any) any {
	numeric, ok := numericValue(value)
	if !ok {
		return value
	}
	return int64(numeric)
}

func numericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case string:
		numeric, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return numeric, err == nil
	default:
		return 0, false
	}
}

func isTruthyJSONValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case nil:
		return false
	case string:
		return typed != ""
	case float64:
		return typed != 0
	default:
		return true
	}
}
