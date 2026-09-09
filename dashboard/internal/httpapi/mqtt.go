package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/mqttclient"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// ResolveMQTTConfig wendet die Praezedenz an, die der MQTT-Einstellungstab
// dokumentiert: eine gespeicherte, aktivierte mqtt.json gewinnt
// vollstaendig (kein Feld-fuer-Feld-Mischen), sonst gilt die Verbindung
// aus config.json. Einen dritten Zweig gibt es nicht mehr - config.json
// ist Pflicht, eingebaute Standardwerte waeren nur eine stille
// Fehlerquelle. Exportiert, damit cmd/dashboard/main.go fuer den ersten
// Connect() dieselbe effektive Config baut, die dieses Paket spaeter ueber
// GET /api/v1/mqtt meldet.
func ResolveMQTTConfig(store *settings.Store, credentials *mqttclient.CredentialStore, base mqttclient.Config) (mqttclient.Config, string, error) {
	stored, err := store.LoadMQTT()
	if err != nil {
		return mqttclient.Config{}, "", err
	}
	if stored.Enabled {
		password := ""
		if credentials != nil {
			if creds, credErr := credentials.Load(); credErr == nil {
				password = creds.Password
			}
		}
		return mqttclient.Config{
			Host:              stored.Host,
			Port:              strconv.Itoa(stored.Port),
			Username:          stored.Username,
			Password:          password,
			ClientID:          stored.ClientID,
			TLS:               stored.TLS,
			TLSInsecure:       stored.TLSInsecure,
			KeepaliveSeconds:  stored.KeepaliveSeconds,
			CleanSession:      stored.CleanSession,
			DiscoveryPrefix:   stored.DiscoveryPrefix,
			ConnectTimeoutSec: stored.ConnectTimeoutSec,
			AvailabilityTopic: base.AvailabilityTopic,
			Source:            "settings",
		}, "settings", nil
	}

	base.Source = "config"
	return base, "config", nil
}

func requireRole(w http.ResponseWriter, r *http.Request, manager *auth.Manager, role, code, message string) bool {
	if manager == nil {
		writeError(w, http.StatusNotImplemented, "authentication_unavailable", "Anmeldung ist nicht konfiguriert")
		return false
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
		return false
	}
	if !auth.HasRole(user, role) {
		writeError(w, http.StatusForbidden, code, message)
		return false
	}
	return true
}

func requireHTTPS(w http.ResponseWriter, r *http.Request) bool {
	if !isSecureRequest(r) {
		writeError(w, http.StatusForbidden, "secure_connection_required", "Diese Aktion ist nur über HTTPS verfügbar")
		return false
	}
	return true
}

func requireCSRF(w http.ResponseWriter, r *http.Request, manager *auth.Manager) bool {
	cookie, err := r.Cookie(sessionCookieName(isSecureRequest(r)))
	if err != nil || manager == nil || !manager.ValidateCSRF(cookie.Value, r.Header.Get("X-CSRF-Token")) {
		writeError(w, http.StatusForbidden, "csrf_failed", "Sicherheitsprüfung fehlgeschlagen")
		return false
	}
	return true
}

type mqttConfigResponse struct {
	Enabled             bool            `json:"enabled"`
	Host                string          `json:"host"`
	Port                int             `json:"port"`
	ClientID            string          `json:"client_id"`
	Username            string          `json:"username,omitempty"`
	TLS                 bool            `json:"tls"`
	TLSInsecure         bool            `json:"tls_insecure"`
	KeepaliveSeconds    int             `json:"keepalive_seconds"`
	CleanSession        bool            `json:"clean_session"`
	DiscoveryPrefix     string          `json:"discovery_prefix"`
	ConnectTimeoutSec   int             `json:"connect_timeout_seconds"`
	PublishEnergyDevice bool            `json:"publish_energy_device"`
	SimulationActive    bool            `json:"simulation_active"`
	Metrics             map[string]bool `json:"metrics"`
	Source              string          `json:"source"`
	PasswordConfigured  bool            `json:"password_configured"`
}

// handleMQTTConfig serves the dashboard's own MQTT broker connection
// (mqtt.json). GET requires only a logged-in session (roles matter for
// writing, not reading a masked view); PUT requires mqtt_config, HTTPS and
// CSRF, matching the role table in
// knowhow/dashboard/dashboard-mqtt-setup.md. PUT never touches the running
// connection - see POST /api/v1/mqtt/reconnect for that.
func handleMQTTConfig(store *settings.Store, credentials *mqttclient.CredentialStore, authManager *auth.Manager, base mqttclient.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			cfg, source, err := ResolveMQTTConfig(store, credentials, base)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "mqtt_invalid", err.Error())
				return
			}
			stored, err := store.LoadMQTT()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "mqtt_invalid", err.Error())
				return
			}
			port, _ := strconv.Atoi(cfg.Port)
			writeJSON(w, mqttConfigResponse{
				Enabled:             stored.Enabled,
				Host:                cfg.Host,
				Port:                port,
				ClientID:            cfg.ClientID,
				Username:            cfg.Username,
				TLS:                 cfg.TLS,
				TLSInsecure:         cfg.TLSInsecure,
				KeepaliveSeconds:    cfg.KeepaliveSeconds,
				CleanSession:        cfg.CleanSession,
				DiscoveryPrefix:     cfg.DiscoveryPrefix,
				ConnectTimeoutSec:   cfg.ConnectTimeoutSec,
				PublishEnergyDevice: stored.PublishEnergyDevice,
				SimulationActive:    stored.SimulationActive,
				Metrics:             stored.Metrics,
				Source:              source,
				PasswordConfigured:  mqttPasswordConfigured(credentials),
			})
		case http.MethodPut:
			if !requireRole(w, r, authManager, auth.RoleMQTTConfig, "mqtt_config_forbidden", "Für MQTT-Einstellungen fehlt die Berechtigung") {
				return
			}
			if !requireHTTPS(w, r) {
				return
			}
			if !requireCSRF(w, r, authManager) {
				return
			}
			var value settings.MQTTConfig
			if err := decodeBody(r, &value); err != nil {
				writeError(w, http.StatusBadRequest, "mqtt_rejected", err.Error())
				return
			}
			if err := store.SaveMQTT(value); err != nil {
				writeError(w, http.StatusBadRequest, "mqtt_rejected", err.Error())
				return
			}
			writeJSON(w, value)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPut)
		}
	}
}

// handleMQTTEnergyDevice flips just MQTTConfig.publish_energy_device in
// mqtt.json. It is deliberately separate from PUT /api/v1/mqtt: the HA
// energy device is (un)published on every broker connect regardless of
// whether the dashboard-managed connection is enabled at all (see
// cmd/dashboard/main.go), so this switch must be usable without supplying
// a full, valid broker configuration.
func handleMQTTEnergyDevice(store *settings.Store, authManager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			methodNotAllowed(w, http.MethodPut)
			return
		}
		if !requireRole(w, r, authManager, auth.RoleMQTTConfig, "mqtt_config_forbidden", "Für MQTT-Einstellungen fehlt die Berechtigung") {
			return
		}
		if !requireHTTPS(w, r) {
			return
		}
		if !requireCSRF(w, r, authManager) {
			return
		}
		var body struct {
			PublishEnergyDevice bool `json:"publish_energy_device"`
		}
		if err := decodeBody(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "mqtt_rejected", err.Error())
			return
		}
		stored, err := store.LoadMQTT()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "mqtt_invalid", err.Error())
			return
		}
		stored.PublishEnergyDevice = body.PublishEnergyDevice
		if err := store.SaveMQTT(stored); err != nil {
			writeError(w, http.StatusBadRequest, "mqtt_rejected", err.Error())
			return
		}
		writeJSON(w, map[string]bool{"publish_energy_device": body.PublishEnergyDevice})
	}
}

// handleMQTTNodeSettings persists the two node-broadcast controls of the
// MQTT tab into mqtt.json: MQTTConfig.simulation_active (the retained
// outstation/<node>/settings/simulation_active/set desired state) and
// MQTTConfig.metrics (the per-metric publish toggles). Like
// handleMQTTEnergyDevice it is deliberately separate from PUT /api/v1/mqtt
// so the switches work without supplying a full, valid broker config. When
// simulation_active is part of the request the resolved state is published
// straight away via NodeSettingsPublisher; the per-connect publish in
// cmd/dashboard/main.go covers everything else.
func handleMQTTNodeSettings(store *settings.Store, authManager *auth.Manager, publisher NodeSettingsPublisher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if !requireRole(w, r, authManager, auth.RoleMQTTConfig, "mqtt_config_forbidden", "Für MQTT-Einstellungen fehlt die Berechtigung") {
			return
		}
		if !requireHTTPS(w, r) {
			return
		}
		if !requireCSRF(w, r, authManager) {
			return
		}
		var body struct {
			SimulationActive *bool           `json:"simulation_active"`
			Metrics          map[string]bool `json:"metrics"`
		}
		if err := decodeBody(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "mqtt_rejected", err.Error())
			return
		}
		stored, err := store.LoadMQTT()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "mqtt_invalid", err.Error())
			return
		}
		if body.SimulationActive != nil {
			stored.SimulationActive = *body.SimulationActive
		}
		if body.Metrics != nil {
			// Assign the freshly decoded map rather than mutating the one the
			// in-memory config cache may still hold (map-aliasing caution).
			stored.Metrics = body.Metrics
		}
		if err := store.SaveMQTT(stored); err != nil {
			writeError(w, http.StatusBadRequest, "mqtt_rejected", err.Error())
			return
		}
		if publisher != nil && body.SimulationActive != nil {
			publisher.PublishNodeSimulation(stored.SimulationActive)
		}
		writeJSON(w, map[string]any{"simulation_active": stored.SimulationActive, "metrics": stored.Metrics})
	}
}

func mqttPasswordConfigured(credentials *mqttclient.CredentialStore) bool {
	if credentials == nil {
		return false
	}
	creds, err := credentials.Load()
	return err == nil && creds.Password != ""
}

// handleMQTTCredentials sets or clears the broker password kept in
// mqtt_credentials.json, outside mqtt.json so it never lands in a settings
// revision copy. The password itself is never echoed back.
func handleMQTTCredentials(credentials *mqttclient.CredentialStore, authManager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if credentials == nil {
			writeError(w, http.StatusNotImplemented, "mqtt_unavailable", "MQTT-Zugangsdaten sind nicht verfügbar")
			return
		}
		if !requireRole(w, r, authManager, auth.RoleMQTTConfig, "mqtt_config_forbidden", "Für MQTT-Zugangsdaten fehlt die Berechtigung") {
			return
		}
		if !requireHTTPS(w, r) {
			return
		}
		if !requireCSRF(w, r, authManager) {
			return
		}
		switch r.Method {
		case http.MethodPost:
			var body struct {
				Password string `json:"password"`
			}
			if err := decodeBody(r, &body); err != nil {
				writeError(w, http.StatusBadRequest, "mqtt_rejected", err.Error())
				return
			}
			if strings.TrimSpace(body.Password) == "" {
				writeError(w, http.StatusBadRequest, "mqtt_rejected", "Passwort darf nicht leer sein")
				return
			}
			if err := credentials.Save(mqttclient.Credentials{Password: body.Password}); err != nil {
				writeError(w, http.StatusBadRequest, "mqtt_rejected", err.Error())
				return
			}
			writeJSON(w, map[string]bool{"password_configured": true})
		case http.MethodDelete:
			if err := credentials.Delete(); err != nil {
				writeError(w, http.StatusInternalServerError, "mqtt_credentials_failed", err.Error())
				return
			}
			writeJSON(w, map[string]bool{"password_configured": false})
		default:
			methodNotAllowed(w, http.MethodPost, http.MethodDelete)
		}
	}
}

type mqttTestRequest struct {
	Host              string `json:"host"`
	Port              int    `json:"port"`
	ClientID          string `json:"client_id"`
	Username          string `json:"username"`
	Password          string `json:"password,omitempty"`
	TLS               bool   `json:"tls"`
	TLSInsecure       bool   `json:"tls_insecure"`
	DiscoveryPrefix   string `json:"discovery_prefix"`
	ConnectTimeoutSec int    `json:"connect_timeout_seconds"`
}

// handleMQTTTest opens a short-lived, separate connection to verify a
// candidate configuration before it is saved. It never touches the running
// production connection - see mqttclient.TestConnection.
func handleMQTTTest(credentials *mqttclient.CredentialStore, authManager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if !requireRole(w, r, authManager, auth.RoleMQTTConfig, "mqtt_config_forbidden", "Für den Verbindungstest fehlt die Berechtigung") {
			return
		}
		if !requireHTTPS(w, r) {
			return
		}
		if !requireCSRF(w, r, authManager) {
			return
		}
		var body mqttTestRequest
		if err := decodeBody(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "mqtt_rejected", err.Error())
			return
		}
		if strings.TrimSpace(body.Host) == "" {
			writeError(w, http.StatusBadRequest, "mqtt_rejected", "host darf nicht leer sein")
			return
		}
		if body.Port < 1 || body.Port > 65535 {
			writeError(w, http.StatusBadRequest, "mqtt_rejected", "port muss zwischen 1 und 65535 liegen")
			return
		}
		password := body.Password
		if password == "" && credentials != nil {
			if creds, err := credentials.Load(); err == nil {
				password = creds.Password
			}
		}
		timeout := time.Duration(body.ConnectTimeoutSec) * time.Second
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		result := mqttclient.TestConnection(mqttclient.Config{
			Host:            body.Host,
			Port:            strconv.Itoa(body.Port),
			Username:        body.Username,
			Password:        password,
			ClientID:        body.ClientID,
			TLS:             body.TLS,
			TLSInsecure:     body.TLSInsecure,
			DiscoveryPrefix: body.DiscoveryPrefix,
		}, timeout)
		writeJSON(w, result)
	}
}

// handleMQTTReconnect applies the effective saved configuration to the live
// connection via mqttclient.Client.Reconfigure, which falls back to the
// previous configuration on failure - see that method's doc comment. The
// response always includes the resulting status, even on failure, so the UI
// can show what actually happened.
func handleMQTTReconnect(store *settings.Store, credentials *mqttclient.CredentialStore, authManager *auth.Manager, reconfigurer MQTTReconfigurer, statusProvider mqttclient.StatusProvider, base mqttclient.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if reconfigurer == nil {
			writeError(w, http.StatusNotImplemented, "mqtt_unavailable", "MQTT-Reconnect ist nicht verfügbar")
			return
		}
		if !requireRole(w, r, authManager, auth.RoleMQTTConfig, "mqtt_config_forbidden", "Für den Reconnect fehlt die Berechtigung") {
			return
		}
		if !requireHTTPS(w, r) {
			return
		}
		if !requireCSRF(w, r, authManager) {
			return
		}
		cfg, _, err := ResolveMQTTConfig(store, credentials, base)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "mqtt_invalid", err.Error())
			return
		}
		reconfigureErr := reconfigurer.Reconfigure(cfg)
		response := map[string]any{"ok": reconfigureErr == nil}
		if statusProvider != nil {
			response["status"] = statusProvider.Status()
		}
		if reconfigureErr != nil {
			response["error"] = reconfigureErr.Error()
			writeJSONStatus(w, http.StatusBadGateway, response)
			return
		}
		writeJSON(w, response)
	}
}

func handleMQTTStatus(statusProvider mqttclient.StatusProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if statusProvider == nil {
			writeError(w, http.StatusServiceUnavailable, "mqtt_unavailable", "MQTT-Client ist nicht verfügbar")
			return
		}
		writeJSON(w, statusProvider.Status())
	}
}
