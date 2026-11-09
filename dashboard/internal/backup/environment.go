package backup

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/Developer-Simon/energy-node-dashboard/internal/appconfig"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
)

// EnvRow ist eine Zeile im Schritt Umgebung. Key nennt die Auswahl, Here und
// Archive zeigen die Werte beider Seiten. Off ist der Katalogschlüssel einer
// abgeschalteten Zeile ohne Auswahl, dann bleiben Here und Archive leer.
type EnvRow struct {
	Key, Here, Archive string
	Off                string
}

// Schlüssel der Umgebungszeilen. Die Oberfläche ordnet ihnen die Texte zu.
const (
	envBroker    = "broker"
	envPort      = "port"
	envBridge    = "bridge"
	envTailscale = "tailscale"
)

// envChoice ordnet jeder auswählbaren Zeile die Pfade zu, die sie vom Archiv übernimmt.
var envChoice = map[string][]string{
	envBroker: {"mqtt.host", "mqtt.port"},
	envPort:   {"dashboard.port"},
}

// envFromTarget bleibt immer vom Ziel, ohne Zeile in der Umgebung.
var envFromTarget = []string{
	"paths",
	"dashboard.bind_address",
	"dashboard.tls",
	"dashboard.admin_password_file",
	"dashboard.mosquitto_bridge_target",
	"tailscale",
	"mqtt.username",
	"mqtt.password_file",
	// Programme und Skripte, die der Dashboard-Prozess startet. Ein Archiv
	// darf dort keinen Pfad setzen.
	"tinytuya.probe_python",
	"tinytuya.probe_script",
	"dashboard.system_action_helper",
}

// EnvironmentRows liefert die Umgebungswerte, die sich zwischen Archiv und Ziel
// unterscheiden. Gleiche Werte ergeben keine Zeile. archivedBridge ist die
// bridge.json aus dem Archiv, nil oder leer heißt keine Bridge.
func EnvironmentRows(archiveConfig, hereConfig []byte, archivedBridge []byte) ([]EnvRow, error) {
	archive, err := loadEnvConfig(archiveConfig)
	if err != nil {
		return nil, fmt.Errorf("Konfiguration im Archiv: %w", err)
	}
	here, err := loadEnvConfig(hereConfig)
	if err != nil {
		return nil, fmt.Errorf("Konfiguration dieses Geräts: %w", err)
	}

	var rows []EnvRow
	if archive.MQTT.Host != here.MQTT.Host || archive.MQTT.Port != here.MQTT.Port {
		rows = append(rows, EnvRow{
			Key:     envBroker,
			Here:    net.JoinHostPort(here.MQTT.Host, strconv.Itoa(here.MQTT.Port)),
			Archive: net.JoinHostPort(archive.MQTT.Host, strconv.Itoa(archive.MQTT.Port)),
		})
	}
	if archive.Dashboard.Port != here.Dashboard.Port {
		rows = append(rows, EnvRow{
			Key:     envPort,
			Here:    strconv.Itoa(here.Dashboard.Port),
			Archive: strconv.Itoa(archive.Dashboard.Port),
		})
	}
	if here.Dashboard.MosquittoBridgeTarget == "" && bridgeActive(archivedBridge) {
		rows = append(rows, EnvRow{Key: envBridge, Off: envBridge})
	}
	if archive.Tailscale != here.Tailscale {
		rows = append(rows, EnvRow{Key: envTailscale, Off: envTailscale})
	}
	return rows, nil
}

// MergeConfig baut die Konfiguration für dieses Gerät. Grundlage ist das Archiv.
// Die Werte aus envFromTarget kommen immer vom Ziel. Die Werte einer Auswahl
// kommen vom Ziel, außer useArchive wählt sie für das Archiv. Das Ergebnis
// prüft das Schema aus appconfig.
func MergeConfig(archiveConfig, hereConfig []byte, useArchive map[string]bool) ([]byte, error) {
	archiveData, err := migrateConfig(archiveConfig)
	if err != nil {
		return nil, fmt.Errorf("Konfiguration im Archiv: %w", err)
	}
	hereData, err := migrateConfig(hereConfig)
	if err != nil {
		return nil, fmt.Errorf("Konfiguration dieses Geräts: %w", err)
	}
	var merged, here map[string]any
	if err := json.Unmarshal(archiveData, &merged); err != nil {
		return nil, fmt.Errorf("Konfiguration im Archiv: ungültiges JSON: %w", err)
	}
	if err := json.Unmarshal(hereData, &here); err != nil {
		return nil, fmt.Errorf("Konfiguration dieses Geräts: ungültiges JSON: %w", err)
	}

	fromHere := append([]string(nil), envFromTarget...)
	for key, paths := range envChoice {
		if !useArchive[key] {
			fromHere = append(fromHere, paths...)
		}
	}
	for _, p := range fromHere {
		keepFrom(merged, here, strings.Split(p, "."))
	}

	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, err
	}
	out = append(out, '\n')
	if err := config.ValidateDocument(out, appconfig.Schema()); err != nil {
		return nil, fmt.Errorf("Die zusammengeführte Konfiguration ist ungültig: %w", err)
	}
	return out, nil
}

// keepFrom übernimmt den Wert am Pfad keys aus src nach dst. Fehlt er in src,
// fehlt er danach auch in dst.
func keepFrom(dst, src map[string]any, keys []string) {
	k := keys[0]
	if len(keys) == 1 {
		if v, ok := src[k]; ok {
			dst[k] = v
		} else {
			delete(dst, k)
		}
		return
	}
	sub, ok := src[k].(map[string]any)
	if !ok {
		delete(dst, k)
		return
	}
	dsub, ok := dst[k].(map[string]any)
	if !ok {
		dsub = map[string]any{}
		dst[k] = dsub
	}
	keepFrom(dsub, sub, keys[1:])
}

// migrateConfig hebt ein schema_version-1-Dokument wie appconfig.Load auf Version 2.
func migrateConfig(data []byte) ([]byte, error) {
	if !appconfig.IsSchemaVersionOne(data) {
		return data, nil
	}
	migrated, _, err := appconfig.MigrateV1toV2(data)
	return migrated, err
}

// loadEnvConfig liest eine Konfiguration nach Migration in appconfig.Config.
func loadEnvConfig(data []byte) (appconfig.Config, error) {
	var cfg appconfig.Config
	migrated, err := migrateConfig(data)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(migrated, &cfg); err != nil {
		return cfg, fmt.Errorf("ungültiges JSON: %w", err)
	}
	return cfg, nil
}

// bridgeActive meldet, ob bridge.json eine eingeschaltete Verbindung hat.
func bridgeActive(data []byte) bool {
	var bridge struct {
		Connections []struct {
			Enabled bool `json:"enabled"`
		} `json:"connections"`
	}
	if json.Unmarshal(data, &bridge) != nil {
		return false
	}
	for _, c := range bridge.Connections {
		if c.Enabled {
			return true
		}
	}
	return false
}
