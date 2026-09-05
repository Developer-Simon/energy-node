// Package energydiscovery baut die Home-Assistant-MQTT-Discovery-Configs,
// mit denen sich das Dashboard selbst als ein HA-Gerät ("Energy
// Node") mit read-only Energie-Sensoren anbietet. Alle Sensoren lesen
// aus demselben retained State-Topic (StateTopic), das cmd/dashboard alle
// 10 s als {"at":…,"balance":energy.Balance,…} veröffentlicht; jeder Sensor
// wählt sein Feld per value_template. Siehe
// dashboard/mqtt-topics-und-discovery-format.md, Abschnitt "Dashboard als
// Discovery-Publisher".
package energydiscovery

import "encoding/json"

const (
	// DeviceID ist die device_id-Ebene des 3-Ebenen-Discovery-Topics und
	// zugleich der unique_id-Präfix.
	DeviceID = "dashboard_energy"
	// DeviceIdentifier ist der Eintrag in device.identifiers, an dem Home
	// Assistant die Entitäten zu einem Gerät zusammenfasst.
	DeviceIdentifier = "energy-node-dashboard-energy"

	deviceName         = "Energy Node"
	deviceManufacturer = "Energy Node"
	deviceModel        = "Dashboard Energy"

	// StateTopic ist das geteilte, retained JSON-Topic (identisch mit dem
	// Broadcast in cmd/dashboard/main.go).
	StateTopic = "outstation/dashboard/energy/balance"
	// AvailabilityTopic trägt die retained LWT/Birth "0"/"1" des
	// Dashboards; jede Config verweist mit availability_topic hierauf.
	AvailabilityTopic = "outstation/dashboard/status/online"
)

// Message ist eine zu veröffentlichende Discovery-Nachricht. Ein leeres
// Payload ist die HA-Konvention, um eine zuvor retained angekündigte
// Entität wieder zu entfernen.
type Message struct {
	Topic   string
	Payload []byte
}

// sensor beschreibt eine Sensor-Entität. field ist der Schlüssel unterhalb
// von value_json.balance im StateTopic-JSON. Das value_template nutzt die
// Subscript-Form value_json.balance['<feld>'] statt der zweistufigen
// Punkt-Form: Home Assistant versteht beide, aber der value_template-Parser
// des Dashboards (internal/registry) deckt nur genau eine Punktebene plus
// optionalem ['key'] ab, sodass die Punkt-Form die eigene Energie-Kachel
// unaufgelöst ließe.
type sensor struct {
	objectID    string
	name        string
	field       string
	deviceClass string
	unit        string
}

// sensors ist die feste Liste der angebotenen Entitäten, in
// Veröffentlichungsreihenfolge.
var sensors = []sensor{
	{"pv_power", "PV-Leistung", "pv", "power", "W"},
	{"grid_import", "Netzbezug", "grid_import", "power", "W"},
	{"grid_export", "Netzeinspeisung", "grid_export", "power", "W"},
	{"battery_charge", "Batterie-Ladeleistung", "battery_charge", "power", "W"},
	{"battery_discharge", "Batterie-Entladeleistung", "battery_discharge", "power", "W"},
	{"house_load", "Hauslast", "load_total", "power", "W"},
	{"battery_soc", "Batterie-Ladezustand", "battery_soc", "battery", "%"},
}

// DiscoveryTopic baut das 3-Ebenen-Discovery-Config-Topic für eine
// object_id.
func DiscoveryTopic(prefix, objectID string) string {
	return prefix + "/sensor/" + DeviceID + "/" + objectID + "/config"
}

func deviceBlock(swVersion string) map[string]any {
	return map[string]any{
		"identifiers":  []string{DeviceIdentifier},
		"name":         deviceName,
		"manufacturer": deviceManufacturer,
		"model":        deviceModel,
		"sw_version":   swVersion,
	}
}

// Configs liefert die sieben retained Discovery-Config-Nachrichten. prefix
// ist der HA-Discovery-Präfix (z. B. "homeassistant"), swVersion die
// Dashboard-Build-Version. Bei enabled == false trägt jede Nachricht ein
// leeres Payload (Removal), die Topics bleiben unverändert.
func Configs(prefix, swVersion string, enabled bool) []Message {
	out := make([]Message, 0, len(sensors))
	device := deviceBlock(swVersion)
	for _, s := range sensors {
		msg := Message{Topic: DiscoveryTopic(prefix, s.objectID)}
		if enabled {
			payload := map[string]any{
				"name":                  s.name,
				"unique_id":             DeviceID + "_" + s.objectID,
				"device":                device,
				"state_topic":           StateTopic,
				"value_template":        "{{ value_json.balance['" + s.field + "'] }}",
				"device_class":          s.deviceClass,
				"unit_of_measurement":   s.unit,
				"state_class":           "measurement",
				"availability_topic":    AvailabilityTopic,
				"payload_available":     "1",
				"payload_not_available": "0",
			}
			encoded, err := json.Marshal(payload)
			if err != nil {
				// map[string]any mit reinen JSON-Skalaren/Slices kann nicht
				// fehlschlagen; defensiv trotzdem als Removal behandeln.
				encoded = nil
			}
			msg.Payload = encoded
		}
		out = append(out, msg)
	}
	return out
}
