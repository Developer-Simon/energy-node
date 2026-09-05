// Package diagnostics will hold the DiagnosticRule interface and the
// DiagnosticsEngine that runs all registered rules (DuplicateUniqueID,
// MissingAvailability, WrongUnit, MissingTopic, NoStateUpdate, Offline,
// InvalidPayload, DiscoveryMismatch) against normalized device state,
// discovery data and MQTT metadata, plus the per-device Health-Score
// computed against the settings.json threshold.
package diagnostics

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/devicefilter"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

type Warning struct {
	RuleID   string         `json:"rule_id"`
	Severity Severity       `json:"severity"`
	DeviceID string         `json:"device_id"`
	EntityID string         `json:"entity_id,omitempty"`
	Topic    string         `json:"topic,omitempty"`
	Message  string         `json:"message"`
	Hint     string         `json:"hint"`
	Details  map[string]any `json:"details,omitempty"`
	At       time.Time      `json:"at"`
}

type Context struct {
	Now                time.Time
	NoStateUpdateAfter time.Duration
	DuplicateUniqueIDs map[string][]string
}

type DeviceHealth struct {
	DeviceID         string    `json:"device_id"`
	Score            int       `json:"score"`
	Status           string    `json:"status"`
	ThresholdSeconds int64     `json:"threshold_seconds"`
	LastSeen         time.Time `json:"last_seen,omitempty"`
	WarningCount     int       `json:"warning_count"`
}

type DiagnosticRule interface {
	RuleID() string
	Evaluate(device registry.DeviceView, ctx Context) []Warning
}

// SnapshotContext carries the cross-cutting state a SnapshotRule needs to
// evaluate the whole registry at once, as opposed to DiagnosticRule/Context
// which evaluate exactly one device.
type SnapshotContext struct {
	Now       time.Time
	Ignored   *devicefilter.Store
	Configs   *config.Manager
	StartedAt time.Time
}

// SnapshotRule evaluates diagnostics that have no single registry.DeviceView
// to run against — e.g. a device that is configured locally but entirely
// absent from the registry has no DeviceView at all.
type SnapshotRule interface {
	RuleID() string
	Evaluate(snapshot registry.DiagnosticsSnapshot, ctx SnapshotContext) []Warning
}

type Engine struct {
	Registry      *registry.Registry
	Settings      *settings.Store
	Rules         []DiagnosticRule
	SnapshotRules []SnapshotRule
	Ignored       *devicefilter.Store
	Configs       *config.Manager
	StartedAt     time.Time
}

func NewEngine(reg *registry.Registry, store *settings.Store) *Engine {
	return &Engine{
		Registry: reg,
		Settings: store,
		Rules: []DiagnosticRule{
			DuplicateUniqueIDRule{},
			DiscoveryMismatchRule{},
			DiscoveryRetainedRule{},
			MissingTopicRule{},
			MissingAvailabilityRule{},
			WrongUnitRule{},
			NoStateUpdateRule{},
			OfflineRule{},
			InvalidPayloadRule{},
		},
		SnapshotRules: []SnapshotRule{
			InvalidDiscoveryErrorRule{},
			IgnoredDeviceDiscoveryStaleRule{},
			ConfiguredDeviceMissingRule{},
		},
	}
}

func (e *Engine) SetIgnoredStore(store *devicefilter.Store) {
	e.Ignored = store
}

func (e *Engine) SetConfigManager(configs *config.Manager) {
	e.Configs = configs
}

func (e *Engine) SetStartedAt(startedAt time.Time) {
	e.StartedAt = startedAt
}

func (e *Engine) Evaluate(now time.Time) []Warning {
	if e.Registry == nil {
		return []Warning{}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	snapshot := e.Registry.DiagnosticsSnapshot()
	return e.evaluate(now, snapshot, e.settingsThreshold())
}

func (e *Engine) evaluate(now time.Time, snapshot registry.DiagnosticsSnapshot, threshold time.Duration) []Warning {
	if e.Registry == nil {
		return []Warning{}
	}
	var warnings []Warning
	for _, device := range snapshot.Devices {
		ctx := Context{
			Now:                now,
			NoStateUpdateAfter: e.thresholdForDevice(device, threshold),
			DuplicateUniqueIDs: snapshot.DuplicateUniqueIDs,
		}
		for _, rule := range e.Rules {
			if rule == nil {
				continue
			}
			warnings = append(warnings, rule.Evaluate(device, ctx)...)
		}
	}
	snapCtx := SnapshotContext{Now: now, Ignored: e.Ignored, Configs: e.Configs, StartedAt: e.StartedAt}
	for _, rule := range e.SnapshotRules {
		if rule == nil {
			continue
		}
		warnings = append(warnings, rule.Evaluate(snapshot, snapCtx)...)
	}
	return deduplicateAndSort(warnings)
}

func (e *Engine) settingsThreshold() time.Duration {
	threshold := 3 * time.Minute
	if e.Settings != nil {
		if value, err := e.Settings.LoadSettings(); err == nil && value.HealthScoreThreshold > 0 {
			threshold = time.Duration(value.HealthScoreThreshold) * time.Second
		}
	}
	return threshold
}

func (e *Engine) RuleIDs() []string {
	seen := make(map[string]bool)
	for _, rule := range e.Rules {
		if rule != nil {
			seen[rule.RuleID()] = true
		}
	}
	for _, rule := range e.SnapshotRules {
		if rule != nil {
			seen[rule.RuleID()] = true
		}
	}
	result := make([]string, 0, len(seen))
	for ruleID := range seen {
		result = append(result, ruleID)
	}
	sort.Strings(result)
	return result
}

func (e *Engine) thresholdForDevice(device registry.DeviceView, fallback time.Duration) time.Duration {
	for _, entity := range device.Entities {
		if entity.ObjectID != "poll_interval_s" || !entity.HasValue {
			continue
		}
		seconds, err := strconv.ParseFloat(strings.TrimSpace(entity.Value), 64)
		if err == nil && seconds > 0 && !math.IsInf(seconds, 0) && !math.IsNaN(seconds) {
			return time.Duration(3 * seconds * float64(time.Second))
		}
	}
	return fallback
}

func (e *Engine) Health(now time.Time) []DeviceHealth {
	if e.Registry == nil {
		return []DeviceHealth{}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	fallback := e.settingsThreshold()
	snapshot := e.Registry.DiagnosticsSnapshot()
	warningCounts := make(map[string]int)
	for _, item := range e.evaluate(now, snapshot, fallback) {
		warningCounts[item.DeviceID]++
	}
	result := make([]DeviceHealth, 0)
	for _, device := range snapshot.Devices {
		threshold := e.thresholdForDevice(device, fallback)
		var lastSeen time.Time
		offline := false
		for _, entity := range device.Entities {
			if entity.ObjectID == "poll_interval_s" {
				continue
			}
			if entity.LastSeen.After(lastSeen) {
				lastSeen = entity.LastSeen
			}
			if entity.HasAvailability && !entity.Available {
				offline = true
			}
		}
		score := 0
		status := "unknown"
		if !lastSeen.IsZero() {
			age := now.Sub(lastSeen)
			if age < 0 {
				age = 0
			}
			score = int(math.Round(100 - float64(age)/float64(threshold)*100))
			if score < 0 {
				score = 0
			}
			if score > 100 {
				score = 100
			}
			status = healthStatus(score)
		}
		if offline {
			score = 0
			status = "critical"
		}
		result = append(result, DeviceHealth{
			DeviceID:         device.ID,
			Score:            score,
			Status:           status,
			ThresholdSeconds: int64(threshold / time.Second),
			LastSeen:         lastSeen,
			WarningCount:     warningCounts[device.ID],
		})
	}
	return result
}

func healthStatus(score int) string {
	switch {
	case score >= 80:
		return "healthy"
	case score >= 50:
		return "degraded"
	default:
		return "unhealthy"
	}
}

func warning(rule string, severity Severity, deviceID string, entity registry.EntityView, message, hint string, at time.Time) Warning {
	return Warning{RuleID: rule, Severity: severity, DeviceID: deviceID, EntityID: entity.UniqueID, Topic: entity.StateTopic, Message: message, Hint: hint, At: at}
}

func warningWithDetails(rule string, severity Severity, deviceID string, entity registry.EntityView, message, hint string, details map[string]any, at time.Time) Warning {
	result := warning(rule, severity, deviceID, entity, message, hint, at)
	result.Details = details
	return result
}

func deduplicateAndSort(warnings []Warning) []Warning {
	seen := make(map[string]bool, len(warnings))
	result := make([]Warning, 0, len(warnings))
	for _, item := range warnings {
		key := strings.Join([]string{item.RuleID, item.DeviceID, item.EntityID, item.Topic}, "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Severity != result[j].Severity {
			return result[i].Severity > result[j].Severity
		}
		if result[i].DeviceID != result[j].DeviceID {
			return result[i].DeviceID < result[j].DeviceID
		}
		if result[i].EntityID != result[j].EntityID {
			return result[i].EntityID < result[j].EntityID
		}
		return result[i].RuleID < result[j].RuleID
	})
	return result
}

// Device evaluates diagnostics for a single device without paying for a full
// DiagnosticsSnapshot and rule pass over every other device (see Evaluate).
// Discovery-parse-error warnings carry no DeviceID (see evaluate above) and
// therefore never matched a specific device's filter here anyway, so this
// path omits them entirely instead of computing and then discarding them.
func (e *Engine) Device(deviceID string, now time.Time) []Warning {
	if e.Registry == nil {
		return []Warning{}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var warnings []Warning
	if dev, ok := e.Registry.Get(deviceID); ok {
		ctx := Context{
			Now:                now,
			NoStateUpdateAfter: e.thresholdForDevice(dev, e.settingsThreshold()),
			DuplicateUniqueIDs: e.Registry.DuplicateUniqueIDs(),
		}
		for _, rule := range e.Rules {
			if rule == nil {
				continue
			}
			warnings = append(warnings, rule.Evaluate(dev, ctx)...)
		}
	}
	if e.Ignored != nil {
		if record, ok := e.Ignored.Get(deviceID); ok {
			if missing, stale := e.Ignored.Missing(deviceID); stale {
				topic := ""
				if len(missing) > 0 {
					topic = missing[0]
				}
				warnings = append(warnings, Warning{
					RuleID: "IgnoredDeviceDiscoveryStale", Severity: SeverityWarning,
					DeviceID: deviceID, Topic: topic,
					Message: "Ignoriertes Gerät besitzt keine bekannte Discovery mehr.",
					Hint:    "Gerät reaktivieren, falls es wieder benötigt wird, oder Discovery endgültig löschen",
					Details: map[string]any{
						"ignored":                  true,
						"missing_discovery_topics": missing,
						"ignored_at":               record.IgnoredAt,
					},
					At: now,
				})
			}
		}
	}
	return deduplicateAndSort(warnings)
}

type MissingTopicRule struct{}

type DuplicateUniqueIDRule struct{}

func (DuplicateUniqueIDRule) RuleID() string { return "DuplicateUniqueID" }

func (rule DuplicateUniqueIDRule) Evaluate(device registry.DeviceView, ctx Context) []Warning {
	var result []Warning
	for _, entity := range device.Entities {
		deviceIDs := ctx.DuplicateUniqueIDs[entity.UniqueID]
		if len(deviceIDs) < 2 {
			continue
		}
		result = append(result, warningWithDetails(rule.RuleID(), SeverityCritical, device.ID, entity, "Unique-ID ist mehreren Geräten zugeordnet.", "Unique-ID in der Discovery-Konfiguration eindeutig vergeben", map[string]any{
			"unique_id":  entity.UniqueID,
			"device_ids": deviceIDs,
		}, ctx.Now))
	}
	return result
}

type DiscoveryMismatchRule struct{}

func (DiscoveryMismatchRule) RuleID() string { return "DiscoveryMismatch" }

func (rule DiscoveryMismatchRule) Evaluate(device registry.DeviceView, ctx Context) []Warning {
	var result []Warning
	for _, entity := range device.Entities {
		if strings.TrimSpace(entity.DiscoveryJSON) == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(entity.DiscoveryJSON), &payload); err != nil {
			result = append(result, warningWithDetails(rule.RuleID(), SeverityCritical, device.ID, entity, "Discovery-Payload ist kein gültiges JSON.", "Discovery-Payload und Retain-Zustand prüfen", map[string]any{"error": err.Error()}, ctx.Now))
			continue
		}
		if mismatch := compareDiscovery(payload, device, entity); mismatch != nil {
			result = append(result, warningWithDetails(rule.RuleID(), SeverityWarning, device.ID, entity, "Discovery-Daten stimmen nicht mit dem normalisierten Entitätsmodell überein.", "Discovery-Payload und Registry-Zustand vergleichen", mismatch, ctx.Now))
		}
	}
	return result
}

func compareDiscovery(payload map[string]any, device registry.DeviceView, entity registry.EntityView) map[string]any {
	fields := []struct {
		name     string
		expected string
	}{
		{"unique_id", entity.UniqueID},
		{"name", entity.Name},
		{"state_topic", entity.StateTopic},
		{"availability_topic", entity.AvailabilityTopic},
		{"command_topic", entity.CommandTopic},
	}
	for _, field := range fields {
		actual, present := payload[field.name]
		actualString, isString := actual.(string)
		if !present {
			if field.expected != "" && field.name != "command_topic" {
				return map[string]any{"field": field.name, "expected": field.expected, "actual": nil}
			}
			continue
		}
		if !isString || actualString != field.expected {
			return map[string]any{"field": field.name, "expected": field.expected, "actual": actual}
		}
	}
	devicePayload, ok := payload["device"].(map[string]any)
	if !ok {
		return map[string]any{"field": "device", "expected": "object", "actual": payload["device"]}
	}
	deviceFields := []struct {
		name     string
		expected string
	}{
		{"name", device.Name},
		{"manufacturer", device.Manufacturer},
		{"model", device.Model},
	}
	for _, field := range deviceFields {
		if field.expected == "" {
			continue
		}
		actual, present := devicePayload[field.name]
		actualString, isString := actual.(string)
		if !present || !isString || actualString != field.expected {
			return map[string]any{"field": "device." + field.name, "expected": field.expected, "actual": actual}
		}
	}
	if entity.DiscoveryTopic != "" {
		parts := strings.Split(entity.DiscoveryTopic, "/")
		if len(parts) != 5 || parts[0] != "homeassistant" || parts[1] != entity.Component || parts[3] != entity.ObjectID || parts[4] != "config" {
			return map[string]any{"field": "discovery_topic", "expected": "homeassistant/{component}/{device_id}/{object_id}/config", "actual": entity.DiscoveryTopic}
		}
	}
	return nil
}

type DiscoveryRetainedRule struct{}

func (DiscoveryRetainedRule) RuleID() string { return "DiscoveryRetained" }

func (rule DiscoveryRetainedRule) Evaluate(device registry.DeviceView, ctx Context) []Warning {
	var result []Warning
	for _, entity := range device.Entities {
		if entity.DiscoveryTopic != "" && !entity.DiscoveryRetained {
			result = append(result, warningWithDetails(rule.RuleID(), SeverityWarning, device.ID, entity, "Discovery-Payload wurde nicht retained veröffentlicht.", "Discovery-Topic retained veröffentlichen", map[string]any{
				"discovery_topic": entity.DiscoveryTopic,
				"retained":        entity.DiscoveryRetained,
			}, ctx.Now))
		}
	}
	return result
}

func (MissingTopicRule) RuleID() string { return "MissingTopic" }

func (rule MissingTopicRule) Evaluate(device registry.DeviceView, ctx Context) []Warning {
	var result []Warning
	for _, entity := range device.Entities {
		if entity.StateTopic == "" {
			result = append(result, warning(rule.RuleID(), SeverityCritical, device.ID, entity, "State-Topic fehlt.", "Discovery-Konfiguration prüfen", ctx.Now))
		}
	}
	return result
}

type MissingAvailabilityRule struct{}

func (MissingAvailabilityRule) RuleID() string { return "MissingAvailability" }

func (rule MissingAvailabilityRule) Evaluate(device registry.DeviceView, ctx Context) []Warning {
	var result []Warning
	for _, entity := range device.Entities {
		if entity.AvailabilityTopic != "" {
			continue
		}
		// Die Erreichbarkeits-Entity selbst (binary_sensor mit device_class
		// connectivity) traegt bewusst kein Availability-Topic: sie *ist* das
		// Verfuegbarkeitssignal des Geraets und kann sich nicht auf sich
		// selbst beziehen.
		if entity.Component == "binary_sensor" && entity.DeviceClass == "connectivity" {
			continue
		}
		result = append(result, warning(rule.RuleID(), SeverityWarning, device.ID, entity, "Kein Availability-Topic konfiguriert.", "Availability-Topic und Online-/Offline-Payload ergänzen", ctx.Now))
	}
	return result
}

type WrongUnitRule struct{}

func (WrongUnitRule) RuleID() string { return "WrongUnit" }

func (rule WrongUnitRule) Evaluate(device registry.DeviceView, ctx Context) []Warning {
	var result []Warning
	for _, entity := range device.Entities {
		if entity.Component == "sensor" && strings.TrimSpace(entity.UnitOfMeasurement) == "" {
			result = append(result, warning(rule.RuleID(), SeverityWarning, device.ID, entity, "Sensor hat keine Einheit.", "Einheit in der Discovery-Konfiguration setzen", ctx.Now))
		}
	}
	return result
}

type NoStateUpdateRule struct{}

func (NoStateUpdateRule) RuleID() string { return "NoStateUpdate" }

func (rule NoStateUpdateRule) Evaluate(device registry.DeviceView, ctx Context) []Warning {
	var result []Warning
	for _, entity := range device.Entities {
		if !entity.HasValue || entity.LastSeen.IsZero() || ctx.Now.Sub(entity.LastSeen) > ctx.NoStateUpdateAfter {
			result = append(result, warning(rule.RuleID(), SeverityWarning, device.ID, entity, "Seit dem letzten State-Update ist der Schwellwert überschritten.", "Bridge, MQTT-State-Topic und Polling prüfen", ctx.Now))
		}
	}
	return result
}

type OfflineRule struct{}

func (OfflineRule) RuleID() string { return "Offline" }

func (rule OfflineRule) Evaluate(device registry.DeviceView, ctx Context) []Warning {
	var result []Warning
	for _, entity := range device.Entities {
		if entity.HasAvailability && !entity.Available {
			result = append(result, warning(rule.RuleID(), SeverityCritical, device.ID, entity, "Gerät meldet sich als offline.", "Stromversorgung, Netzwerk und Bridge prüfen", ctx.Now))
		}
	}
	return result
}

type InvalidPayloadRule struct{}

func (InvalidPayloadRule) RuleID() string { return "InvalidPayload" }

func (rule InvalidPayloadRule) Evaluate(device registry.DeviceView, ctx Context) []Warning {
	var result []Warning
	for _, entity := range device.Entities {
		if entity.HasValue && (!entity.PayloadValid || strings.TrimSpace(entity.Value) == "") {
			result = append(result, warningWithDetails(rule.RuleID(), SeverityWarning, device.ID, entity, "State-Payload ist leer.", "Payload-Format und Value-Template prüfen", map[string]any{
				"value":  entity.Value,
				"source": entity.Source,
				"error":  entity.PayloadError,
			}, ctx.Now))
		}
	}
	return result
}

type InvalidDiscoveryErrorRule struct{}

func (InvalidDiscoveryErrorRule) RuleID() string { return "InvalidPayload" }

func (InvalidDiscoveryErrorRule) Evaluate(snapshot registry.DiagnosticsSnapshot, ctx SnapshotContext) []Warning {
	var result []Warning
	for _, issue := range snapshot.DiscoveryErrors {
		result = append(result, Warning{
			RuleID:   "InvalidPayload",
			Severity: SeverityCritical,
			Topic:    issue.Topic,
			Message:  "Discovery-Payload ist ungueltig.",
			Hint:     "Discovery-Payload, JSON-Struktur und Availability-Definition pruefen",
			Details: map[string]any{
				"kind":     "discovery",
				"error":    issue.Error,
				"payload":  issue.Payload,
				"retained": issue.Retained,
				"qos":      issue.QoS,
				"source":   issue.Source,
			},
			At: issue.At,
		})
	}
	return result
}

type IgnoredDeviceDiscoveryStaleRule struct{}

func (IgnoredDeviceDiscoveryStaleRule) RuleID() string { return "IgnoredDeviceDiscoveryStale" }

func (rule IgnoredDeviceDiscoveryStaleRule) Evaluate(snapshot registry.DiagnosticsSnapshot, ctx SnapshotContext) []Warning {
	if ctx.Ignored == nil {
		return nil
	}
	var result []Warning
	for _, record := range ctx.Ignored.List() {
		missing, stale := ctx.Ignored.Missing(record.DeviceID)
		if !stale {
			continue
		}
		topic := ""
		if len(missing) > 0 {
			topic = missing[0]
		}
		result = append(result, Warning{
			RuleID: rule.RuleID(), Severity: SeverityWarning,
			DeviceID: record.DeviceID, Topic: topic,
			Message: "Ignoriertes Gerät besitzt keine bekannte Discovery mehr.",
			Hint:    "Gerät reaktivieren, falls es wieder benötigt wird, oder Discovery endgültig löschen",
			Details: map[string]any{
				"ignored":                  true,
				"missing_discovery_topics": missing,
				"ignored_at":               record.IgnoredAt,
			},
			At: ctx.Now,
		})
	}
	return result
}

// configuredDeviceMissingStartupGrace withholds ConfiguredDeviceMissing
// warnings for this long after Engine.StartedAt, so retained discovery that
// simply hasn't arrived yet at process start doesn't look like a genuine
// configuration/discovery mismatch. 30s comfortably covers the discovery
// wildcard subscription plus the broker replaying every retained topic.
const configuredDeviceMissingStartupGrace = 30 * time.Second

type ConfiguredDeviceMissingRule struct{}

func (ConfiguredDeviceMissingRule) RuleID() string { return "ConfiguredDeviceMissing" }

func (rule ConfiguredDeviceMissingRule) Evaluate(snapshot registry.DiagnosticsSnapshot, ctx SnapshotContext) []Warning {
	if ctx.Configs == nil {
		return nil
	}
	if !ctx.StartedAt.IsZero() && ctx.Now.Sub(ctx.StartedAt) < configuredDeviceMissingStartupGrace {
		return nil
	}
	known := make(map[string]bool, len(snapshot.Devices))
	for _, device := range snapshot.Devices {
		known[device.ID] = true
	}
	documents, err := ctx.Configs.Scan()
	if err != nil {
		return nil
	}
	var result []Warning
	for _, document := range documents {
		raw, err := ctx.Configs.Read(document.Name)
		if err != nil {
			continue
		}
		var entries []map[string]any
		if err := json.Unmarshal(raw, &entries); err != nil {
			// Not a top-level array of device objects (e.g. automation_rules.json
			// is a single object) - this rule only understands the device-list
			// config shape, so anything else is silently out of scope.
			continue
		}
		for _, entry := range entries {
			id, ok := entry["id"].(string)
			if !ok || id == "" || known[id] {
				continue
			}
			if ctx.Ignored != nil && ctx.Ignored.IsIgnored(id) {
				continue
			}
			name, _ := entry["name"].(string)
			result = append(result, Warning{
				RuleID:   rule.RuleID(),
				Severity: SeverityWarning,
				DeviceID: id,
				Message:  "Konfiguriertes Gerät ist in der Discovery nicht vorhanden.",
				Hint:     "Bridge-Dienst neu starten oder Discovery erneut veröffentlichen lassen",
				Details: map[string]any{
					"config_file":     document.Name + ".json",
					"configured_id":   id,
					"configured_name": name,
				},
				At: ctx.Now,
			})
		}
	}
	return result
}
