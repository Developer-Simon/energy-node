// Package appconfig liest die zentrale Konfigurationsdatei
// /etc/energy-node/config.json, die die frueheren
// werkstatt_iot_dashboard.env und *.env-Dateien vollstaendig ersetzt.
//
// Die Datei ist Pflicht: fehlt sie oder ist sie ungueltig, startet das
// Dashboard nicht. Das eingebettete config.schema.json beschreibt die
// gesamte Datei - auch die Abschnitte "node" und "services", die nur die
// Python-Dienste lesen -, weil dasselbe Schema die Pruefung beim Schreiben
// ueber PUT /api/v1/system/config traegt.
package appconfig

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
)

// DefaultPath ist der feste Ort der Datei. Abweichungen nur ueber -config.
const DefaultPath = "/etc/energy-node/config.json"

// SchemaVersion ist die Version, die dieser Leser versteht. Weicht die
// Datei ab, bricht der Start ab - so faellt ein Deploy, bei dem Dashboard
// und Python-Dienste auseinanderlaufen, sofort auf.
const SchemaVersion = 1

//go:embed config.schema.json
var schemaJSON []byte

// secretPathPrefixes begrenzt, wohin ein *_file-Feld zeigen darf. Ohne
// diese Regel koennte ein Fehlgriff im Formular den Dienst auf eine
// beliebige Datei zeigen lassen. Geprueft wird nur beim Schreiben durch
// das Dashboard, nicht beim Laden - der Smoke-Test faehrt bewusst mit
// einer config.json in seinem Arbeitsverzeichnis.
var secretPathPrefixes = []string{"/etc/energy-node/", "/etc/energy-node-dashboard/"}

type Config struct {
	SchemaVersion int              `json:"schema_version"`
	MQTT          MQTTSection      `json:"mqtt"`
	Paths         PathsSection     `json:"paths"`
	Logging       LoggingSection   `json:"logging"`
	Dashboard     DashboardSection `json:"dashboard"`
	Tailscale     TailscaleSection `json:"tailscale"`
	TinyTuya      TinyTuyaSection  `json:"tinytuya"`
}

type MQTTSection struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Username     string `json:"username"`
	PasswordFile string `json:"password_file"`
}

type PathsSection struct {
	DevicesDir string `json:"devices_dir"`
	DataDir    string `json:"data_dir"`
	// ServicesVersionFile ist optional und zeigt, falls gesetzt, auf die von
	// scripts/deploy/deploy_src_to_remote.sh deployte services/VERSION-Datei. Leer bedeutet
	// "nicht konfiguriert", nicht "Fehler" - die Settings-Seite zeigt dann
	// "unbekannt" statt den Start zu verhindern.
	ServicesVersionFile string `json:"services_version_file"`
}

type LoggingSection struct {
	Level string `json:"level"`
}

type TLSSection struct {
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
}

type DashboardSection struct {
	BindAddress           string     `json:"bind_address"`
	Port                  int        `json:"port"`
	ClientID              string     `json:"client_id"`
	DeviceIdentifier      string     `json:"device_identifier"`
	LogLevel              string     `json:"log_level"`
	SweepIntervalSeconds  int        `json:"sweep_interval_seconds"`
	AdminUsername         string     `json:"admin_username"`
	AdminPasswordFile     string     `json:"admin_password_file"`
	TLS                   TLSSection `json:"tls"`
	SystemActionHelper    string     `json:"system_action_helper"`
	MosquittoBridgeTarget string     `json:"mosquitto_bridge_target"`
}

type TailscaleSection struct {
	Bin            string `json:"bin"`
	StatusTimeoutS int    `json:"status_timeout_s"`
}

type TinyTuyaSection struct {
	ProbePython   string `json:"probe_python"`
	ProbeScript   string `json:"probe_script"`
	ProbeTimeoutS int    `json:"probe_timeout_s"`
}

// Schema liefert das eingebettete Schema fuer die Pruefung beim Schreiben.
func Schema() []byte { return schemaJSON }

// Load liest und validiert die Datei oder liefert einen Fehler, der Datei
// und Ursache nennt.
func Load(path string) (Config, error) {
	if path == "" {
		path = DefaultPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("%s: Konfigurationsdatei nicht lesbar (anderer Pfad ueber -config): %w", path, err)
	}
	if err := config.ValidateDocument(data, schemaJSON); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("%s: ungueltiges JSON: %w", path, err)
	}
	if cfg.SchemaVersion != SchemaVersion {
		return Config{}, fmt.Errorf("%s: schema_version %d passt nicht zu erwarteter Version %d", path, cfg.SchemaVersion, SchemaVersion)
	}
	return cfg, nil
}

// DevicesConfig loest die Konvention devices_dir/<name>_devices.json auf.
func (c Config) DevicesConfig(name string) string {
	return filepath.Join(c.Paths.DevicesDir, name+"_devices.json")
}

// ShellyPresets loest devices_dir/shelly_presets.json auf.
func (c Config) ShellyPresets() string {
	return filepath.Join(c.Paths.DevicesDir, "shelly_presets.json")
}

// MQTTPassword liest das Broker-Passwort aus der referenzierten Datei.
func (c Config) MQTTPassword() (string, error) { return readSecret(c.MQTT.PasswordFile) }

// AdminPassword liest das Admin-Passwort aus der referenzierten Datei.
func (c Config) AdminPassword() (string, error) { return readSecret(c.Dashboard.AdminPasswordFile) }

// readSecret liefert "" fuer ein leeres Feld (kein Passwort gesetzt) und
// einen Fehler mit Dateiname, wenn ein gesetzter Pfad nicht lesbar ist -
// nie stillschweigend ein leeres Passwort.
func readSecret(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s: Passwortdatei nicht lesbar: %w", path, err)
	}
	return strings.TrimRight(string(data), " \t\r\n"), nil
}

// ValidateSecretPaths prueft alle *_file-Felder eines zu schreibenden
// Dokuments gegen die Allowlist.
func ValidateSecretPaths(data []byte) error {
	var document struct {
		MQTT struct {
			PasswordFile string `json:"password_file"`
		} `json:"mqtt"`
		Dashboard struct {
			AdminPasswordFile string     `json:"admin_password_file"`
			TLS               TLSSection `json:"tls"`
		} `json:"dashboard"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("ungueltiges JSON: %w", err)
	}
	fields := []struct {
		name  string
		value string
	}{
		{"mqtt.password_file", document.MQTT.PasswordFile},
		{"dashboard.admin_password_file", document.Dashboard.AdminPasswordFile},
		{"dashboard.tls.cert_file", document.Dashboard.TLS.CertFile},
		{"dashboard.tls.key_file", document.Dashboard.TLS.KeyFile},
	}
	for _, field := range fields {
		if err := checkSecretPath(field.name, field.value); err != nil {
			return err
		}
	}
	return nil
}

func checkSecretPath(name, value string) error {
	if value == "" {
		return nil
	}
	if !filepath.IsAbs(value) {
		return fmt.Errorf("%s: erwartet absoluten Pfad, gefunden %q", name, value)
	}
	cleaned := filepath.Clean(value)
	for _, prefix := range secretPathPrefixes {
		if strings.HasPrefix(cleaned, prefix) {
			return nil
		}
	}
	return fmt.Errorf("%s: %q liegt ausserhalb von %s", name, value, strings.Join(secretPathPrefixes, " und "))
}
