package httpapi

import (
	"net/http"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/beispiel"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// handleBeispielStop schaltet die Beispiel-Simulation aus und entfernt mit
// remove=true die unveränderten Beispiel-Teile. Die Rechte folgen dem
// MQTT-Node-Settings-Endpunkt: mqtt_config für das Ausschalten, zusätzlich
// edit_config, sobald Geräte, Szenario oder Zuordnungen entfernt werden.
func handleBeispielStop(dataDir string, configs *config.Manager, store *settings.Store, authManager *auth.Manager, publisher NodeSettingsPublisher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if !requireRole(w, r, authManager, auth.RoleMQTTConfig, "mqtt_config_forbidden", "error.mqtt_config_forbidden.settings", "Für MQTT-Einstellungen fehlt die Berechtigung") {
			return
		}
		if !requireHTTPS(w, r) {
			return
		}
		if !requireCSRF(w, r, authManager) {
			return
		}
		var body struct {
			Remove bool `json:"remove"`
		}
		if err := decodeBody(r, &body); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
			return
		}
		if body.Remove && !requireRole(w, r, authManager, auth.RoleEditConfig, "edit_config_forbidden", "error.edit_config_forbidden", "Zum Bearbeiten der Gerätekonfiguration fehlt die Berechtigung.") {
			return
		}
		publish := func(active bool) {
			if publisher != nil {
				publisher.PublishNodeSimulation(active)
			}
		}
		res, err := beispiel.Stop(dataDir, body.Remove, configs, store, publish)
		if err != nil {
			writeErrorDetail(w, http.StatusInternalServerError, "settings_invalid", err)
			return
		}
		// Ohne remove liefert Stop leere Ergebnisse ohne Slices. Die Antwort
		// soll trotzdem Arrays tragen, nie null.
		if res.Removed == nil {
			res.Removed = []string{}
		}
		if res.Kept == nil {
			res.Kept = []string{}
		}
		writeJSON(w, res)
	}
}
