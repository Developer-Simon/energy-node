package appconfig_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/appconfig"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
)

// v1Document nimmt die v2-Vorlage und dreht sie auf schema_version 1
// zurueck: der flache dashboard.node_*-Block wird durch den frueheren
// Top-Level-node-Block ersetzt (inkl. des inzwischen entfallenen
// managed_bridges und des alten Bindestrich-device_id).
func v1Document() map[string]any {
	doc := validDocument()
	doc["schema_version"] = float64(1)

	dash := doc["dashboard"].(map[string]any)
	delete(dash, "node_device_id")
	delete(dash, "node_device_name")
	delete(dash, "node_poll_interval_s")
	delete(dash, "node_diagnostic_poll_multiplier")

	doc["node"] = map[string]any{
		"device_id":                  "energy-node",
		"device_name":                "Energy Node",
		"managed_bridges":            []any{"apsystems", "tuya", "battery_soc", "shelly"},
		"poll_interval_s":            float64(60),
		"diagnostic_poll_multiplier": float64(10),
	}
	return doc
}

func migrate(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, _, err := appconfig.MigrateV1toV2(raw)
	if err != nil {
		t.Fatalf("MigrateV1toV2: %v", err)
	}
	var migrated map[string]any
	if err := json.Unmarshal(out, &migrated); err != nil {
		t.Fatalf("Ergebnis ist kein JSON: %v", err)
	}
	return migrated
}

func TestMigrateV1toV2MovesNodeFieldsIntoDashboard(t *testing.T) {
	got := migrate(t, v1Document())

	if got["schema_version"] != float64(2) {
		t.Fatalf("schema_version = %v, want 2", got["schema_version"])
	}
	if _, ok := got["node"]; ok {
		t.Fatalf("node-Block noch vorhanden: %v", got["node"])
	}
	dash := got["dashboard"].(map[string]any)
	if dash["node_device_name"] != "Energy Node" {
		t.Fatalf("node_device_name = %v", dash["node_device_name"])
	}
	if dash["node_poll_interval_s"] != float64(60) {
		t.Fatalf("node_poll_interval_s = %v", dash["node_poll_interval_s"])
	}
	if dash["node_diagnostic_poll_multiplier"] != float64(10) {
		t.Fatalf("node_diagnostic_poll_multiplier = %v", dash["node_diagnostic_poll_multiplier"])
	}
}

func TestMigrateV1toV2NormalisesHyphenatedNodeID(t *testing.T) {
	got := migrate(t, v1Document())
	if id := got["dashboard"].(map[string]any)["node_device_id"]; id != "energy_node" {
		t.Fatalf("node_device_id = %v, want energy_node", id)
	}
}

func TestMigrateV1toV2KeepsCustomNodeIDAndWarns(t *testing.T) {
	doc := v1Document()
	doc["node"].(map[string]any)["device_id"] = "werkstatt-pi"

	raw, _ := json.Marshal(doc)
	out, warnings, err := appconfig.MigrateV1toV2(raw)
	if err != nil {
		t.Fatalf("MigrateV1toV2: %v", err)
	}
	var migrated map[string]any
	if err := json.Unmarshal(out, &migrated); err != nil {
		t.Fatal(err)
	}
	if id := migrated["dashboard"].(map[string]any)["node_device_id"]; id != "werkstatt-pi" {
		t.Fatalf("node_device_id = %v, want werkstatt-pi (unveraendert)", id)
	}
	if !containsSubstring(warnings, "werkstatt-pi") {
		t.Fatalf("erwartet Warnung zum Custom-Wert, bekam %v", warnings)
	}
}

func TestMigrateV1toV2DropsManagedBridges(t *testing.T) {
	got := migrate(t, v1Document())
	if _, ok := got["dashboard"].(map[string]any)["node_managed_bridges"]; ok {
		t.Fatal("managed_bridges nach dashboard.* durchgereicht")
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "managed_bridges") {
		t.Fatalf("managed_bridges noch im Dokument: %s", raw)
	}
}

func TestMigrateV1toV2FillsMissingNodeFieldsWithDefaults(t *testing.T) {
	doc := v1Document()
	doc["node"] = map[string]any{"device_id": "energy-node"}

	got := migrate(t, doc)
	dash := got["dashboard"].(map[string]any)
	if dash["node_device_id"] != "energy_node" {
		t.Fatalf("node_device_id = %v", dash["node_device_id"])
	}
	if dash["node_device_name"] != "Energy Node" {
		t.Fatalf("node_device_name default = %v, want Energy Node", dash["node_device_name"])
	}
	if dash["node_poll_interval_s"] != float64(60) {
		t.Fatalf("node_poll_interval_s default = %v, want 60", dash["node_poll_interval_s"])
	}
	if dash["node_diagnostic_poll_multiplier"] != float64(10) {
		t.Fatalf("node_diagnostic_poll_multiplier default = %v, want 10", dash["node_diagnostic_poll_multiplier"])
	}
}

func TestMigrateV1toV2PrefersExistingDashboardNodeFields(t *testing.T) {
	doc := v1Document()
	dash := doc["dashboard"].(map[string]any)
	dash["node_device_id"] = "energy_node"
	dash["node_poll_interval_s"] = float64(90)
	doc["node"].(map[string]any)["poll_interval_s"] = float64(60)

	raw, _ := json.Marshal(doc)
	out, warnings, err := appconfig.MigrateV1toV2(raw)
	if err != nil {
		t.Fatalf("MigrateV1toV2: %v", err)
	}
	var migrated map[string]any
	_ = json.Unmarshal(out, &migrated)
	if got := migrated["dashboard"].(map[string]any)["node_poll_interval_s"]; got != float64(90) {
		t.Fatalf("node_poll_interval_s = %v, want 90 (dashboard.node_* gewinnt)", got)
	}
	if !containsSubstring(warnings, "poll_interval") {
		t.Fatalf("erwartet Divergenz-Warnung, bekam %v", warnings)
	}
}

func TestMigrateV1toV2PreservesOtherSections(t *testing.T) {
	doc := v1Document()
	before, _ := json.Marshal(doc["services"])

	got := migrate(t, doc)
	after, _ := json.Marshal(got["services"])
	if string(before) != string(after) {
		t.Fatalf("services veraendert:\n vor:  %s\n nach: %s", before, after)
	}
	if !json.Valid(mustMarshal(t, got["mqtt"])) || got["mqtt"].(map[string]any)["host"] != "127.0.0.1" {
		t.Fatalf("mqtt veraendert: %v", got["mqtt"])
	}
}

// earlyV1Document ist eine v1-Datei aus der Zeit vor PR #10: die
// Service-Eintraege tragen noch device_id statt service_id.
func earlyV1Document() map[string]any {
	doc := v1Document()
	services := doc["services"].(map[string]any)
	for name, raw := range services {
		entry := raw.(map[string]any)
		entry["device_id"] = entry["service_id"]
		delete(entry, "service_id")
		services[name] = entry
	}
	return doc
}

func TestMigrateV1toV2RenamesServiceDeviceIDToServiceID(t *testing.T) {
	got := migrate(t, earlyV1Document())

	services, ok := got["services"].(map[string]any)
	if !ok {
		t.Fatalf("services fehlt: %v", got["services"])
	}
	for name, raw := range services {
		entry := raw.(map[string]any)
		if _, ok := entry["device_id"]; ok {
			t.Fatalf("services.%s.device_id noch vorhanden: %v", name, entry)
		}
		if entry["service_id"] != name {
			t.Fatalf("services.%s.service_id = %v, want %q", name, entry["service_id"], name)
		}
	}
}

func TestMigrateV1toV2EarlyV1OutputValidatesAgainstSchema(t *testing.T) {
	raw, _ := json.Marshal(earlyV1Document())
	out, _, err := appconfig.MigrateV1toV2(raw)
	if err != nil {
		t.Fatalf("MigrateV1toV2: %v", err)
	}
	if err := config.ValidateDocument(out, appconfig.Schema()); err != nil {
		t.Fatalf("migriertes early-v1-Dokument verletzt das v2-Schema: %v", err)
	}
}

func TestMigrateV1toV2RejectsNonV1(t *testing.T) {
	raw, _ := json.Marshal(validDocument())
	if _, _, err := appconfig.MigrateV1toV2(raw); err == nil {
		t.Fatal("erwartet Fehler fuer schema_version 2")
	}
}

func TestMigrateV1toV2OutputValidatesAgainstSchema(t *testing.T) {
	raw, _ := json.Marshal(v1Document())
	out, _, err := appconfig.MigrateV1toV2(raw)
	if err != nil {
		t.Fatalf("MigrateV1toV2: %v", err)
	}
	if err := config.ValidateDocument(out, appconfig.Schema()); err != nil {
		t.Fatalf("migriertes Dokument verletzt das v2-Schema: %v", err)
	}
}

func containsSubstring(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}
