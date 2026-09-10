package nodeagent

import (
	"context"
	"encoding/json"

	"github.com/Developer-Simon/energy-node-dashboard/internal/mqttclient"
)

const (
	metricCPUTemp      = "cpu_temp"
	metricCPULoad      = "cpu_load"
	metricRAM          = "ram"
	metricDisk         = "disk"
	metricWiFi         = "wifi_signal"
	metricUndervoltage = "undervoltage"
	metricThrottled    = "throttled"
	metricLastBoot     = "last_boot"
	metricIP           = "ip_address"
	metricMosquitto    = "mosquitto"
	metricTailscale    = "tailscale"
	metricApt          = "apt_updates"
)

// Metrics ist die kanonische, geordnete Liste der abschaltbaren Metriken -
// vom MQTT-Tab (Task 4) und der Statusleisten-Auswahl (Task 7) konsumiert.
var Metrics = []string{
	metricCPUTemp, metricCPULoad, metricRAM, metricDisk, metricWiFi,
	metricUndervoltage, metricThrottled, metricLastBoot,
	metricIP, metricMosquitto, metricTailscale, metricApt,
}

// entity beschreibt eine HA-Discovery-Entity. component ist "sensor" oder
// "binary_sensor"; objectID ist zugleich der unique_id-Suffix; metric ist
// der Abschalt-Schluessel; source ist "state" oder "diagnostics".
type entity struct {
	component string
	objectID  string
	name      string
	metric    string
	source    string
	extra     map[string]any
}

func nodeEntities() []entity {
	tpl := func(field string) string { return "{{ value_json." + field + " }}" }
	boolTpl := func(field string) string {
		return "{{ 'ON' if value_json." + field + " else 'OFF' }}"
	}
	return []entity{
		{"sensor", "cpu_temp", "CPU-Temperatur", metricCPUTemp, "state", map[string]any{
			"value_template": tpl("cpu_temp_c"), "unit_of_measurement": "°C",
			"device_class": "temperature", "state_class": "measurement", "entity_category": "diagnostic"}},
		{"sensor", "cpu_load", "CPU-Auslastung", metricCPULoad, "state", map[string]any{
			"value_template": tpl("cpu_load_pct"), "unit_of_measurement": "%",
			"state_class": "measurement", "icon": "mdi:chip", "entity_category": "diagnostic"}},
		{"sensor", "ram_used", "RAM-Auslastung", metricRAM, "state", map[string]any{
			"value_template": tpl("ram_used_pct"), "unit_of_measurement": "%",
			"state_class": "measurement", "icon": "mdi:memory", "entity_category": "diagnostic"}},
		{"sensor", "disk_used", "Speicherplatz belegt", metricDisk, "state", map[string]any{
			"value_template": tpl("disk_used_pct"), "unit_of_measurement": "%",
			"state_class": "measurement", "icon": "mdi:harddisk", "entity_category": "diagnostic"}},
		{"sensor", "wifi_signal", "WLAN-Signalstärke", metricWiFi, "state", map[string]any{
			"value_template": tpl("wifi_signal_dbm"), "unit_of_measurement": "dBm",
			"device_class": "signal_strength", "state_class": "measurement",
			"entity_category": "diagnostic", "enabled_by_default": false}},
		{"binary_sensor", "undervoltage_now", "Unterspannung aktuell", metricUndervoltage, "state", map[string]any{
			"value_template": boolTpl("undervoltage_now"), "device_class": "problem", "entity_category": "diagnostic"}},
		{"binary_sensor", "undervoltage_occurred", "Unterspannung seit letztem Neustart aufgetreten", metricUndervoltage, "state", map[string]any{
			"value_template": boolTpl("undervoltage_occurred"), "device_class": "problem", "entity_category": "diagnostic"}},
		{"binary_sensor", "throttled_now", "CPU aktuell gedrosselt", metricThrottled, "state", map[string]any{
			"value_template": boolTpl("throttled_now"), "device_class": "problem", "entity_category": "diagnostic"}},
		{"sensor", "last_boot", "Letzter Neustart", metricLastBoot, "state", map[string]any{
			"value_template": tpl("last_boot"), "device_class": "timestamp", "entity_category": "diagnostic"}},
		{"sensor", "ip_address", "IP-Adresse", metricIP, "diagnostics", map[string]any{
			"value_template": tpl("ip_address"), "icon": "mdi:ip-network", "entity_category": "diagnostic"}},
		{"binary_sensor", "mosquitto_running", "Mosquitto läuft", metricMosquitto, "diagnostics", map[string]any{
			"value_template": boolTpl("mosquitto_active"), "device_class": "running", "entity_category": "diagnostic"}},
		{"binary_sensor", "tailscale_connected", "Tailscale verbunden", metricTailscale, "diagnostics", map[string]any{
			"value_template": boolTpl("tailscale_connected"), "device_class": "connectivity", "entity_category": "diagnostic"}},
		{"sensor", "apt_updates", "Verfügbare Paket-Updates", metricApt, "diagnostics", map[string]any{
			"value_template": tpl("apt_updates_pending"), "icon": "mdi:package-up",
			"state_class": "measurement", "entity_category": "diagnostic"}},
	}
}

// deviceBlock is the HA device card for the shared "energy_node" device.
// internal/energydiscovery publishes the same identifiers/name/manufacturer/
// model for the dashboard's own energy sensors; those fields are kept
// identical on both sides so HA's identifier merge cannot make the card
// flap. sw_version is deliberately owned by energydiscovery alone (the
// dashboard build version) - emitting an OS string here would clobber it
// depending on retained delivery order.
func (a *Agent) deviceBlock() map[string]any {
	return map[string]any{
		"identifiers":  []string{a.opts.NodeID},
		"name":         a.opts.NodeName,
		"manufacturer": "Raspberry Pi Foundation",
		"model":        "Raspberry Pi 1 (ARMv6)",
	}
}

func (a *Agent) discoveryTopic(component, objectID string) string {
	return a.opts.DiscoveryPrefix + "/" + component + "/" + a.opts.NodeID + "/" + objectID + "/config"
}

func (a *Agent) sourceTopic(source string) string {
	if source == "diagnostics" {
		return a.diagTopic()
	}
	return a.stateTopic()
}

func (a *Agent) DiscoveryMessages(metricEnabled func(string) bool) []mqttclient.OutboundMessage {
	if metricEnabled == nil {
		metricEnabled = func(string) bool { return true }
	}
	ents := nodeEntities()
	out := make([]mqttclient.OutboundMessage, 0, len(ents)+1)
	for _, e := range ents {
		topic := a.discoveryTopic(e.component, e.objectID)
		if !metricEnabled(e.metric) {
			out = append(out, mqttclient.OutboundMessage{Topic: topic, Payload: "", Retain: true})
			continue
		}
		payload := map[string]any{
			"name":                  e.name,
			"unique_id":             a.opts.NodeID + "_" + e.objectID,
			"state_topic":           a.sourceTopic(e.source),
			"availability_topic":    a.availabilityTopic(),
			"payload_available":     "1",
			"payload_not_available": "0",
			"device":                a.deviceBlock(),
		}
		for k, v := range e.extra {
			payload[k] = v
		}
		encoded, _ := json.Marshal(payload)
		out = append(out, mqttclient.OutboundMessage{Topic: topic, Payload: string(encoded), Retain: true})
	}
	return out
}

// StateMessages refreshes both the fast metrics and the expensive slow
// diagnostics and returns the state and diagnostics payloads in that order.
// The two tickers in cmd/dashboard use StateMessage / DiagnosticsMessage for
// the split cadence; this convenience is the "publish everything once" call
// (start-up, and the slow tick).
func (a *Agent) StateMessages(ctx context.Context, metricEnabled func(string) bool) []mqttclient.OutboundMessage {
	if metricEnabled == nil {
		metricEnabled = func(string) bool { return true }
	}
	a.Refresh(ctx, true)
	return []mqttclient.OutboundMessage{
		{Topic: a.stateTopic(), Payload: a.statePayload(metricEnabled), Retain: true},
		{Topic: a.diagTopic(), Payload: a.diagnosticsPayload(metricEnabled), Retain: true},
	}
}

// StateMessage refreshes only the cheap fast metrics and returns the single
// retained `outstation/<id>/state` message - the fast ticker's payload.
func (a *Agent) StateMessage(ctx context.Context, metricEnabled func(string) bool) mqttclient.OutboundMessage {
	if metricEnabled == nil {
		metricEnabled = func(string) bool { return true }
	}
	a.Refresh(ctx, false)
	return mqttclient.OutboundMessage{Topic: a.stateTopic(), Payload: a.statePayload(metricEnabled), Retain: true}
}

// DiagnosticsMessage refreshes the fast metrics and the expensive slow
// diagnostics and returns the single retained `outstation/<id>/diagnostics`
// message - the slow ticker's payload.
func (a *Agent) DiagnosticsMessage(ctx context.Context, metricEnabled func(string) bool) mqttclient.OutboundMessage {
	if metricEnabled == nil {
		metricEnabled = func(string) bool { return true }
	}
	a.Refresh(ctx, true)
	return mqttclient.OutboundMessage{Topic: a.diagTopic(), Payload: a.diagnosticsPayload(metricEnabled), Retain: true}
}

// statePayload renders the fast-metric JSON object from the last cached
// FastState. It does not refresh - the caller decides the cadence.
func (a *Agent) statePayload(metricEnabled func(string) bool) string {
	a.mu.RLock()
	fast := a.fast
	a.mu.RUnlock()

	state := map[string]any{}
	if metricEnabled(metricCPUTemp) {
		state["cpu_temp_c"] = fast.CPUTempC
	}
	if metricEnabled(metricCPULoad) {
		state["cpu_load_pct"] = fast.CPULoadPct
	}
	if metricEnabled(metricRAM) {
		state["ram_used_pct"] = fast.RAMUsedPct
	}
	if metricEnabled(metricDisk) {
		state["disk_used_pct"] = fast.DiskUsedPct
	}
	if metricEnabled(metricWiFi) {
		state["wifi_signal_dbm"] = fast.WiFiSignalDBm
	}
	if metricEnabled(metricLastBoot) {
		state["last_boot"] = fast.LastBoot
	}
	if metricEnabled(metricUndervoltage) {
		state["undervoltage_now"] = fast.UndervoltageNow
		state["undervoltage_occurred"] = fast.UndervoltageOccurred
	}
	if metricEnabled(metricThrottled) {
		state["throttled_now"] = fast.ThrottledNow
	}

	stateJSON, _ := json.Marshal(state)
	return string(stateJSON)
}

// diagnosticsPayload renders the slow-diagnostics JSON object from the last
// cached SlowDiagnostics. It does not refresh - the caller decides the
// cadence.
func (a *Agent) diagnosticsPayload(metricEnabled func(string) bool) string {
	a.mu.RLock()
	slow := a.slow
	a.mu.RUnlock()

	diag := map[string]any{}
	if metricEnabled(metricIP) {
		diag["ip_address"] = slow.IPAddress
	}
	if metricEnabled(metricMosquitto) {
		diag["mosquitto_active"] = slow.MosquittoActive
	}
	if metricEnabled(metricTailscale) {
		diag["tailscale_connected"] = slow.TailscaleConnected
	}
	if metricEnabled(metricApt) {
		diag["apt_updates_pending"] = slow.AptUpdatesPending
	}

	diagJSON, _ := json.Marshal(diag)
	return string(diagJSON)
}

// LegacyCleanupMessages raeumt das alte, mit Bindestrich benannte
// energy-node-Node-Geraet des geloeschten Python-Dienstes ab: leere retained
// Payloads auf jedes Discovery-Config-Topic und die zwei State-Topics. Der
// Satz object_ids ist identisch mit nodeEntities(), das alte Praefix war
// "energy-node".
func (a *Agent) LegacyCleanupMessages() []mqttclient.OutboundMessage {
	const legacy = "energy-node"
	prefix := a.opts.DiscoveryPrefix
	msgs := []mqttclient.OutboundMessage{
		{Topic: "outstation/" + legacy + "/state", Payload: "", Retain: true},
		{Topic: "outstation/" + legacy + "/diagnostics", Payload: "", Retain: true},
		{Topic: "outstation/" + legacy + "/status/online", Payload: "", Retain: true},
	}
	for _, e := range nodeEntities() {
		msgs = append(msgs, mqttclient.OutboundMessage{
			Topic: prefix + "/" + e.component + "/" + legacy + "/" + e.objectID + "/config", Payload: "", Retain: true,
		})
	}
	// Der frueheren availability-Entity ("online") ihr Config-Topic mit:
	msgs = append(msgs, mqttclient.OutboundMessage{
		Topic: prefix + "/binary_sensor/" + legacy + "/online/config", Payload: "", Retain: true,
	})
	return msgs
}
