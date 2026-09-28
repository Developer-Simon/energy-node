package diagnostics

// ruleText is what a warning says. The dashboard shows the catalog entries
// diagnostics.rule.<key>.message/.hint; Message and Hint stay in the API as
// the German fallback. texts_test.go keeps both identical.
type ruleText struct {
	Message string
	Hint    string
}

var ruleTexts = map[string]ruleText{
	"duplicate_unique_id":            {"Unique-ID ist mehreren Geräten zugeordnet.", "Unique-ID in der Discovery-Konfiguration eindeutig vergeben"},
	"discovery_invalid_json":         {"Discovery-Payload ist kein gültiges JSON.", "Discovery-Payload und Retain-Zustand prüfen"},
	"discovery_mismatch":             {"Discovery-Daten stimmen nicht mit dem normalisierten Entitätsmodell überein.", "Discovery-Payload und Registry-Zustand vergleichen"},
	"discovery_not_retained":         {"Discovery-Payload wurde nicht retained veröffentlicht.", "Discovery-Topic retained veröffentlichen"},
	"missing_topic":                  {"State-Topic fehlt.", "Discovery-Konfiguration prüfen"},
	"missing_availability":           {"Kein Availability-Topic konfiguriert.", "Availability-Topic und Online-/Offline-Payload ergänzen"},
	"missing_unit":                   {"Sensor hat keine Einheit.", "Einheit in der Discovery-Konfiguration setzen"},
	"no_state_update":                {"Seit dem letzten State-Update ist der Schwellwert überschritten.", "Bridge, MQTT-State-Topic und Polling prüfen"},
	"offline":                        {"Gerät meldet sich als offline.", "Stromversorgung, Netzwerk und Bridge prüfen"},
	"empty_state_payload":            {"State-Payload ist leer.", "Payload-Format und Value-Template prüfen"},
	"invalid_discovery_payload":      {"Discovery-Payload ist ungueltig.", "Discovery-Payload, JSON-Struktur und Availability-Definition pruefen"},
	"ignored_device_discovery_stale": {"Ignoriertes Gerät besitzt keine bekannte Discovery mehr.", "Gerät reaktivieren, falls es wieder benötigt wird, oder Discovery endgültig löschen"},
	"configured_device_missing":      {"Konfiguriertes Gerät ist in der Discovery nicht vorhanden.", "Bridge-Dienst neu starten oder Discovery erneut veröffentlichen lassen"},
}
