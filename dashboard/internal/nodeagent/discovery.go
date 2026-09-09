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

func (a *Agent) deviceBlock() map[string]any {
	return map[string]any{
		"identifiers":  []string{a.opts.NodeID},
		"name":         a.opts.NodeName,
		"manufacturer": "Raspberry Pi Foundation",
		"model":        "Raspberry Pi 1 (ARMv6)",
		"sw_version":   "Raspberry Pi OS Legacy (32-bit) Lite",
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

func (a *Agent) StateMessages(ctx context.Context, metricEnabled func(string) bool) []mqttclient.OutboundMessage {
	if metricEnabled == nil {
		metricEnabled = func(string) bool { return true }
	}
	a.Refresh(ctx, true)
	a.mu.RLock()
	fast, slow := a.fast, a.slow
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

	stateJSON, _ := json.Marshal(state)
	diagJSON, _ := json.Marshal(diag)
	return []mqttclient.OutboundMessage{
		{Topic: a.stateTopic(), Payload: string(stateJSON), Retain: true},
		{Topic: a.diagTopic(), Payload: string(diagJSON), Retain: true},
	}
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
