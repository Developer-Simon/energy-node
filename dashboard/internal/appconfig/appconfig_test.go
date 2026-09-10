package appconfig_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/appconfig"
)

func validDocument() map[string]any {
	var document map[string]any
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "services", "energy-node.config.json"))
	if err != nil {
		panic("Vorlage services/energy-node.config.json fehlt: " + err.Error())
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		panic("Vorlage ist kein gueltiges JSON: " + err.Error())
	}
	return document
}

func writeConfig(t *testing.T, document map[string]any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestLoadReadsEverySection(t *testing.T) {
	cfg, err := appconfig.Load(writeConfig(t, validDocument()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MQTT.Host == "" || cfg.MQTT.Port == 0 {
		t.Fatalf("mqtt unvollstaendig: %+v", cfg.MQTT)
	}
	if cfg.Dashboard.Port == 0 || cfg.Dashboard.BindAddress == "" {
		t.Fatalf("dashboard unvollstaendig: %+v", cfg.Dashboard)
	}
	if cfg.Paths.DevicesDir == "" || cfg.Paths.DataDir == "" {
		t.Fatalf("paths unvollstaendig: %+v", cfg.Paths)
	}
	if got, want := cfg.DevicesConfig("shelly"), filepath.Join(cfg.Paths.DevicesDir, "shelly_devices.json"); got != want {
		t.Fatalf("DevicesConfig = %q, erwartet %q", got, want)
	}
	if got, want := cfg.ShellyPresets(), filepath.Join(cfg.Paths.DevicesDir, "shelly_presets.json"); got != want {
		t.Fatalf("ShellyPresets = %q, erwartet %q", got, want)
	}
}

func TestLoadReadsDashboardNodeFields(t *testing.T) {
	cfg, err := appconfig.Load(writeConfig(t, validDocument()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Dashboard.NodeDeviceID != "energy_node" {
		t.Fatalf("NodeDeviceID = %q, want energy_node", cfg.Dashboard.NodeDeviceID)
	}
	if cfg.Dashboard.NodeDeviceName != "Energy Node" {
		t.Fatalf("NodeDeviceName = %q, want Energy Node", cfg.Dashboard.NodeDeviceName)
	}
	if cfg.Dashboard.NodePollIntervalS != 60 || cfg.Dashboard.NodeDiagnosticPollMultiplier != 10 {
		t.Fatalf("node poll = %v / %v, want 60 / 10", cfg.Dashboard.NodePollIntervalS, cfg.Dashboard.NodeDiagnosticPollMultiplier)
	}
	if got := cfg.Services["apsystems"].PollIntervalS; got != 60 {
		t.Fatalf("Services[apsystems].PollIntervalS = %v, want 60", got)
	}
}

func TestLoadRejectsMissingFile(t *testing.T) {
	_, err := appconfig.Load(filepath.Join(t.TempDir(), "fehlt.json"))
	if err == nil || !strings.Contains(err.Error(), "fehlt.json") {
		t.Fatalf("erwartet Fehler mit Dateiname, bekam %v", err)
	}
}

func TestLoadRejectsInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := appconfig.Load(path); err == nil {
		t.Fatal("erwartet Fehler bei ungueltigem JSON")
	}
}

func TestLoadRejectsWrongType(t *testing.T) {
	document := validDocument()
	document["dashboard"].(map[string]any)["port"] = "8080"
	if _, err := appconfig.Load(writeConfig(t, document)); err == nil {
		t.Fatal("erwartet Fehler bei falschem Typ")
	}
}

func TestLoadMigratesSchemaVersionOneInPlace(t *testing.T) {
	path := writeConfig(t, v1Document())

	cfg, err := appconfig.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SchemaVersion != 2 {
		t.Fatalf("SchemaVersion = %d, want 2", cfg.SchemaVersion)
	}
	if cfg.Dashboard.NodeDeviceID != "energy_node" {
		t.Fatalf("NodeDeviceID = %q, want energy_node", cfg.Dashboard.NodeDeviceID)
	}
	if cfg.Dashboard.NodePollIntervalS != 60 || cfg.Dashboard.NodeDiagnosticPollMultiplier != 10 {
		t.Fatalf("node poll = %v / %v, want 60 / 10", cfg.Dashboard.NodePollIntervalS, cfg.Dashboard.NodeDiagnosticPollMultiplier)
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Datei nach Migration nicht lesbar: %v", err)
	}
	var disk map[string]any
	if err := json.Unmarshal(onDisk, &disk); err != nil {
		t.Fatalf("migrierte Datei ist kein JSON: %v", err)
	}
	if disk["schema_version"] != float64(2) {
		t.Fatalf("Datei traegt schema_version %v, want 2", disk["schema_version"])
	}
	if _, ok := disk["node"]; ok {
		t.Fatal("node-Block steht noch in der migrierten Datei")
	}

	backup, err := os.ReadFile(path + ".v1-backup")
	if err != nil {
		t.Fatalf("Sicherung fehlt: %v", err)
	}
	var saved map[string]any
	if err := json.Unmarshal(backup, &saved); err != nil {
		t.Fatalf("Sicherung ist kein JSON: %v", err)
	}
	if saved["schema_version"] != float64(1) {
		t.Fatalf("Sicherung traegt schema_version %v, want 1", saved["schema_version"])
	}
}

func TestLoadMigratesSchemaVersionOneEvenWhenWriteBackFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root umgeht die 0555-Verzeichnisrechte, die diesen Fall ausloesen")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw, _ := json.Marshal(v1Document())
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	cfg, err := appconfig.Load(path)
	if err != nil {
		t.Fatalf("Load soll trotz fehlgeschlagenem Rueckschreiben laufen: %v", err)
	}
	if cfg.SchemaVersion != 2 || cfg.Dashboard.NodeDeviceID != "energy_node" {
		t.Fatalf("in-memory-Migration unvollstaendig: %+v", cfg.Dashboard)
	}
}

func TestLoadRejectsSchemaVersionMismatch(t *testing.T) {
	document := validDocument()
	document["schema_version"] = float64(99)
	_, err := appconfig.Load(writeConfig(t, document))
	if err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("erwartet schema_version-Fehler, bekam %v", err)
	}
}

func TestLoadRejectsMissingSection(t *testing.T) {
	document := validDocument()
	delete(document, "tinytuya")
	if _, err := appconfig.Load(writeConfig(t, document)); err == nil {
		t.Fatal("erwartet Fehler bei fehlendem Abschnitt")
	}
}

func TestMQTTPasswordReadsFileAndTrims(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "mqtt.pw")
	if err := os.WriteFile(secret, []byte("geheim\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	document := validDocument()
	document["mqtt"].(map[string]any)["password_file"] = secret
	cfg, err := appconfig.Load(writeConfig(t, document))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	password, err := cfg.MQTTPassword()
	if err != nil || password != "geheim" {
		t.Fatalf("MQTTPassword = %q, %v", password, err)
	}
}

func TestMQTTPasswordMissingFileNamesFile(t *testing.T) {
	document := validDocument()
	document["mqtt"].(map[string]any)["password_file"] = filepath.Join(t.TempDir(), "mqtt.pw")
	cfg, err := appconfig.Load(writeConfig(t, document))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := cfg.MQTTPassword(); err == nil || !strings.Contains(err.Error(), "mqtt.pw") {
		t.Fatalf("erwartet Fehler mit Dateiname, bekam %v", err)
	}
}

func TestEmptyPasswordFileFieldMeansNoPassword(t *testing.T) {
	document := validDocument()
	document["mqtt"].(map[string]any)["password_file"] = ""
	cfg, err := appconfig.Load(writeConfig(t, document))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	password, err := cfg.MQTTPassword()
	if err != nil || password != "" {
		t.Fatalf("MQTTPassword = %q, %v", password, err)
	}
}

func TestValidateSecretPaths(t *testing.T) {
	allowed := validDocument()
	allowed["mqtt"].(map[string]any)["password_file"] = "/etc/energy-node/mqtt.pw"
	allowed["dashboard"].(map[string]any)["admin_password_file"] = "/etc/energy-node-dashboard/auth.pw"
	raw, _ := json.Marshal(allowed)
	if err := appconfig.ValidateSecretPaths(raw); err != nil {
		t.Fatalf("erlaubte Pfade abgelehnt: %v", err)
	}

	for _, bad := range []string{"/etc/shadow", "/home/energynode/mqtt.pw", "/etc/energy-node/../shadow", "relativ.pw"} {
		document := validDocument()
		document["mqtt"].(map[string]any)["password_file"] = bad
		raw, _ := json.Marshal(document)
		if err := appconfig.ValidateSecretPaths(raw); err == nil {
			t.Fatalf("Pfad %q haette abgelehnt werden muessen", bad)
		}
	}

	empty := validDocument()
	empty["mqtt"].(map[string]any)["password_file"] = ""
	raw, _ = json.Marshal(empty)
	if err := appconfig.ValidateSecretPaths(raw); err != nil {
		t.Fatalf("leerer Pfad abgelehnt: %v", err)
	}
}
