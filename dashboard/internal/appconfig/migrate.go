package appconfig

import (
	"encoding/json"
	"fmt"
)

// nodeFieldDefaults sind die Werte, die ein v1-node-Block laut altem Schema
// immer trug; sie greifen nur, wenn ein Feld beim Migrieren fehlt.
var nodeFieldDefaults = struct {
	deviceID       string
	deviceName     string
	pollIntervalS  float64
	pollMultiplier float64
}{
	deviceID:       "energy_node",
	deviceName:     "Energy Node",
	pollIntervalS:  60,
	pollMultiplier: 10,
}

// MigrateV1toV2 wandelt ein config.json-Dokument mit schema_version 1 in
// das v2-Layout um: der Top-Level-"node"-Block wird aufgeloest, seine vier
// ueberlebenden Felder wandern flach nach dashboard.node_*, und
// node.managed_bridges entfaellt (die Liste leitet sich jetzt aus
// "services" ab). Alle uebrigen Abschnitte bleiben unveraendert.
//
// Der Rueckgabewert ist das neue Dokument mit 2-Space-Einrueckung; die
// Schluesselreihenfolge folgt danach der von encoding/json (alphabetisch).
// warnings sammelt Auffaelligkeiten, die den Start nicht verhindern:
// ein von "energy_node" abweichender node_device_id-Wert und ein
// Widerspruch zwischen einem bereits vorhandenen dashboard.node_*-Feld
// und dem gleichnamigen node-Feld (dann gewinnt dashboard.node_*).
//
// Ein Dokument, das nicht schema_version 1 traegt, ist ein Fehler - der
// Aufrufer ruft die Funktion nur fuer eine erkannte v1-Datei auf.
func MigrateV1toV2(data []byte) (migrated []byte, warnings []string, err error) {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("ungueltiges JSON: %w", err)
	}
	if v, _ := doc["schema_version"].(float64); v != 1 {
		return nil, nil, fmt.Errorf("MigrateV1toV2 erwartet schema_version 1, gefunden %v", doc["schema_version"])
	}

	dashboard, ok := doc["dashboard"].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("dashboard-Block fehlt oder ist kein Objekt")
	}
	node, _ := doc["node"].(map[string]any)

	// resolve waehlt fuer ein Feld den Zielwert: ein bereits vorhandenes
	// dashboard.node_*-Feld gewinnt vor dem node-Feld, das node-Feld vor
	// dem Default. Weichen dashboard.node_* und node voneinander ab, wird
	// das protokolliert.
	resolve := func(dashKey, nodeKey string, def any) any {
		dashVal, dashSet := dashboard[dashKey]
		nodeVal, nodeSet := node[nodeKey]
		switch {
		case dashSet && nodeSet && !jsonEqual(dashVal, nodeVal):
			warnings = append(warnings, fmt.Sprintf(
				"%s (%v) und node.%s (%v) weichen ab - %s gewinnt", dashKey, dashVal, nodeKey, nodeVal, dashKey))
			return dashVal
		case dashSet:
			return dashVal
		case nodeSet:
			return nodeVal
		default:
			return def
		}
	}

	deviceID, _ := resolve("node_device_id", "device_id", nodeFieldDefaults.deviceID).(string)
	switch deviceID {
	case "", "energy-node":
		deviceID = "energy_node"
	case "energy_node":
		// schon normalisiert
	default:
		warnings = append(warnings, fmt.Sprintf(
			"node_device_id %q ist kein %q - der Wert bleibt unveraendert, das Dashboard lehnt den Start damit ab", deviceID, "energy_node"))
	}

	dashboard["node_device_id"] = deviceID
	dashboard["node_device_name"] = resolve("node_device_name", "device_name", nodeFieldDefaults.deviceName)
	dashboard["node_poll_interval_s"] = resolve("node_poll_interval_s", "poll_interval_s", nodeFieldDefaults.pollIntervalS)
	dashboard["node_diagnostic_poll_multiplier"] = resolve(
		"node_diagnostic_poll_multiplier", "diagnostic_poll_multiplier", nodeFieldDefaults.pollMultiplier)

	// PR #10 hat das Service-Feld device_id in service_id umbenannt, noch
	// unter schema_version 1. Eine Datei aus der Zeit davor traegt in jedem
	// services.<name>-Eintrag device_id; der Wert ist immer der Servicename.
	if services, ok := doc["services"].(map[string]any); ok {
		for _, raw := range services {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			legacyID, hasLegacy := entry["device_id"]
			if !hasLegacy {
				continue
			}
			delete(entry, "device_id")
			if _, hasNew := entry["service_id"]; !hasNew {
				entry["service_id"] = legacyID
			}
		}
	}

	delete(dashboard, "node_managed_bridges")
	delete(doc, "node")
	doc["schema_version"] = float64(2)

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("Ergebnis nicht serialisierbar: %w", err)
	}
	return append(out, '\n'), warnings, nil
}

// jsonEqual vergleicht zwei aus JSON dekodierte Werte strukturell.
func jsonEqual(a, b any) bool {
	ra, err := json.Marshal(a)
	if err != nil {
		return false
	}
	rb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(ra) == string(rb)
}
