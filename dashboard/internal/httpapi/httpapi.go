// Package httpapi wires the /api/v1/* HTTP handlers of the dashboard plus
// (from Phase 1 on) the server-side overview page at "/".
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/basepath"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/devicefilter"
	"github.com/Developer-Simon/energy-node-dashboard/internal/diagnostics"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/mqttclient"
	"github.com/Developer-Simon/energy-node-dashboard/internal/nodeagent"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registryevents"
	"github.com/Developer-Simon/energy-node-dashboard/internal/runtimecache"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
	"github.com/Developer-Simon/energy-node-dashboard/internal/shellypresets"
	"github.com/Developer-Simon/energy-node-dashboard/internal/storagehealth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/systemactions"
	"github.com/Developer-Simon/energy-node-dashboard/internal/tailscale"
	"github.com/Developer-Simon/energy-node-dashboard/internal/tinytuya"
	"github.com/Developer-Simon/energy-node-dashboard/internal/webui"
)

type CommandPublisher interface {
	Publish(topic, payload string) error
}

type DeviceReloader interface {
	Reload() error
}

type DeviceActioner interface {
	IgnoreDevice(deviceID string) error
	UnignoreDevice(deviceID string) error
	DeleteDeviceDiscovery(deviceID string) error
}

type SystemActionExecutor interface {
	Execute(context.Context, systemactions.Action) error
}

// MQTTReconfigurer is implemented by *mqttclient.Client. It is its own
// interface (rather than reusing mqttclient.StatusProvider) so tests can
// supply a fake without pulling in a real Paho connection.
type MQTTReconfigurer interface {
	Reconfigure(mqttclient.Config) error
}

const maxCommandActionsPerDevice = 12

type CommandAction struct {
	At       time.Time `json:"at"`
	User     string    `json:"user,omitempty"`
	EntityID string    `json:"entity_id"`
	Topic    string    `json:"topic"`
	Payload  string    `json:"payload"`
	Result   string    `json:"result"`
	Error    string    `json:"error,omitempty"`
}

type commandHistory struct {
	mu      sync.RWMutex
	entries map[string][]CommandAction
}

func newCommandHistory() *commandHistory {
	return &commandHistory{entries: make(map[string][]CommandAction)}
}

func (h *commandHistory) add(deviceID string, action CommandAction) {
	if h == nil || deviceID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	entries := append(h.entries[deviceID], action)
	if len(entries) > maxCommandActionsPerDevice {
		entries = entries[len(entries)-maxCommandActionsPerDevice:]
	}
	h.entries[deviceID] = entries
}

func (h *commandHistory) forDevice(deviceID string) []CommandAction {
	if h == nil {
		return []CommandAction{}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]CommandAction(nil), h.entries[deviceID]...)
}

const (
	secureSessionCookie = "energy_node_session"
	guestSessionCookie  = "energy_node_guest_session"
)

type RouterDependencies struct {
	MQTT               mqttclient.StatusProvider
	MQTTReconfigure    MQTTReconfigurer
	MQTTCredentials    *mqttclient.CredentialStore
	Reloader           DeviceReloader
	Auth               *auth.Manager
	SystemActions      SystemActionExecutor
	DeviceFilter       *devicefilter.Store
	DeviceActions      DeviceActioner
	StartedAt          time.Time
	Now                func() time.Time
	ShellyPresets      *shellypresets.Store
	BridgeCredentials  *mqttclient.CredentialStore
	MQTTBridgeWatcher  BridgeWatcher
	SystemStatusRunner systemactions.OutputRunner
	DataDir            string
	AppConfigPath      string
	BridgeTargetPath   string
	Tailscale          *tailscale.Client
	Resolver           *energy.Resolver
	// Version ist die aus dashboard/VERSION plus Branch-Suffix gebaute
	// Versionskennung (siehe main.buildVersion), leer bzw. "dev" ausserhalb
	// von Release-Builds.
	Version string
	// ServicesVersion ist der Inhalt von services/VERSION, gelesen von der in
	// config.json unter paths.services_version_file konfigurierten Datei.
	// Leer, wenn nicht konfiguriert oder nicht lesbar.
	ServicesVersion string
	// AdminAuthWarning wird auf der Anmeldeseite angezeigt, wenn das
	// Admin-Passwort beim Start nicht gelesen werden konnte (siehe
	// main.go). Der Dienst startet trotzdem - fail-closed gilt fuer
	// config.json selbst, nicht fuer eine einzelne Passwortdatei, die
	// sonst ohne SSH-Zugriff niemand mehr reparieren koennte.
	AdminAuthWarning string
	// MQTTBase ist die Verbindung aus config.json - die zweite Stufe der
	// Praezedenz, wenn keine aktivierte mqtt.json vorliegt.
	MQTTBase mqttclient.Config
	// NodeAgent publiziert den Pi-Knoten als HA-Geraet und haelt die
	// zuletzt gelesene Systemtelemetrie. Der Health-Endpunkt (Task 6/7)
	// liest daraus; hier nur durchgereicht.
	NodeAgent *nodeagent.Agent
}

// NewRouter builds the HTTP mux for the dashboard. Later phases extend this
// function rather than introducing a second routing entry point.
func NewRouter(reg *registry.Registry, configs *config.Manager, store *settings.Store) *http.ServeMux {
	return NewRouterWithTinyTuyaAndPublisher(reg, configs, store, nil, nil, nil)
}

func NewRouterWithTinyTuya(reg *registry.Registry, configs *config.Manager, store *settings.Store, probe tinytuya.Prober, credentialStore *tinytuya.CredentialStore) *http.ServeMux {
	return NewRouterWithTinyTuyaAndPublisher(reg, configs, store, probe, credentialStore, nil)
}

func NewRouterWithTinyTuyaAndPublisher(reg *registry.Registry, configs *config.Manager, store *settings.Store, probe tinytuya.Prober, credentialStore *tinytuya.CredentialStore, publisher CommandPublisher) *http.ServeMux {
	return NewRouterWithStorageHealth(reg, configs, store, probe, credentialStore, publisher, storagehealth.New())
}

func NewRouterWithTinyTuyaAndPublisherAndRuntimeCache(reg *registry.Registry, configs *config.Manager, store *settings.Store, probe tinytuya.Prober, credentialStore *tinytuya.CredentialStore, publisher CommandPublisher, cache runtimecache.StatusProvider) *http.ServeMux {
	return NewRouterWithTinyTuyaAndPublisherAndRuntimeCacheAndStorage(reg, configs, store, probe, credentialStore, publisher, cache, storagehealth.New())
}

func NewRouterWithTinyTuyaAndPublisherAndRuntimeCacheAndStorage(reg *registry.Registry, configs *config.Manager, store *settings.Store, probe tinytuya.Prober, credentialStore *tinytuya.CredentialStore, publisher CommandPublisher, cache runtimecache.StatusProvider, storageProvider storagehealth.Provider) *http.ServeMux {
	return NewRouterWithStorageHealthAndRuntimeCache(reg, configs, store, probe, credentialStore, publisher, storageProvider, cache)
}

func NewRouterWithStorageHealth(reg *registry.Registry, configs *config.Manager, store *settings.Store, probe tinytuya.Prober, credentialStore *tinytuya.CredentialStore, publisher CommandPublisher, storageProvider storagehealth.Provider) *http.ServeMux {
	return NewRouterWithStorageHealthAndRuntimeCache(reg, configs, store, probe, credentialStore, publisher, storageProvider, nil)
}

func NewRouterWithStorageHealthAndRuntimeCache(reg *registry.Registry, configs *config.Manager, store *settings.Store, probe tinytuya.Prober, credentialStore *tinytuya.CredentialStore, publisher CommandPublisher, storageProvider storagehealth.Provider, cache runtimecache.StatusProvider) *http.ServeMux {
	return NewRouterWithDependencies(reg, configs, store, probe, credentialStore, publisher, storageProvider, cache, RouterDependencies{})
}

func NewAuthenticatedRouter(reg *registry.Registry, configs *config.Manager, store *settings.Store, probe tinytuya.Prober, credentialStore *tinytuya.CredentialStore, publisher CommandPublisher, storageProvider storagehealth.Provider, cache runtimecache.StatusProvider, dependencies RouterDependencies) http.Handler {
	return authMiddleware(dependencies.Auth, store, dependencies.AdminAuthWarning, NewRouterWithDependencies(reg, configs, store, probe, credentialStore, publisher, storageProvider, cache, dependencies))
}

func NewRouterWithDependencies(reg *registry.Registry, configs *config.Manager, store *settings.Store, probe tinytuya.Prober, credentialStore *tinytuya.CredentialStore, publisher CommandPublisher, storageProvider storagehealth.Provider, cache runtimecache.StatusProvider, dependencies RouterDependencies) *http.ServeMux {
	mux := http.NewServeMux()
	commands := newCommandHistory()
	engine := diagnostics.NewEngine(reg, store)
	engine.SetConfigManager(configs)
	engine.SetStartedAt(dependencies.StartedAt)
	now := dependencies.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	mux.HandleFunc("/api/v1/health", handleHealth(cache, storageProvider, dependencies.MQTT, dependencies.NodeAgent, dependencies.StartedAt, dependencies.Version, dependencies.ServicesVersion, now))
	mux.HandleFunc("/api/v1/runtime-cache", handleRuntimeCache(cache))
	mux.Handle("/static/", webui.Static())
	mux.HandleFunc("/api/v1/devices", handleDevices(reg))
	mux.HandleFunc("/api/v1/devices/ignored", handleIgnoredDevices(dependencies.DeviceFilter))
	engine.SetIgnoredStore(dependencies.DeviceFilter)
	mux.HandleFunc("/api/v1/devices/", handleDevice(reg, engine, dependencies.Reloader, now, commands, configs, dependencies.Auth, dependencies.DeviceFilter, dependencies.DeviceActions))
	mux.HandleFunc("/api/v1/discovery", handleDiscovery(reg))
	// /api/v1/discovery ist ohne Schraegstrich registriert, hat also keinen
	// Unterrouter, mit dem diese Route kollidieren koennte.
	mux.HandleFunc("/api/v1/discovery/summary", handleDiscoverySummary(reg))
	mux.HandleFunc("/api/v1/entities/", handleEntityCommand(reg, publisher, commands))
	mux.HandleFunc("/api/v1/topics", handleTopics(reg))
	mux.HandleFunc("/api/v1/topics/samples", handleTopicSamples(reg))
	mux.HandleFunc("/api/v1/automation/notification", handleAutomationNotification(reg))
	resolver := dependencies.Resolver
	if resolver == nil {
		resolver = energy.NewResolver(nil)
	}
	if store != nil {
		if value, err := store.LoadEnergy(); err == nil {
			resolver.SetOverrides(value.Assignments)
			resolver.SetInterpretation(value.Interpretation)
		}
	}
	mux.HandleFunc("/api/v1/events", handleEvents(reg, store, resolver, engine))
	mux.HandleFunc("/api/v1/energy", handleEnergy(reg, resolver))
	mux.HandleFunc("/api/v1/energy/roles", handleEnergyRoles(store, resolver))
	mux.HandleFunc("/api/v1/energy/interpretation", handleEnergyInterpretation(store, resolver))
	mux.HandleFunc("/api/v1/history/entities", handleHistoryEntities(reg, store))
	newHistoryExchange().routes(mux)
	mux.HandleFunc("/api/v1/diagnostics", handleDiagnostics(engine))
	mux.HandleFunc("/api/v1/diagnostics/rules", handleDiagnosticRules(engine))
	mux.HandleFunc("/api/v1/diagnostics/health", handleDiagnosticHealth(engine))
	mux.HandleFunc("/api/v1/health/storage", handleStorageHealth(storageProvider))
	mux.HandleFunc("/api/v1/shelly/presets", handleShellyPresets(dependencies.ShellyPresets))
	if dependencies.Auth != nil {
		mux.HandleFunc("/api/v1/auth/login", handleAuthLogin(dependencies.Auth))
		mux.HandleFunc("/api/v1/auth/guest", handleAuthGuest(dependencies.Auth))
		mux.HandleFunc("/api/v1/auth/session", handleAuthSession(dependencies.Auth))
		mux.HandleFunc("/api/v1/auth/logout", handleAuthLogout(dependencies.Auth))
	}
	if dependencies.SystemActions != nil {
		mux.HandleFunc("/api/v1/system/restart-dashboard", handleSystemAction(dependencies.Auth, dependencies.SystemActions, systemactions.RestartDashboard))
		mux.HandleFunc("/api/v1/system/reboot", handleSystemAction(dependencies.Auth, dependencies.SystemActions, systemactions.Reboot))
		mux.HandleFunc("/api/v1/system/poweroff", handleSystemAction(dependencies.Auth, dependencies.SystemActions, systemactions.Poweroff))
	}
	if dependencies.AppConfigPath != "" {
		mux.HandleFunc("/api/v1/system/config", handleSystemConfig(dependencies.AppConfigPath, dependencies.DataDir, dependencies.Auth, nil))
		mux.HandleFunc("/api/v1/system/config/schema", handleSystemConfigSchema())
	}
	if configs != nil {
		mux.HandleFunc("/api/v1/configurations", handleConfigurations(configs))
		mux.HandleFunc("/api/v1/configurations/", handleConfiguration(configs, dependencies.Auth))
		mux.HandleFunc("/api/v1/automations/test", handleAutomationTest(publisher, dependencies.Auth))
		mux.HandleFunc("/api/v1/automations/history/", handleAutomationHistory(configs))
	}
	if store != nil {
		mux.HandleFunc("/api/v1/layout", handleLayout(store))
		mux.HandleFunc("/api/v1/layout/", handleLayoutRevision(store))
		mux.HandleFunc("/api/v1/settings", handleSettings(store, dependencies.Auth))
		mux.HandleFunc("/api/v1/settings/", handleSettingsRevision(store))
		mux.HandleFunc("/api/v1/energy/", handleEnergyRevision(store))
		if deviceMap, err := store.LoadDeviceMap(); err == nil {
			applyRelationOverrides(reg, deviceMap)
		}
		mux.HandleFunc("/api/v1/device/map", handleDeviceMap(store))
		mux.HandleFunc("/api/v1/device/map/", handleDeviceMapSub(store, reg))
		mux.HandleFunc("/api/v1/mqtt", handleMQTTConfig(store, dependencies.MQTTCredentials, dependencies.Auth, dependencies.MQTTBase))
		mux.HandleFunc("/api/v1/mqtt/credentials", handleMQTTCredentials(dependencies.MQTTCredentials, dependencies.Auth))
		mux.HandleFunc("/api/v1/mqtt/energy-device", handleMQTTEnergyDevice(store, dependencies.Auth))
		mux.HandleFunc("/api/v1/mqtt/test", handleMQTTTest(dependencies.MQTTCredentials, dependencies.Auth))
		mux.HandleFunc("/api/v1/mqtt/reconnect", handleMQTTReconnect(store, dependencies.MQTTCredentials, dependencies.Auth, dependencies.MQTTReconfigure, dependencies.MQTT, dependencies.MQTTBase))
		mux.HandleFunc("/api/v1/mqtt/status", handleMQTTStatus(dependencies.MQTT))

		bridgeTargetPath := dependencies.BridgeTargetPath
		if bridgeTargetPath == "" {
			bridgeTargetPath = defaultBridgeTargetPath
		}
		bridgeApplyState := newBridgeApplyState()
		mux.HandleFunc("/api/v1/mqtt/bridge", handleBridgeConfig(store, dependencies.BridgeCredentials, dependencies.Auth, dependencies.MQTTBridgeWatcher))
		mux.HandleFunc("/api/v1/mqtt/bridge/credentials", handleBridgeCredentials(dependencies.BridgeCredentials, dependencies.Auth))
		mux.HandleFunc("/api/v1/mqtt/bridge/apply", handleBridgeApply(store, dependencies.BridgeCredentials, dependencies.Auth, dependencies.SystemActions, dependencies.MQTTBridgeWatcher, dependencies.DataDir, bridgeApplyState))
		mux.HandleFunc("/api/v1/mqtt/bridge/restart", handleBridgeRestart(dependencies.Auth, dependencies.SystemActions))
		mux.HandleFunc("/api/v1/mqtt/bridge/status", handleBridgeStatus(store, dependencies.BridgeCredentials, dependencies.MQTTBridgeWatcher, dependencies.SystemStatusRunner, bridgeTargetPath, bridgeApplyState))
		mux.HandleFunc("/api/v1/mqtt/bridge/", handleBridgeSub(store, dependencies.Auth, dependencies.MQTTBridgeWatcher))
	}
	mux.HandleFunc("/api/v1/tiny-tuya/devices", handleTinyTuyaDevices(probe, credentialStore))
	mux.HandleFunc("/api/v1/tiny-tuya/status", handleTinyTuyaStatus(probe))
	mux.HandleFunc("/api/v1/tiny-tuya/configure", handleTinyTuyaConfigure(configs))
	mux.HandleFunc("/api/v1/tiny-tuya/credentials", handleTinyTuyaCredentials(credentialStore))
	if dependencies.Tailscale != nil {
		tailscaleActionState := newTailscaleActionState()
		mux.HandleFunc("/api/v1/tailscale/status", handleTailscaleStatus(dependencies.Tailscale, tailscaleActionState))
		mux.HandleFunc("/api/v1/tailscale/prereqs", handleTailscalePrereqs(dependencies.Tailscale))
		mux.HandleFunc("/api/v1/tailscale/login", handleTailscaleLogin(dependencies.Auth, dependencies.SystemActions, tailscaleActionState))
		mux.HandleFunc("/api/v1/tailscale/logout", handleTailscaleLogout(dependencies.Auth, dependencies.SystemActions, tailscaleActionState))
		mux.HandleFunc("/api/v1/tailscale/restart", handleTailscaleRestart(dependencies.Auth, dependencies.SystemActions, tailscaleActionState))
	}
	mux.HandleFunc("/", webui.OverviewWithDeviceFilterAndEngine(reg, configs, store, dependencies.DeviceFilter, engine))
	return mux
}

func authMiddleware(manager *auth.Manager, store *settings.Store, adminAuthWarning string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if manager == nil || strings.HasPrefix(r.URL.Path, "/static/") || strings.HasPrefix(r.URL.Path, "/api/v1/auth/") {
			next.ServeHTTP(w, r)
			return
		}
		secureRequest := isSecureRequest(r)
		cookie, err := r.Cookie(sessionCookieName(secureRequest))
		if err == nil {
			if session, ok := manager.Session(cookie.Value); ok {
				if secureRequest || !auth.HasRole(session.User, auth.RoleSystemActions) {
					next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), session.User)))
					return
				}
			}
		}
		if r.URL.Path == "/" && r.Method == http.MethodGet {
			webui.Login(!secureRequest, store, adminAuthWarning).ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
			return
		}
		http.Error(w, "Anmeldung erforderlich", http.StatusUnauthorized)
	})
}

func handleAuthLogin(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if !isSecureRequest(r) {
			writeError(w, http.StatusForbidden, "secure_login_required", "Admin-Anmeldung ist nur über HTTPS verfügbar")
			return
		}
		var request struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := decodeBody(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		session, err := manager.Login(request.Username, request.Password)
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "Benutzername oder Passwort ist ungültig")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "login_failed", "Anmeldung konnte nicht gespeichert werden")
			return
		}
		setSessionCookie(w, r, session)
		writeAuthSession(w, session)
	}
}

func handleAuthGuest(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		session, err := manager.ContinueAsGuest()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "guest_login_failed", "Gastzugang konnte nicht angelegt werden")
			return
		}
		setSessionCookie(w, r, session)
		writeAuthSession(w, session)
	}
}

func handleAuthSession(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		cookie, err := r.Cookie(sessionCookieName(isSecureRequest(r)))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
			return
		}
		session, ok := manager.Session(cookie.Value)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
			return
		}
		if !isSecureRequest(r) && auth.HasRole(session.User, auth.RoleSystemActions) {
			writeError(w, http.StatusForbidden, "secure_login_required", "Admin-Sitzungen sind nur über HTTPS verfügbar")
			return
		}
		writeAuthSession(w, session)
	}
}

func handleAuthLogout(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		secureRequest := isSecureRequest(r)
		if cookie, err := r.Cookie(sessionCookieName(secureRequest)); err == nil {
			manager.Logout(cookie.Value)
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookieName(secureRequest), Value: "", Path: basepath.CookiePath(r), MaxAge: -1, HttpOnly: true, Secure: secureRequest, SameSite: http.SameSiteLaxMode})
		writeJSON(w, map[string]string{"status": "logged_out"})
	}
}

func handleSystemAction(manager *auth.Manager, executor SystemActionExecutor, action systemactions.Action) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if manager == nil {
			writeError(w, http.StatusNotImplemented, "system_actions_unavailable", "Systemaktionen sind nicht konfiguriert")
			return
		}
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
			return
		}
		if !auth.HasRole(user, auth.RoleSystemActions) {
			writeError(w, http.StatusForbidden, "system_actions_forbidden", "Für Systemaktionen fehlt die Berechtigung")
			return
		}
		cookie, err := r.Cookie(sessionCookieName(isSecureRequest(r)))
		if err != nil || !manager.ValidateCSRF(cookie.Value, r.Header.Get("X-CSRF-Token")) {
			writeError(w, http.StatusForbidden, "csrf_failed", "Sicherheitsprüfung fehlgeschlagen")
			return
		}
		if err := executor.Execute(r.Context(), action); err != nil {
			if errors.Is(err, systemactions.ErrBusy) {
				writeError(w, http.StatusConflict, "system_action_busy", "Eine Systemaktion läuft bereits")
				return
			}
			writeError(w, http.StatusInternalServerError, "system_action_failed", "Systemaktion konnte nicht gestartet werden")
			return
		}
		w.WriteHeader(http.StatusAccepted)
		writeJSON(w, map[string]string{"action": string(action), "status": "accepted"})
	}
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, session auth.Session) {
	maxAge := int(time.Until(session.ExpiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName(secure), Value: session.CookieValue(), Path: basepath.CookiePath(r), MaxAge: maxAge, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
}

func sessionCookieName(secure bool) string {
	if secure {
		return secureSessionCookie
	}
	return guestSessionCookie
}

func isSecureRequest(r *http.Request) bool {
	forwardedProto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	return r.TLS != nil || strings.EqualFold(forwardedProto, "https")
}

func writeAuthSession(w http.ResponseWriter, session auth.Session) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{
		"username":                session.User.Username,
		"guest":                   session.User.Guest,
		"roles":                   session.User.Roles,
		"system_actions":          auth.HasRole(session.User, auth.RoleSystemActions),
		"delete_device_discovery": auth.HasRole(session.User, auth.RoleDeleteDeviceDiscovery),
		"tune_live_updates":       auth.HasRole(session.User, auth.RoleTuneLiveUpdates),
		"mqtt_config":             auth.HasRole(session.User, auth.RoleMQTTConfig),
		"automations":             auth.HasRole(session.User, auth.RoleAutomations),
		"edit_layout":             auth.HasRole(session.User, auth.RoleEditLayout),
		"csrf_token":              session.CSRFToken,
		"expires_at":              session.ExpiresAt,
	})
}

// entityCommandPendingTimeout is how long a published command waits for a
// live MQTT state confirmation before the registry falls back to "timeout"
// (see registry.Registry.ExpirePendingCommands and P1.3 in the ideenliste).
const entityCommandPendingTimeout = 10 * time.Second

func handleEntityCommand(reg *registry.Registry, publisher CommandPublisher, history *commandHistory) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/entities/")
		if !strings.HasSuffix(path, "/command") {
			http.NotFound(w, r)
			return
		}
		uniqueID, err := url.PathUnescape(strings.TrimSuffix(path, "/command"))
		if err != nil || uniqueID == "" {
			http.NotFound(w, r)
			return
		}
		if publisher == nil {
			writeError(w, http.StatusServiceUnavailable, "commands_unavailable", "MQTT-Befehle sind nicht verfügbar")
			return
		}
		command, ok := reg.Command(uniqueID)
		if !ok {
			writeError(w, http.StatusNotFound, "command_not_supported", "Entität ist nicht schaltbar")
			return
		}
		var request struct {
			Value   *float64 `json:"value"`
			Payload string   `json:"payload"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&request)
		if decodeErr != nil && !errors.Is(decodeErr, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid_command", "Ungültiger Befehlsinhalt")
			return
		}
		payload := command.PayloadOn
		if command.Component == "number" {
			if request.Value == nil || math.IsNaN(*request.Value) || math.IsInf(*request.Value, 0) {
				writeError(w, http.StatusBadRequest, "invalid_command_value", "Eine gültige Zahl ist erforderlich")
				return
			}
			value := *request.Value
			if command.MinValue != nil && value < *command.MinValue || command.MaxValue != nil && value > *command.MaxValue {
				writeError(w, http.StatusBadRequest, "command_value_out_of_range", "Der Wert liegt außerhalb des erlaubten Bereichs")
				return
			}
			if command.Step != nil && *command.Step > 0 {
				base := 0.0
				if command.MinValue != nil {
					base = *command.MinValue
				}
				steps := (value - base) / *command.Step
				if math.Abs(steps-math.Round(steps)) > 1e-9 {
					writeError(w, http.StatusBadRequest, "invalid_command_step", "Der Wert entspricht nicht der erlaubten Schrittweite")
					return
				}
			}
			payload = strconv.FormatFloat(value, 'f', -1, 64)
		} else if request.Payload != "" {
			if request.Payload != command.PayloadOn && request.Payload != command.PayloadOff {
				writeError(w, http.StatusBadRequest, "invalid_command_payload", "Ungültiger Schaltwert")
				return
			}
			payload = request.Payload
		} else if command.HasValue && command.Value == command.PayloadOn {
			payload = command.PayloadOff
		}
		now := time.Now().UTC()
		ok, conflict := reg.BeginPendingCommand(uniqueID, payload, now, entityCommandPendingTimeout)
		if !ok {
			if conflict {
				writeError(w, http.StatusConflict, "command_pending", "Für diese Entität läuft bereits ein Schreibvorgang")
				return
			}
			writeError(w, http.StatusNotFound, "command_not_supported", "Entität ist nicht schaltbar")
			return
		}
		deviceID := entityDeviceID(reg, uniqueID)
		action := CommandAction{At: now, EntityID: uniqueID, Topic: command.Topic, Payload: payload, Result: "published"}
		if user, ok := auth.UserFromContext(r.Context()); ok {
			action.User = user.Username
		}
		if err := publisher.Publish(command.Topic, payload); err != nil {
			reg.CancelPendingCommand(uniqueID)
			action.Result = "error"
			action.Error = err.Error()
			history.add(deviceID, action)
			writeError(w, http.StatusBadGateway, "command_publish_failed", err.Error())
			return
		}
		history.add(deviceID, action)
		writeJSON(w, map[string]any{
			"entity_id":        uniqueID,
			"payload":          payload,
			"status":           "pending",
			"pending_deadline": now.Add(entityCommandPendingTimeout).Format(time.RFC3339Nano),
		})
	}
}

func entityDeviceID(reg *registry.Registry, uniqueID string) string {
	deviceID, _ := reg.DeviceIDForUnique(uniqueID)
	return deviceID
}

func handleEnergy(reg *registry.Registry, resolver *energy.Resolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		snapshot := energy.Aggregate(reg.Snapshot(), resolver, time.Now().UTC())
		writeJSON(w, snapshot.WithInterpretation(resolver.Interpretation()))
	}
}

func handleEnergyRoles(store *settings.Store, resolver *energy.Resolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			writeError(w, http.StatusNotImplemented, "energy_roles_unavailable", "Energie-Rollen sind ohne Datenverzeichnis nicht verfügbar")
			return
		}
		switch r.Method {
		case http.MethodGet:
			value, err := store.LoadEnergy()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "energy_roles_invalid", err.Error())
				return
			}
			writeJSON(w, value)
		case http.MethodPut:
			// Decoding into the stored config (not a zero value) means a body
			// that omits "interpretation" entirely keeps the saved
			// interpretation instead of resetting it - see
			// knowhow/dashboard/energie-interpretation.md. Assignments is
			// reset to nil first so the map is fully replaced rather than
			// merged, preserving the pre-existing full-replace semantics for
			// role assignments.
			value, err := store.LoadEnergy()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "energy_roles_invalid", err.Error())
				return
			}
			value.Assignments = nil
			if err := decodeBody(r, &value); err != nil {
				writeError(w, http.StatusBadRequest, "energy_roles_rejected", err.Error())
				return
			}
			if err := store.SaveEnergy(value); err != nil {
				writeError(w, http.StatusBadRequest, "energy_roles_rejected", err.Error())
				return
			}
			resolver.SetOverrides(value.Assignments)
			resolver.SetInterpretation(value.Interpretation)
			writeJSON(w, value)
		default:
			methodNotAllowed(w)
		}
	}
}

func handleEnergyInterpretation(store *settings.Store, resolver *energy.Resolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			writeError(w, http.StatusNotImplemented, "energy_interpretation_unavailable", "Energie-Interpretation ist ohne Datenverzeichnis nicht verfügbar")
			return
		}
		switch r.Method {
		case http.MethodGet:
			value, err := store.LoadEnergy()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "energy_interpretation_invalid", err.Error())
				return
			}
			writeJSON(w, value.Interpretation)
		case http.MethodPut:
			current, err := store.LoadEnergy()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "energy_interpretation_invalid", err.Error())
				return
			}
			var interpretation energy.Interpretation
			if err := decodeBody(r, &interpretation); err != nil {
				writeError(w, http.StatusBadRequest, "energy_interpretation_rejected", err.Error())
				return
			}
			current.Interpretation = interpretation
			if err := store.SaveEnergy(current); err != nil {
				writeError(w, http.StatusBadRequest, "energy_interpretation_rejected", err.Error())
				return
			}
			resolver.SetInterpretation(current.Interpretation)
			writeJSON(w, current.Interpretation)
		default:
			methodNotAllowed(w)
		}
	}
}

func handleDiagnostics(engine *diagnostics.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, engine.Evaluate(time.Now()))
	}
}

func handleDiagnosticRules(engine *diagnostics.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, engine.RuleIDs())
	}
}

func handleDiagnosticHealth(engine *diagnostics.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, engine.Health(time.Now().UTC()))
	}
}

func handleStorageHealth(provider storagehealth.Provider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		if provider == nil {
			writeError(w, http.StatusServiceUnavailable, "storage_health_unavailable", "Speichermedium-Diagnose ist nicht verfügbar")
			return
		}
		report, err := provider.Check(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "storage_health_failed", err.Error())
			return
		}
		writeJSON(w, report)
	}
}

// handleShellyPresets serves the read-only shelly_presets.json document used
// by the configuration UI's shelly_devices editor. It never writes device
// JSON and never triggers a bridge reload; saving merged devices still goes
// through PUT /api/v1/configurations/shelly_devices.
func handleShellyPresets(store *shellypresets.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		if store == nil {
			writeError(w, http.StatusServiceUnavailable, "shelly_presets_unavailable", "Shelly-Presets sind nicht verfügbar")
			return
		}
		presets, err := store.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "shelly_presets_failed", err.Error())
			return
		}
		writeJSON(w, presets)
	}
}

type healthResponse struct {
	Status          string              `json:"status"`
	RuntimeCache    runtimecache.Status `json:"runtime_cache"`
	Version         string              `json:"version"`
	ServicesVersion string              `json:"services_version"`
	UptimeSeconds   *int64              `json:"uptime_seconds,omitempty"`
	MQTT            *mqttclient.Status  `json:"mqtt,omitempty"`
	Storage         map[string]any      `json:"storage"`
	// Features nennt Faehigkeiten, die ueber den Grundbetrieb hinausgehen,
	// mit ihrer Protokollversion. Der Verlauf-Austausch hat daneben eine
	// eigene, ausfuehrliche Ankuendigung unter /api/v1/history/exchange -
	// diese Zeile macht ihn nur dort sichtbar, wo der Betriebszustand
	// ohnehin abgefragt wird.
	Features map[string]int `json:"features"`
}

// nodeAgent is threaded through for Task 6/7 (healthResponse.Node); the
// handler body does not read it yet.
func handleHealth(cache runtimecache.StatusProvider, storageProvider storagehealth.Provider, mqttStatus mqttclient.StatusProvider, nodeAgent *nodeagent.Agent, startedAt time.Time, version string, servicesVersion string, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		status := runtimecache.Status{}
		result := "ok"
		if cache != nil {
			status = cache.Status()
			if status.Degraded() {
				result = "degraded"
			}
		}
		currentTime := now()
		if version == "" {
			version = "dev"
		}
		response := healthResponse{
			Status:          result,
			RuntimeCache:    status,
			Version:         version,
			ServicesVersion: servicesVersion,
		}
		if !startedAt.IsZero() {
			uptime := currentTime.Sub(startedAt)
			if uptime < 0 {
				uptime = 0
			}
			uptimeSeconds := int64(uptime / time.Second)
			response.UptimeSeconds = &uptimeSeconds
		}
		if mqttStatus != nil {
			mqtt := mqttStatus.Status()
			response.MQTT = &mqtt
			if !mqtt.Connected {
				result = "degraded"
			}
		}
		storage := map[string]any{"available": false, "reason": "Speichermedium-Diagnose ist nicht verfügbar"}
		if storageProvider != nil {
			report, err := storageProvider.Check(r.Context())
			if err != nil {
				storage["reason"] = err.Error()
				result = "degraded"
			} else {
				storage = map[string]any{
					"available":  report.Available,
					"medium":     report.Medium,
					"mode":       report.Mode,
					"confidence": report.Confidence,
					"reason":     report.Reason,
				}
				if report.Estimate != nil {
					storage["remaining_label"] = report.Estimate.RemainingLabel
				}
			}
		}
		response.Status = result
		response.Storage = storage
		response.Features = map[string]int{"history_exchange": exchangeProtocolVersion}
		writeJSON(w, response)
	}
}

func handleRuntimeCache(cache runtimecache.StatusProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		if cache == nil {
			writeError(w, http.StatusServiceUnavailable, "runtime_cache_unavailable", "Runtime-Cache ist nicht verfügbar")
			return
		}
		writeJSON(w, cache.Status())
	}
}

// handleDevices returns every device the registry currently knows about,
// with all of their entities' live state (source: "live" once Phase 3 adds
// the source field to the response - Phase 1 always serves the in-memory
// registry, so there is no cache/history fallback yet).
func handleDevices(reg *registry.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeVersionedJSON(w, r, reg.Version(), reg.Snapshot())
	}
}

// liveUpdateInterval reads the SSE polling interval from the (cached, see
// settings.Store.LoadLayout for the same pattern) settings store. Calling
// this from inside the SSE loop keeps a changed setting effective without
// requiring clients to reconnect.
func liveUpdateInterval(store *settings.Store) time.Duration {
	seconds := settings.Default().LiveUpdateIntervalSeconds
	if store != nil {
		if value, err := store.LoadSettings(); err == nil && value.LiveUpdateIntervalSeconds > 0 {
			seconds = value.LiveUpdateIntervalSeconds
		}
	}
	return time.Duration(seconds) * time.Second
}

// eventCache haelt den zuletzt serialisierten SSE-Rumpf samt der
// Registry-Version, aus der er stammt. Ohne ihn wuerde jede offene
// Verbindung bei derselben Aenderung dieselbe Arbeit noch einmal machen: ein
// reg.Snapshot() (Deep-Copy aller Geraete), ein energy.Aggregate() und ein
// Diagnose-Lauf - die Kosten skalierten mit der Zahl der Tabs statt mit der
// Zahl der Aenderungen. Gemessen kostete der Fragment-Weg 0,89 ms je Client
// und Aenderung; hier faellt das einmal fuer alle an.
//
// Der Snapshot wird bewusst nur einmal gezogen und an alle drei Zweige
// weitergereicht.
type eventCache struct {
	mu         sync.Mutex
	version    uint64
	valid      bool
	full       []byte
	delta      []byte
	lastValues map[string]registry.ValueView
}

// eventBody ist der Rumpf hinter "data: ". Fehlt ein Zweig, faellt der
// Client fuer die betroffenen Karten auf den Fragment-Tausch zurueck (siehe
// liveGridPushCovers in dashboard.js) - deshalb "omitempty" statt eines
// leeren Platzhalters.
type eventBody struct {
	Version     uint64                        `json:"version"`
	Energy      any                           `json:"energy,omitempty"`
	Entities    map[string]registry.ValueView `json:"entities"`
	Diagnostics *diagnostics.Summary          `json:"diagnostics,omitempty"`
	// Structure sagt dem Browser, ob sein gerendertes Fragment noch zur
	// Registry passt (siehe registry.StructureFingerprint). Gleich heisst
	// nachziehen, ungleich heisst tauschen - "omitempty" ist hier die
	// sichere Seite: ohne Fingerabdruck tauscht der Client.
	Structure string `json:"structure,omitempty"`
	// StructureCompact deckt fuer die Kompakt-Ansicht des Geraete-Tabs ab,
	// was Structure nicht sieht: die wertabhaengige Zeilenauswahl je
	// compact-card und die Geraete-Ampel (siehe
	// webui.CompactStructureFingerprint). "omitempty" ist die sichere
	// Seite: ohne den Zweig tauscht der Client.
	StructureCompact string `json:"structure_compact,omitempty"`
	// StructureAvailability deckt fuer die *konfigurierte* Kompaktkachel der
	// Uebersicht die Geraete-Ampel ab. Ihre Zeilen stehen fest (entity_refs),
	// also braucht sie StructureCompact nicht - nur diesen Zweig (siehe
	// webui.AvailabilityStructureFingerprint). "omitempty" ist die sichere
	// Seite: ohne den Zweig tauscht der Client.
	StructureAvailability string `json:"structure_availability,omitempty"`
	// EntitiesDelta markiert einen Rumpf, dessen "entities" nur die seit der
	// zuletzt gebauten Version geaenderten Werte traegt. Fehlt das Feld (oder
	// ist es false), enthaelt "entities" alle Werte. "omitempty", damit der
	// volle Rumpf byte-identisch zu frueher bleibt.
	EntitiesDelta bool `json:"entities_delta,omitempty"`
}

// bodies baut den vollen Rumpf (fuer neu verbundene Clients) und den
// Delta-Rumpf (nur die seit der zuletzt gebauten Version geaenderten
// Entitaetswerte, fuer bereits verbundene Clients). Bei gleicher Version
// kommen beide aus dem Cache - das ist nicht nur Sparsamkeit: ein zweiter
// Aufruf darf den Delta nicht gegen sich selbst rechnen, er waere leer.
//
// Der Snapshot wird bewusst nur einmal gezogen und an alle Zweige
// weitergereicht.
func (c *eventCache) bodies(reg *registry.Registry, resolver *energy.Resolver, engine *diagnostics.Engine, version uint64) (full, delta []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.valid && c.version == version {
		return c.full, c.delta
	}

	devices := reg.Snapshot()
	values := registry.ValueViews(devices)
	body := eventBody{
		Version:               version,
		Entities:              values,
		Structure:             registry.StructureFingerprint(devices),
		StructureCompact:      webui.CompactStructureFingerprint(devices),
		StructureAvailability: webui.AvailabilityStructureFingerprint(devices),
	}
	snapshot := energy.Aggregate(devices, resolver, time.Now().UTC())
	body.Energy = snapshot.WithInterpretation(resolver.Interpretation())
	if engine != nil {
		now := time.Now().UTC()
		summary := diagnostics.Summarize(engine.Evaluate(now), engine.Health(now), devices)
		body.Diagnostics = &summary
	}

	fullBytes, err := json.Marshal(body)
	if err != nil {
		return nil, nil
	}

	body.Entities = changedValues(c.lastValues, values)
	body.EntitiesDelta = true
	deltaBytes, err := json.Marshal(body)
	if err != nil {
		return nil, nil
	}

	c.version, c.full, c.delta, c.lastValues, c.valid = version, fullBytes, deltaBytes, values, true
	return c.full, c.delta
}

// changedValues sind die Eintraege aus now, die in previous fehlen oder sich
// unterscheiden. previous == nil (erster Lauf) heisst: alles ist neu, der
// Delta ist dann deckungsgleich mit dem vollen Rumpf. Eine entfernte
// Entitaet steht bewusst nicht drin - ihr Verschwinden aendert den
// StructureFingerprint, und der Client tauscht dann sein Fragment ganz.
func changedValues(previous, now map[string]registry.ValueView) map[string]registry.ValueView {
	changed := make(map[string]registry.ValueView, len(now))
	for id, value := range now {
		if old, ok := previous[id]; !ok || old != value {
			changed[id] = value
		}
	}
	return changed
}

// registryStreamKeepAlive ist der Abstand zwischen zwei Lebenszeichen auf
// einem stillen /api/v1/events-Strom. Ohne sie schliessen Zwischenstellen
// (caddy, tailscale) den Strom nach wenigen Minuten - und der Server merkt
// einen weggebrochenen Client erst beim naechsten echten Schreibversuch,
// bis dahin kostet die tote Verbindung so viel wie eine lebende. Variable
// statt Konstante, damit Tests sie verkuerzen koennen.
var registryStreamKeepAlive = 20 * time.Second

// registryStreamQueueSize ist der Vorlauf je Verbindung im Hub. Vier
// Ruempfe reichen: eine Verbindung, die nicht mitkommt, wird getrennt und
// beginnt nach dem EventSource-Reconnect mit einem frischen Rumpf.
const registryStreamQueueSize = 4

// handleEvents bedient den /api/v1/events-SSE-Strom. Bis hierher fuehrte
// jede Verbindung ihren eigenen time.Ticker und las pro Tick die
// Einstellungen - die Grundlast skalierte mit der Zahl offener Tabs. Jetzt
// teilen sich alle Verbindungen einen Poll-Loop im Hub, der reg.Version()
// beobachtet und den Rumpf einmal je Aenderung baut (siehe registryevents
// und eventCache). Der Loop laeuft nur, solange eine Verbindung offen ist.
func handleEvents(reg *registry.Registry, store *settings.Store, resolver *energy.Resolver, engine *diagnostics.Engine) http.HandlerFunc {
	cache := &eventCache{}
	hub := registryevents.NewHub(
		registryStreamQueueSize,
		reg.Version,
		func(version uint64) (full, delta []byte) {
			return cache.bodies(reg, resolver, engine, version)
		},
		func() time.Duration { return liveUpdateInterval(store) },
	)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming is not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		latest, updates, cancel := hub.Subscribe()
		defer cancel()

		writeEvent := func(payload []byte) bool {
			// Faellt das Marshalling aus, geht nur die Version raus: der
			// Client schaltet dann auf den Fragment-Tausch zurueck, statt mit
			// veralteten Zahlen dazustehen.
			var err error
			if payload != nil {
				_, err = fmt.Fprintf(w, "event: registry\ndata: %s\n\n", payload)
			} else {
				_, err = fmt.Fprintf(w, "event: registry\ndata: {\"version\":%d}\n\n", hub.Version())
			}
			if err != nil {
				return false
			}
			flusher.Flush()
			return true
		}

		if !writeEvent(latest) {
			return
		}

		keepAlive := time.NewTicker(registryStreamKeepAlive)
		defer keepAlive.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case payload, open := <-updates:
				// Kanal zu: der Hub hat diese Verbindung als zu langsam
				// getrennt. Die EventSource verbindet von selbst neu.
				if !open {
					return
				}
				if !writeEvent(payload) {
					return
				}
			case <-keepAlive.C:
				if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}

func handleDiscovery(reg *registry.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, map[string]any{
			"devices":          reg.Snapshot(),
			"discovery_errors": reg.DiscoveryErrors(),
			"duplicate_ids":    reg.DuplicateUniqueIDs(),
		})
	}
}

// handleDiscoverySummary liefert die vier Zahlen, die das Diagnose-Panel aus
// /api/v1/discovery tatsaechlich benutzt: Geraete- und Entitaetenzahl,
// Parserfehler und doppelte unique_ids. Die Vollansicht kostet am Live-System
// 448 KB pro Panel-Oeffnung, von denen alles ausser diesen vier Dingen
// deserialisiert und weggeworfen wird. /api/v1/discovery bleibt daneben
// unveraendert - externe Nutzer und die Discovery-Löschvorschau im Modal brauchen es weiter.
//
// writeVersionedJSON statt writeJSON, damit die ETag-/304-Logik unveraendert
// mitkommt.
func handleDiscoverySummary(reg *registry.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		devices, entities := reg.Counts()
		writeVersionedJSON(w, r, reg.Version(), map[string]any{
			"device_count":     devices,
			"entity_count":     entities,
			"discovery_errors": reg.DiscoveryErrors(),
			"duplicate_ids":    reg.DuplicateUniqueIDs(),
		})
	}
}

func handleTopics(reg *registry.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, reg.Topics())
	}
}

// handleTopicSamples serves the last payload seen per topic. The configuration
// form uses it to suggest JSON keys for foreign topics instead of making the
// user guess them.
func handleTopicSamples(reg *registry.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		if topics := r.URL.Query()["topic"]; len(topics) > 0 {
			writeJSON(w, reg.TopicSamplesFor(topics))
			return
		}
		writeJSON(w, reg.TopicSamples())
	}
}

// handleAutomationNotification serves the last {at, message} document from the
// automation service's last_event topic, already parsed. notifications.js polls
// this instead of the full single-device route: the device route only exposes
// last_message, the slot an availability heartbeat shares with the state
// payload and can overwrite (see registry lastStateMessage vs lastMessage),
// which left the browser JSON.parse failing until the next real event. A 404
// means this instance runs no automation service - notifications.js treats that
// as a permanent, silent state. A 204 means the service is known but has not
// published an event yet.
func handleAutomationNotification(reg *registry.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		device, ok := reg.Get("automation")
		if !ok {
			writeError(w, http.StatusNotFound, "automation_not_found", "Kein Automations-Dienst auf dieser Instanz")
			return
		}
		var stateTopic string
		for _, entity := range device.Entities {
			if entity.ObjectID == "last_event" {
				stateTopic = entity.StateTopic
				break
			}
		}
		if stateTopic == "" {
			writeError(w, http.StatusNotFound, "automation_not_found", "Automations-Dienst ohne last_event-Topic")
			return
		}
		samples := reg.TopicSamplesFor([]string{stateTopic})
		if len(samples) == 0 || samples[0].Payload == "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var document struct {
			At      json.Number `json:"at"`
			Message string      `json:"message"`
		}
		// A payload that is not the expected {at, message} document - e.g. an
		// availability word that reached this topic - is nothing the client can
		// turn into a toast, so it reads the same as "no event yet".
		if err := json.Unmarshal([]byte(samples[0].Payload), &document); err != nil || document.Message == "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, document)
	}
}

// handleDevice returns a single device by ID (device.identifiers[0], or the
// discovery topic's device_id segment as fallback - see
// dashboard/mqtt-topics-und-discovery-format.md).
func handleIgnoredDevices(store *devicefilter.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if store == nil {
			writeJSON(w, []devicefilter.Summary{})
			return
		}
		writeJSON(w, store.Summaries())
	}
}

func handleDevice(reg *registry.Registry, engine *diagnostics.Engine, reloader DeviceReloader, now func() time.Time, history *commandHistory, configs *config.Manager, authManager *auth.Manager, filter *devicefilter.Store, actions DeviceActioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/devices/")
		parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			writeError(w, http.StatusNotFound, "device_not_found", "Gerät wurde nicht gefunden")
			return
		}
		id, err := url.PathUnescape(parts[0])
		if err != nil || id == "" || len(parts) > 3 {
			writeError(w, http.StatusNotFound, "route_not_found", "Route wurde nicht gefunden")
			return
		}
		if len(parts) == 2 && parts[1] == "diagnostics" {
			if r.Method != http.MethodGet {
				methodNotAllowed(w, http.MethodGet)
				return
			}
			if _, ok := reg.Get(id); !ok {
				writeError(w, http.StatusNotFound, "device_not_found", "Gerät wurde nicht gefunden")
				return
			}
			writeJSON(w, engine.Device(id, now()))
			return
		}
		if len(parts) == 2 && parts[1] == "ignore" {
			if r.Method != http.MethodPost {
				methodNotAllowed(w, http.MethodPost)
				return
			}
			if !requireDeviceMutation(w, r, authManager, false, true) {
				return
			}
			if _, ok := reg.Get(id); !ok {
				writeError(w, http.StatusNotFound, "device_not_found", "Gerät wurde nicht gefunden")
				return
			}
			if actions == nil {
				writeError(w, http.StatusServiceUnavailable, "device_actions_unavailable", "Geräteaktionen sind nicht verfügbar")
				return
			}
			if err := actions.IgnoreDevice(id); err != nil {
				writeError(w, http.StatusBadGateway, "device_ignore_failed", err.Error())
				return
			}
			writeJSON(w, map[string]any{"device_id": id, "status": "ignored"})
			return
		}
		if len(parts) == 2 && parts[1] == "unignore" {
			if r.Method != http.MethodPost {
				methodNotAllowed(w, http.MethodPost)
				return
			}
			if !requireDeviceMutation(w, r, authManager, false, true) {
				return
			}
			if filter == nil || !filter.IsIgnored(id) {
				writeError(w, http.StatusNotFound, "device_not_found", "Ignoriertes Gerät wurde nicht gefunden")
				return
			}
			if actions == nil {
				writeError(w, http.StatusServiceUnavailable, "device_actions_unavailable", "Geräteaktionen sind nicht verfügbar")
				return
			}
			if err := actions.UnignoreDevice(id); err != nil {
				writeError(w, http.StatusBadGateway, "device_unignore_failed", err.Error())
				return
			}
			writeJSON(w, map[string]any{"device_id": id, "status": "active_reload_requested"})
			return
		}
		if len(parts) == 3 && parts[1] == "discovery-delete" && parts[2] == "preview" {
			if r.Method != http.MethodGet {
				methodNotAllowed(w, http.MethodGet)
				return
			}
			if !requireDeviceMutation(w, r, authManager, true, false) {
				return
			}
			response, err := deviceDiscoveryPreview(reg, configs, filter, id)
			if err != nil {
				writeError(w, http.StatusNotFound, "device_not_found", err.Error())
				return
			}
			writeJSON(w, response)
			return
		}
		if len(parts) == 2 && parts[1] == "discovery-delete" {
			if r.Method != http.MethodPost {
				methodNotAllowed(w, http.MethodPost)
				return
			}
			if !requireDeviceMutation(w, r, authManager, true, true) {
				return
			}
			var request struct {
				Confirm bool `json:"confirm"`
			}
			if err := decodeBody(r, &request); err != nil || !request.Confirm {
				writeError(w, http.StatusBadRequest, "confirmation_required", "Das ausdrückliche Löschen muss bestätigt werden")
				return
			}
			if _, err := deviceDiscoveryPreview(reg, configs, filter, id); err != nil {
				writeError(w, http.StatusNotFound, "device_not_found", err.Error())
				return
			}
			if actions == nil {
				writeError(w, http.StatusServiceUnavailable, "device_actions_unavailable", "Geräteaktionen sind nicht verfügbar")
				return
			}
			if err := actions.DeleteDeviceDiscovery(id); err != nil {
				writeError(w, http.StatusBadGateway, "discovery_delete_failed", err.Error())
				return
			}
			writeJSON(w, map[string]any{"device_id": id, "status": "discovery_deleted"})
			return
		}
		if len(parts) == 2 && parts[1] == "reload" {
			if r.Method != http.MethodPost {
				methodNotAllowed(w, http.MethodPost)
				return
			}
			// Reload always resets and rebuilds the entire registry (scope:
			// all_devices), so it must not require the requested device to
			// already be present — that is precisely the state a reload is
			// meant to recover from. Gating on reg.Get(id) here would make
			// the button permanently unable to repair an empty registry.
			if reloader == nil {
				writeError(w, http.StatusServiceUnavailable, "reload_unavailable", "MQTT-Registry-Reload ist nicht verfügbar")
				return
			}
			if err := reloader.Reload(); err != nil {
				writeError(w, http.StatusBadGateway, "reload_failed", err.Error())
				return
			}
			writeJSON(w, map[string]any{"device_id": id, "mode": "registry", "scope": "all_devices", "reloaded_at": now()})
			return
		}
		if len(parts) != 1 || r.Method != http.MethodGet {
			if len(parts) == 1 {
				methodNotAllowed(w, http.MethodGet)
				return
			}
			writeError(w, http.StatusNotFound, "route_not_found", "Route wurde nicht gefunden")
			return
		}
		dev, ok := reg.Get(id)
		if !ok {
			writeError(w, http.StatusNotFound, "device_not_found", "Gerät wurde nicht gefunden")
			return
		}
		writeJSON(w, deviceDetailResponse{DeviceView: dev, Warnings: engine.Device(id, now()), CommandActions: history.forDevice(id)})
	}
}

func requireDeviceMutation(w http.ResponseWriter, r *http.Request, manager *auth.Manager, roleRequired, csrfRequired bool) bool {
	if manager == nil {
		writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
		return false
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
		return false
	}
	if roleRequired && !auth.HasRole(user, auth.RoleDeleteDeviceDiscovery) {
		writeError(w, http.StatusForbidden, "device_discovery_forbidden", "Für Discovery-Löschen fehlt die Berechtigung")
		return false
	}
	if csrfRequired {
		cookie, err := r.Cookie(sessionCookieName(isSecureRequest(r)))
		if err != nil || !manager.ValidateCSRF(cookie.Value, r.Header.Get("X-CSRF-Token")) {
			writeError(w, http.StatusForbidden, "csrf_failed", "Sicherheitsprüfung fehlgeschlagen")
			return false
		}
	}
	return true
}

type discoveryDeletePreview struct {
	DeviceID                string             `json:"device_id"`
	DiscoveryTopics         []string           `json:"discovery_topics"`
	ConfigurationReferences []config.Reference `json:"configuration_references"`
}

func deviceDiscoveryPreview(reg *registry.Registry, configs *config.Manager, filter *devicefilter.Store, deviceID string) (discoveryDeletePreview, error) {
	query := config.ReferenceQuery{DeviceID: deviceID}
	result := discoveryDeletePreview{DeviceID: deviceID, DiscoveryTopics: []string{}, ConfigurationReferences: []config.Reference{}}
	if device, ok := reg.Get(deviceID); ok {
		result.DiscoveryTopics = reg.DiscoveryTopics(deviceID)
		for _, entity := range device.Entities {
			query.UniqueIDs = append(query.UniqueIDs, entity.UniqueID)
			query.ObjectIDs = append(query.ObjectIDs, entity.ObjectID)
			query.DiscoveryTopics = append(query.DiscoveryTopics, entity.DiscoveryTopic)
			query.StateTopics = append(query.StateTopics, entity.StateTopic)
			query.AvailabilityTopics = append(query.AvailabilityTopics, entity.AvailabilityTopic)
			for _, availability := range entity.Availability {
				query.AvailabilityTopics = append(query.AvailabilityTopics, availability.Topic)
			}
		}
	} else if filter != nil {
		record, ok := filter.Get(deviceID)
		if !ok {
			return discoveryDeletePreview{}, errors.New("Gerät wurde nicht gefunden")
		}
		for _, discovery := range record.Discoveries {
			result.DiscoveryTopics = append(result.DiscoveryTopics, discovery.Topic)
			query.DiscoveryTopics = append(query.DiscoveryTopics, discovery.Topic)
			query.UniqueIDs = append(query.UniqueIDs, discovery.UniqueID)
			query.ObjectIDs = append(query.ObjectIDs, discovery.ObjectID)
			query.StateTopics = append(query.StateTopics, discovery.StateTopics...)
		}
	} else {
		return discoveryDeletePreview{}, errors.New("Gerät wurde nicht gefunden")
	}
	if configs != nil {
		references, err := configs.FindReferences(query)
		if err != nil {
			return discoveryDeletePreview{}, err
		}
		result.ConfigurationReferences = references
	}
	sort.Strings(result.DiscoveryTopics)
	return result, nil
}

type deviceDetailResponse struct {
	registry.DeviceView
	Warnings       []diagnostics.Warning `json:"warnings"`
	CommandActions []CommandAction       `json:"command_actions"`
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	if err := enc.Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeVersionedJSON(w http.ResponseWriter, r *http.Request, version uint64, value any) {
	etag := fmt.Sprintf("\"registry-%d\"", version)
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, value)
}

func handleConfigurations(manager *config.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		documents, err := manager.Scan()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan_failed", err.Error())
			return
		}
		writeJSON(w, documents)
	}
}

// automationTestTopic ist bewusst eine Konstante und nicht vom Aufrufer
// waehlbar: der Endpunkt ist die dritte, eng umrissene Ausnahme von der
// read-only-Regel (siehe dashboard/AGENTS.md) und darf ausschliesslich den
// Automations-Dienst ansprechen. Jede fachliche Pruefung - Regel existiert,
// Index gueltig, Topic erlaubt - macht der Dienst.
const automationTestTopic = "outstation/automation/test/set"

func handleAutomationTest(publisher CommandPublisher, authManager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if authManager == nil {
			writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
			return
		}
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
			return
		}
		if !auth.HasRole(user, auth.RoleAutomations) {
			writeError(w, http.StatusForbidden, "automations_forbidden", "Für Automations-Tests fehlt die Berechtigung")
			return
		}
		cookie, err := r.Cookie(sessionCookieName(isSecureRequest(r)))
		if err != nil || !authManager.ValidateCSRF(cookie.Value, r.Header.Get("X-CSRF-Token")) {
			writeError(w, http.StatusForbidden, "csrf_failed", "Sicherheitsprüfung fehlgeschlagen")
			return
		}
		if publisher == nil {
			writeError(w, http.StatusServiceUnavailable, "commands_unavailable", "MQTT-Befehle sind nicht verfügbar")
			return
		}
		var request struct {
			RuleID      string `json:"rule_id"`
			ActionIndex *int   `json:"action_index"`
		}
		if err := decodeBody(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		if request.RuleID == "" || request.ActionIndex == nil || *request.ActionIndex < 0 {
			writeError(w, http.StatusBadRequest, "invalid_test_request", "rule_id und action_index sind erforderlich")
			return
		}
		payload, err := json.Marshal(map[string]any{"rule_id": request.RuleID, "action_index": *request.ActionIndex})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "encode_failed", err.Error())
			return
		}
		if err := publisher.Publish(automationTestTopic, string(payload)); err != nil {
			writeError(w, http.StatusBadGateway, "command_publish_failed", err.Error())
			return
		}
		writeJSON(w, map[string]string{"status": "requested"})
	}
}

// automationHistoryFileName ist bewusst eine Konstante, kein Parameter: der
// Automations-Dienst schreibt exakt diese Datei in dasselbe geteilte
// Verzeichnis, in dem auch automation_rules.json liegt (siehe
// AUTOMATION_HISTORY_FILE in services/automation/automation_mqtt.py). Reines
// Lesen - keine neue Schreib-Ausnahme von der read-only-Regel.
const automationHistoryFileName = "automation_history.json"

func handleAutomationHistory(configs *config.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		ruleID := strings.TrimPrefix(r.URL.Path, "/api/v1/automations/history/")
		if ruleID == "" || strings.Contains(ruleID, "/") {
			http.NotFound(w, r)
			return
		}
		data, err := os.ReadFile(filepath.Join(configs.Dir(), automationHistoryFileName))
		if errors.Is(err, fs.ErrNotExist) {
			writeJSON(w, []json.RawMessage{})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "history_read_failed", err.Error())
			return
		}
		var byRule map[string]json.RawMessage
		if err := json.Unmarshal(data, &byRule); err != nil {
			writeError(w, http.StatusBadGateway, "history_corrupt", err.Error())
			return
		}
		events, ok := byRule[ruleID]
		if !ok {
			writeJSON(w, []json.RawMessage{})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(events)
	}
}

func handleConfiguration(manager *config.Manager, authManager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/api/v1/configurations/")
		name = strings.TrimSuffix(name, "/")
		if name == "" {
			http.NotFound(w, r)
			return
		}
		parts := strings.Split(name, "/")
		if len(parts) == 2 && parts[1] == "schema" && r.Method == http.MethodGet {
			data, err := manager.ReadSchema(parts[0])
			if err != nil {
				writeError(w, http.StatusNotFound, "schema_not_found", err.Error())
				return
			}
			writeJSON(w, json.RawMessage(data))
			return
		}
		if len(parts) == 3 && parts[1] == "revisions" {
			data, err := manager.ReadRevision(parts[0], parts[2])
			if err != nil {
				writeError(w, http.StatusNotFound, "revision_not_found", err.Error())
				return
			}
			writeJSON(w, json.RawMessage(data))
			return
		}
		if len(parts) == 2 && parts[1] == "revisions" && r.Method == http.MethodGet {
			revisions, err := manager.Revisions(parts[0])
			if err != nil {
				writeError(w, http.StatusNotFound, "configuration_not_found", err.Error())
				return
			}
			writeJSON(w, revisions)
			return
		}
		if len(parts) == 2 && parts[1] == "restore" && r.Method == http.MethodPost {
			var request struct {
				Revision string `json:"revision"`
			}
			if err := decodeBody(r, &request); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
				return
			}
			document, err := manager.Restore(parts[0], request.Revision)
			if err != nil {
				writeError(w, http.StatusBadRequest, "restore_rejected", err.Error())
				return
			}
			writeJSON(w, document)
			return
		}
		if len(parts) == 2 && parts[1] == "reload" && r.Method == http.MethodPost {
			document, err := manager.Reload(parts[0])
			if err != nil {
				writeJSONStatus(w, http.StatusBadGateway, map[string]any{
					"code":     "reload_failed",
					"message":  err.Error(),
					"document": document,
				})
				return
			}
			writeJSON(w, document)
			return
		}
		if name == "automation_rules" && r.Method == http.MethodPut {
			if authManager == nil {
				writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
				return
			}
			user, ok := auth.UserFromContext(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
				return
			}
			if !auth.HasRole(user, auth.RoleAutomations) {
				writeError(w, http.StatusForbidden, "automations_forbidden", "Für Automations-Regeln fehlt die Berechtigung")
				return
			}
			cookie, err := r.Cookie(sessionCookieName(isSecureRequest(r)))
			if err != nil || !authManager.ValidateCSRF(cookie.Value, r.Header.Get("X-CSRF-Token")) {
				writeError(w, http.StatusForbidden, "csrf_failed", "Sicherheitsprüfung fehlgeschlagen")
				return
			}
		}
		switch r.Method {
		case http.MethodGet:
			data, err := manager.Read(name)
			if err != nil {
				writeError(w, http.StatusNotFound, "configuration_not_found", err.Error())
				return
			}
			writeJSON(w, json.RawMessage(data))
		case http.MethodPut:
			data, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
				return
			}
			document, err := manager.Save(name, data)
			if err != nil {
				writeError(w, http.StatusBadRequest, "configuration_rejected", err.Error())
				return
			}
			writeJSON(w, document)
		default:
			methodNotAllowed(w)
		}
	}
}

func handleLayout(store *settings.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			value, err := store.LoadLayout()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "layout_invalid", err.Error())
				return
			}
			// Der Kartentyp-Katalog reist mit dem Layout mit: der Editor
			// laedt diesen Endpunkt ohnehin, ein zweiter Rundlauf waere
			// unnoetig. Reine Ausgabe - PUT nimmt das Feld nicht entgegen,
			// und das Layout-Schema kennt es nicht.
			writeJSON(w, struct {
				settings.Layout
				CardTypes map[string]settings.CardType `json:"card_types"`
			}{Layout: value, CardTypes: settings.CardCatalog()})
		case http.MethodPut:
			var value settings.Layout
			if err := decodeBody(r, &value); err != nil {
				writeError(w, http.StatusBadRequest, "layout_rejected", err.Error())
				return
			}
			if err := store.SaveLayout(value); err != nil {
				writeError(w, http.StatusBadRequest, "layout_rejected", err.Error())
				return
			}
			writeJSON(w, value)
		default:
			methodNotAllowed(w)
		}
	}
}

func handleLayoutRevision(store *settings.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/layout/")
		parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
		if len(parts) == 1 && parts[0] == "revisions" && r.Method == http.MethodGet {
			revisions, err := store.LayoutRevisions()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "layout_revisions_failed", err.Error())
				return
			}
			writeJSON(w, revisions)
			return
		}
		if len(parts) == 2 && parts[0] == "revisions" && r.Method == http.MethodGet {
			data, err := store.ReadLayoutRevision(parts[1])
			if err != nil {
				writeError(w, http.StatusNotFound, "layout_revision_not_found", err.Error())
				return
			}
			writeJSON(w, json.RawMessage(data))
			return
		}
		if len(parts) == 1 && parts[0] == "restore" && r.Method == http.MethodPost {
			var request struct {
				Revision string `json:"revision"`
			}
			if err := decodeBody(r, &request); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
				return
			}
			value, err := store.RestoreLayout(request.Revision)
			if err != nil {
				writeError(w, http.StatusBadRequest, "layout_restore_rejected", err.Error())
				return
			}
			writeJSON(w, value)
			return
		}
		methodNotAllowed(w)
	}
}

func handleSettingsRevision(store *settings.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/settings/")
		parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
		if len(parts) == 1 && parts[0] == "revisions" && r.Method == http.MethodGet {
			revisions, err := store.SettingsRevisions()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "settings_revisions_failed", err.Error())
				return
			}
			writeJSON(w, revisions)
			return
		}
		if len(parts) == 2 && parts[0] == "revisions" && r.Method == http.MethodGet {
			data, err := store.ReadSettingsRevision(parts[1])
			if err != nil {
				writeError(w, http.StatusNotFound, "settings_revision_not_found", err.Error())
				return
			}
			writeJSON(w, json.RawMessage(data))
			return
		}
		if len(parts) == 1 && parts[0] == "restore" && r.Method == http.MethodPost {
			var request struct {
				Revision string `json:"revision"`
			}
			if err := decodeBody(r, &request); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
				return
			}
			value, err := store.RestoreSettings(request.Revision)
			if err != nil {
				writeError(w, http.StatusBadRequest, "settings_restore_rejected", err.Error())
				return
			}
			writeJSON(w, value)
			return
		}
		methodNotAllowed(w)
	}
}

func handleEnergyRevision(store *settings.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/energy/")
		parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
		if len(parts) == 1 && parts[0] == "revisions" && r.Method == http.MethodGet {
			revisions, err := store.EnergyRevisions()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "energy_revisions_failed", err.Error())
				return
			}
			writeJSON(w, revisions)
			return
		}
		if len(parts) == 2 && parts[0] == "revisions" && r.Method == http.MethodGet {
			data, err := store.ReadEnergyRevision(parts[1])
			if err != nil {
				writeError(w, http.StatusNotFound, "energy_revision_not_found", err.Error())
				return
			}
			writeJSON(w, json.RawMessage(data))
			return
		}
		if len(parts) == 1 && parts[0] == "restore" && r.Method == http.MethodPost {
			var request struct {
				Revision string `json:"revision"`
			}
			if err := decodeBody(r, &request); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
				return
			}
			value, err := store.RestoreEnergy(request.Revision)
			if err != nil {
				writeError(w, http.StatusBadRequest, "energy_restore_rejected", err.Error())
				return
			}
			writeJSON(w, value)
			return
		}
		methodNotAllowed(w)
	}
}

func handleDeviceMap(store *settings.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			value, err := store.LoadDeviceMap()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "device_map_invalid", err.Error())
				return
			}
			writeJSON(w, value)
		case http.MethodPut:
			var value settings.DeviceMap
			if err := decodeBody(r, &value); err != nil {
				writeError(w, http.StatusBadRequest, "device_map_rejected", err.Error())
				return
			}
			if err := store.SaveDeviceMap(value); err != nil {
				writeError(w, http.StatusBadRequest, "device_map_rejected", err.Error())
				return
			}
			writeJSON(w, value)
		default:
			methodNotAllowed(w)
		}
	}
}

func handleDeviceMapSub(store *settings.Store, reg *registry.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/device/map/")
		parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
		if len(parts) == 1 && parts[0] == "revisions" && r.Method == http.MethodGet {
			revisions, err := store.DeviceMapRevisions()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "device_map_revisions_failed", err.Error())
				return
			}
			writeJSON(w, revisions)
			return
		}
		if len(parts) == 2 && parts[0] == "revisions" && r.Method == http.MethodGet {
			data, err := store.ReadDeviceMapRevision(parts[1])
			if err != nil {
				writeError(w, http.StatusNotFound, "device_map_revision_not_found", err.Error())
				return
			}
			writeJSON(w, json.RawMessage(data))
			return
		}
		if len(parts) == 1 && parts[0] == "restore" && r.Method == http.MethodPost {
			var request struct {
				Revision string `json:"revision"`
			}
			if err := decodeBody(r, &request); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
				return
			}
			value, err := store.RestoreDeviceMap(request.Revision)
			if err != nil {
				writeError(w, http.StatusBadRequest, "device_map_restore_rejected", err.Error())
				return
			}
			applyRelationOverrides(reg, value)
			writeJSON(w, value)
			return
		}
		if len(parts) == 1 && parts[0] == "relations" && r.Method == http.MethodPost {
			handleCreateDeviceMapRelation(store, reg, w, r)
			return
		}
		if len(parts) == 2 && parts[0] == "relations" && r.Method == http.MethodDelete {
			handleDeleteDeviceMapRelation(store, reg, w, parts[1])
			return
		}
		methodNotAllowed(w)
	}
}

// handleCreateDeviceMapRelation applies a manually created device relation
// (e.g. "connect as sub-device") on top of the via_device-derived tree.
// It rejects self-references, unknown device IDs and relations that would
// create a cycle before persisting anything.
func handleCreateDeviceMapRelation(store *settings.Store, reg *registry.Registry, w http.ResponseWriter, r *http.Request) {
	var request struct {
		ChildID  string `json:"child_id"`
		ParentID string `json:"parent_id"`
		Kind     string `json:"kind"`
	}
	if err := decodeBody(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if request.Kind == "" {
		request.Kind = "via_device"
	}
	if request.ChildID == "" || request.ParentID == "" {
		writeError(w, http.StatusBadRequest, "invalid_relation", "child_id and parent_id are required")
		return
	}
	if request.ChildID == request.ParentID {
		writeError(w, http.StatusBadRequest, "relation_self_reference", "a device cannot be its own parent")
		return
	}

	devices := reg.Snapshot()
	deviceExists := make(map[string]bool, len(devices))
	parentOf := make(map[string]string, len(devices))
	for _, device := range devices {
		deviceExists[device.ID] = true
		if device.ViaDevice != "" {
			parentOf[device.ID] = device.ViaDevice
		}
	}
	if !deviceExists[request.ChildID] || !deviceExists[request.ParentID] {
		writeError(w, http.StatusNotFound, "unknown_device", "child_id or parent_id does not refer to a known device")
		return
	}

	deviceMap, err := store.LoadDeviceMap()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "device_map_invalid", err.Error())
		return
	}
	for _, edge := range deviceMap.Edges {
		parentOf[edge.ChildID] = edge.ParentID
	}
	if relationCreatesCycle(parentOf, request.ChildID, request.ParentID) {
		writeError(w, http.StatusBadRequest, "relation_cycle", "this relation would create a cycle")
		return
	}

	id, err := newRelationID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "relation_id_failed", err.Error())
		return
	}
	override := settings.RelationOverride{
		ID:        id,
		ChildID:   request.ChildID,
		ParentID:  request.ParentID,
		Kind:      request.Kind,
		CreatedAt: time.Now().UTC(),
	}
	deviceMap.Edges = append(deviceMap.Edges, override)
	if err := store.SaveDeviceMap(deviceMap); err != nil {
		writeError(w, http.StatusBadRequest, "device_map_rejected", err.Error())
		return
	}
	applyRelationOverrides(reg, deviceMap)
	writeJSONStatus(w, http.StatusCreated, override)
}

func handleDeleteDeviceMapRelation(store *settings.Store, reg *registry.Registry, w http.ResponseWriter, id string) {
	deviceMap, err := store.LoadDeviceMap()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "device_map_invalid", err.Error())
		return
	}
	kept := make([]settings.RelationOverride, 0, len(deviceMap.Edges))
	found := false
	for _, edge := range deviceMap.Edges {
		if edge.ID == id {
			found = true
			continue
		}
		kept = append(kept, edge)
	}
	if !found {
		writeError(w, http.StatusNotFound, "relation_not_found", "no relation override with this id")
		return
	}
	deviceMap.Edges = kept
	if err := store.SaveDeviceMap(deviceMap); err != nil {
		writeError(w, http.StatusBadRequest, "device_map_rejected", err.Error())
		return
	}
	applyRelationOverrides(reg, deviceMap)
	w.WriteHeader(http.StatusNoContent)
}

// relationCreatesCycle reports whether adding childID -> parentID to parentOf
// (child device ID -> its parent device ID) would create a cycle, i.e.
// whether parentID already descends from childID.
func relationCreatesCycle(parentOf map[string]string, childID, parentID string) bool {
	visited := map[string]bool{}
	for cursor := parentID; cursor != ""; cursor = parentOf[cursor] {
		if cursor == childID {
			return true
		}
		if visited[cursor] {
			return false
		}
		visited[cursor] = true
	}
	return false
}

func applyRelationOverrides(reg *registry.Registry, deviceMap settings.DeviceMap) {
	overrides := make([]registry.RelationOverride, 0, len(deviceMap.Edges))
	for _, edge := range deviceMap.Edges {
		overrides = append(overrides, registry.RelationOverride{ID: edge.ID, ChildID: edge.ChildID, ParentID: edge.ParentID, Kind: edge.Kind})
	}
	reg.SetRelationOverrides(overrides)
}

func newRelationID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "relation-" + hex.EncodeToString(value), nil
}

// handleSettings guards live_update_interval_seconds behind the
// tune_live_updates role: changing it requires the role, but every other
// settings field remains writable by any authenticated user, matching the
// endpoint's existing behavior.
func handleSettings(store *settings.Store, authManager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			value, err := store.LoadSettings()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "settings_invalid", err.Error())
				return
			}
			writeJSON(w, value)
		case http.MethodPut:
			var value settings.Settings
			if err := decodeBody(r, &value); err != nil {
				writeError(w, http.StatusBadRequest, "settings_rejected", err.Error())
				return
			}
			current, err := store.LoadSettings()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "settings_invalid", err.Error())
				return
			}
			if value.LiveUpdateIntervalSeconds != current.LiveUpdateIntervalSeconds {
				if authManager == nil {
					writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
					return
				}
				user, ok := auth.UserFromContext(r.Context())
				if !ok {
					writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
					return
				}
				if !auth.HasRole(user, auth.RoleTuneLiveUpdates) {
					writeError(w, http.StatusForbidden, "live_update_interval_forbidden", "Für das Anpassen des Live-Update-Intervalls fehlt die Berechtigung")
					return
				}
			}
			if err := store.SaveSettings(value); err != nil {
				writeError(w, http.StatusBadRequest, "settings_rejected", err.Error())
				return
			}
			writeJSON(w, value)
		default:
			methodNotAllowed(w)
		}
	}
}

func handleTinyTuyaDevices(probe tinytuya.Prober, credentialStore *tinytuya.CredentialStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if probe == nil {
			writeTinyTuyaError(w, http.StatusNotImplemented, "tiny_tuya_unavailable", errors.New("Der TinyTuya-Helper ist nicht konfiguriert"))
			return
		}
		var request tinytuya.CloudRequest
		if err := decodeBody(r, &request); err != nil {
			writeTinyTuyaError(w, http.StatusBadRequest, "tiny_tuya_rejected", err)
			return
		}
		request, err := resolveTinyTuyaCloudRequest(credentialStore, request)
		if err != nil {
			writeTinyTuyaError(w, http.StatusBadRequest, "tiny_tuya_rejected", err)
			return
		}
		devices, err := probe.Devices(r.Context(), request)
		if err != nil {
			writeTinyTuyaError(w, http.StatusBadGateway, "tiny_tuya_cloud_failed", err)
			return
		}
		writeJSON(w, devices)
	}
}

type tinyTuyaCredentialMetadata struct {
	Configured   bool   `json:"configured"`
	Region       string `json:"region,omitempty"`
	AccessIDHint string `json:"access_id_hint,omitempty"`
}

func handleTinyTuyaCredentials(store *tinytuya.CredentialStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			writeJSON(w, tinyTuyaCredentialMetadata{})
			return
		}
		switch r.Method {
		case http.MethodGet:
			credentials, err := store.Load()
			if errors.Is(err, os.ErrNotExist) {
				writeJSON(w, tinyTuyaCredentialMetadata{})
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "tiny_tuya_credentials_failed", err.Error())
				return
			}
			writeJSON(w, tinyTuyaCredentialMetadata{Configured: true, Region: credentials.Region, AccessIDHint: maskAccessID(credentials.AccessID)})
		case http.MethodPost:
			var credentials tinytuya.CloudRequest
			if err := decodeBody(r, &credentials); err != nil {
				writeError(w, http.StatusBadRequest, "tiny_tuya_rejected", err.Error())
				return
			}
			if err := store.Save(tinytuya.Credentials{Region: credentials.Region, AccessID: credentials.AccessID, AccessSecret: credentials.AccessSecret}); err != nil {
				writeError(w, http.StatusBadRequest, "tiny_tuya_rejected", err.Error())
				return
			}
			writeJSON(w, tinyTuyaCredentialMetadata{Configured: true, Region: credentials.Region, AccessIDHint: maskAccessID(credentials.AccessID)})
		default:
			methodNotAllowed(w)
		}
	}
}

func resolveTinyTuyaCloudRequest(store *tinytuya.CredentialStore, request tinytuya.CloudRequest) (tinytuya.CloudRequest, error) {
	if request.AccessSecret != "" {
		return request, nil
	}
	newAccessIDError := errors.New("für eine neue Access ID muss auch das Access Secret eingegeben werden")
	if store == nil {
		if request.AccessID != "" {
			return request, newAccessIDError
		}
		return request, errors.New("TinyTuya-Zugangsdaten sind nicht konfiguriert")
	}
	credentials, err := store.Load()
	if errors.Is(err, os.ErrNotExist) {
		if request.AccessID != "" {
			return request, newAccessIDError
		}
		return request, errors.New("TinyTuya-Zugangsdaten sind noch nicht gespeichert")
	}
	if err != nil {
		return request, err
	}
	// A populated Access ID field (browser autofill, or left over from a
	// previous query in the same session) must not block reuse of the saved
	// secret. Only reject it when the user really typed a different ID.
	if request.AccessID != "" && request.AccessID != credentials.AccessID {
		return request, newAccessIDError
	}
	return tinytuya.CloudRequest{Region: credentials.Region, AccessID: credentials.AccessID, AccessSecret: credentials.AccessSecret}, nil
}

func maskAccessID(value string) string {
	if len(value) <= 4 {
		return strings.Repeat("*", len(value))
	}
	return strings.Repeat("*", len(value)-4) + value[len(value)-4:]
}

func handleTinyTuyaStatus(probe tinytuya.Prober) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if probe == nil {
			writeTinyTuyaError(w, http.StatusNotImplemented, "tiny_tuya_unavailable", errors.New("Der TinyTuya-Helper ist nicht konfiguriert"))
			return
		}
		var request tinytuya.StatusRequest
		if err := decodeBody(r, &request); err != nil {
			writeTinyTuyaError(w, http.StatusBadRequest, "tiny_tuya_rejected", err)
			return
		}
		status, err := probe.Status(r.Context(), request)
		if err != nil {
			writeTinyTuyaError(w, http.StatusBadGateway, "tiny_tuya_status_failed", err)
			return
		}
		writeJSON(w, status)
	}
}

type tinyTuyaConfigRequest struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	DeviceID   string            `json:"device_id"`
	LocalKey   string            `json:"local_key"`
	IP         string            `json:"ip"`
	Version    float64           `json:"version"`
	DeviceType string            `json:"device_type"`
	Datapoints map[string]string `json:"datapoints"`
}

func handleTinyTuyaConfigure(manager *config.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if manager == nil {
			writeError(w, http.StatusNotImplemented, "tiny_tuya_unavailable", "TinyTuya-Konfiguration ist nicht verfügbar")
			return
		}
		var request tinyTuyaConfigRequest
		if err := decodeBody(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "tiny_tuya_rejected", err.Error())
			return
		}
		if err := validateTinyTuyaConfig(request); err != nil {
			writeError(w, http.StatusBadRequest, "tiny_tuya_rejected", err.Error())
			return
		}
		data, err := mergeTinyTuyaConfig(manager, request)
		if err != nil {
			writeError(w, http.StatusBadRequest, "tiny_tuya_rejected", err.Error())
			return
		}
		document, err := manager.Save("tuya_devices", data)
		if err != nil {
			writeError(w, http.StatusBadRequest, "tiny_tuya_rejected", err.Error())
			return
		}
		writeJSON(w, map[string]any{
			"document":      document,
			"reload_failed": document.ReloadFailed,
			"reload_error":  document.ReloadError,
		})
	}
}

func validateTinyTuyaConfig(request tinyTuyaConfigRequest) error {
	values := []string{request.ID, request.Name, request.DeviceID, request.LocalKey, request.IP, request.DeviceType}
	for _, value := range values {
		if strings.TrimSpace(value) == "" || strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
			return errors.New("alle Tuya-Konfigurationsfelder müssen ausgefüllt sein")
		}
	}
	if request.Version < 3 {
		return errors.New("die Protokollversion muss mindestens 3.0 sein")
	}
	if strings.TrimSpace(request.Datapoints["switch"]) == "" {
		return errors.New("datapoints.switch muss ausgefüllt sein")
	}
	return nil
}

func mergeTinyTuyaConfig(manager *config.Manager, request tinyTuyaConfigRequest) ([]byte, error) {
	data, err := manager.Read("tuya_devices")
	if err != nil {
		return nil, err
	}
	var devices []map[string]any
	if err := json.Unmarshal(data, &devices); err != nil {
		return nil, errors.New("tuya_devices.json enthält ungültiges JSON")
	}
	updated := map[string]any{
		"id": request.ID, "name": request.Name, "device_id": request.DeviceID,
		"local_key": request.LocalKey, "ip": request.IP, "version": request.Version,
		"device_type": request.DeviceType, "datapoints": request.Datapoints,
	}
	found := false
	for index, device := range devices {
		if fmt.Sprint(device["id"]) == request.ID || fmt.Sprint(device["device_id"]) == request.DeviceID {
			devices[index] = updated
			found = true
			break
		}
	}
	if !found {
		devices = append(devices, updated)
	}
	return json.MarshalIndent(devices, "", "  ")
}

func decodeBody(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 2<<20))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}
func methodNotAllowed(w http.ResponseWriter, allowed ...string) {
	if len(allowed) == 0 {
		allowed = []string{http.MethodGet, http.MethodPut}
	}
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method is not supported")
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSONStatus(w, status, map[string]any{"code": code, "message": message})
}

func writeTinyTuyaError(w http.ResponseWriter, status int, code string, err error) {
	message := err.Error()
	writeJSONStatus(w, status, map[string]any{"code": code, "message": message, "tool_output": message})
}
func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
