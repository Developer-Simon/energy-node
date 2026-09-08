package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/appconfig"
	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
)

// restartRequiredFields listet die JSON-Pfade, deren Aenderung ein
// Neustart erfordert: alles, was die Identitaet einer Verbindung oder
// eines Topics bestimmt. Alle uebrigen Felder uebernehmen die Dienste
// ueber outstation/<id>/config/reload ohne Neustart.
var restartRequiredFields = []string{
	"mqtt",
	"paths",
	"node.device_id",
	"services.apsystems.service_id",
	"services.battery_soc.service_id",
	"services.shelly.service_id",
	"services.trucki.service_id",
	"services.tuya.service_id",
	"services.automation.service_id",
	"dashboard.port",
	"dashboard.bind_address",
	"dashboard.tls",
	"dashboard.admin_username",
	"dashboard.admin_password_file",
}

// ConfigReloader schickt einem Dienst die Aufforderung, seine
// Konfiguration neu zu laden, und meldet das Ergebnis je Dienst.
type ConfigReloader interface {
	ReloadService(deviceID string) error
}

func handleSystemConfig(path, dataDir string, authManager *auth.Manager, reloader ConfigReloader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			data, err := os.ReadFile(path)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "config_unreadable", err.Error())
				return
			}
			var document json.RawMessage = data
			revisions, _ := systemConfigRevisions(dataDir)
			writeJSONStatus(w, http.StatusOK, map[string]any{"config": document, "revisions": revisions})
		case http.MethodPut:
			if !requireHTTPS(w, r) {
				return
			}
			if !requireRole(w, r, authManager, "system_actions", "forbidden", "Konfiguration darf nur mit Systemrechten geaendert werden") {
				return
			}
			if !requireCSRF(w, r, authManager) {
				return
			}
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
				return
			}
			if err := config.ValidateDocument(body, appconfig.Schema()); err != nil {
				writeError(w, http.StatusBadRequest, "schema_violation", err.Error())
				return
			}
			if err := appconfig.ValidateSecretPaths(body); err != nil {
				writeError(w, http.StatusBadRequest, "path_not_allowed", err.Error())
				return
			}
			previous, _ := os.ReadFile(path)
			if err := writeSystemConfigRevision(dataDir, previous); err != nil {
				writeError(w, http.StatusInternalServerError, "revision_failed", err.Error())
				return
			}
			if err := config.AtomicWrite(path, body, 0o664); err != nil {
				writeError(w, http.StatusInternalServerError, "config_not_writable", err.Error())
				return
			}
			restart := changedRestartFields(previous, body)
			reloaded := map[string]string{}
			if reloader != nil && len(restart) == 0 {
				for _, deviceID := range serviceDeviceIDs(body) {
					if err := reloader.ReloadService(deviceID); err != nil {
						reloaded[deviceID] = err.Error()
					} else {
						reloaded[deviceID] = "ok"
					}
				}
			}
			sort.Strings(restart)
			writeJSONStatus(w, http.StatusOK, map[string]any{
				"config":           json.RawMessage(body),
				"restart_required": restart,
				"reloaded":         reloaded,
			})
		default:
			w.Header().Set("Allow", "GET, PUT")
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Nur GET und PUT")
		}
	}
}

// handleSystemConfigSchema liefert das eingebettete config.schema.json, aus
// dem die Einstellungsseite ihr Formular fuer /etc/energy-node/config.json
// baut. Wie GET /api/v1/system/config nicht rollengeschuetzt - das Schema
// steht ohnehin im Quellcode.
func handleSystemConfigSchema() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Nur GET")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(appconfig.Schema())
	}
}

// systemConfigRevisions liefert die Liste der Revisions-Zeitstempel
// aus dem revisions/system-config Verzeichnis.
func systemConfigRevisions(dataDir string) ([]string, error) {
	revDir := filepath.Join(dataDir, "revisions", "system-config")
	entries, err := os.ReadDir(revDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	var revisions []string
	for _, entry := range entries {
		if !entry.IsDir() {
			revisions = append(revisions, entry.Name())
		}
	}
	sort.Strings(revisions)
	return revisions, nil
}

// writeSystemConfigRevision legt <dataDir>/revisions/system-config/<UTC-Zeitstempel>.json
// an und begrenzt auf 20 Revisionen. Nutzt die gleiche Logik wie
// config.Manager.writeRevisionLocked/pruneRevisionsLocked (config.go:398-424).
func writeSystemConfigRevision(dataDir string, data []byte) error {
	revDir := filepath.Join(dataDir, "revisions", "system-config")
	if err := os.MkdirAll(revDir, 0o755); err != nil {
		return err
	}

	// Zeitstempel im UTC-Format als Dateiname
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	revPath := filepath.Join(revDir, timestamp+".json")

	if err := config.AtomicWrite(revPath, data, 0o664); err != nil {
		return err
	}

	// Prune old revisions, keep max 20
	entries, err := os.ReadDir(revDir)
	if err != nil {
		return err
	}
	if len(entries) > 20 {
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() < entries[j].Name()
		})
		for i := 0; i < len(entries)-20; i++ {
			_ = os.Remove(filepath.Join(revDir, entries[i].Name()))
		}
	}

	return nil
}

// changedRestartFields vergleicht zwei JSON-Dokumente und liefert die
// Pfade aus restartRequiredFields, die sich unterscheiden. Ein Pfad wie
// "mqtt" vergleicht den ganzen Abschnitt, "node.device_id" nur das Blatt.
func changedRestartFields(previous, next []byte) []string {
	var prevDoc, nextDoc map[string]any
	if err := json.Unmarshal(previous, &prevDoc); err != nil {
		return nil
	}
	if err := json.Unmarshal(next, &nextDoc); err != nil {
		return nil
	}

	var changed []string
	for _, path := range restartRequiredFields {
		prevVal := getPath(prevDoc, path)
		nextVal := getPath(nextDoc, path)
		if !reflect.DeepEqual(prevVal, nextVal) {
			changed = append(changed, path)
		}
	}
	return changed
}

// serviceDeviceIDs liefert alle Dienst-/Node-IDs aus dem JSON-Dokument.
// Das sind die service_id-Werte aller Eintraege unter "services" plus "node.device_id".
func serviceDeviceIDs(data []byte) []string {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}

	var ids []string

	// node.device_id
	if nodeVal, ok := doc["node"]; ok {
		if node, ok := nodeVal.(map[string]any); ok {
			if id, ok := node["device_id"].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
	}

	// services.*.service_id
	if servicesVal, ok := doc["services"]; ok {
		if services, ok := servicesVal.(map[string]any); ok {
			for _, serviceVal := range services {
				if service, ok := serviceVal.(map[string]any); ok {
					if id, ok := service["service_id"].(string); ok && id != "" {
						ids = append(ids, id)
					}
				}
			}
		}
	}

	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	// Duplikate entfernen.
	j := 0
	for i := 1; i < len(ids); i++ {
		if ids[i] != ids[j] {
			j++
			ids[j] = ids[i]
		}
	}
	return ids[:j+1]
}

// getPath liest einen Wert aus einem verschachtelten Dokument anhand eines
// Pfads in Punktschreibweise, z.B. "services.shelly.service_id" oder "mqtt".
func getPath(doc map[string]any, path string) any {
	if doc == nil {
		return nil
	}

	var parts []string
	var current string
	for _, ch := range path {
		if ch == '.' {
			if current != "" {
				parts = append(parts, current)
			}
			current = ""
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}

	var currentValue any = doc
	for _, part := range parts {
		if m, ok := currentValue.(map[string]any); ok {
			currentValue = m[part]
		} else {
			return nil
		}
	}
	return currentValue
}
