package httpapi

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/mqttbridge"
	"github.com/Developer-Simon/energy-node-dashboard/internal/mqttclient"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
	"github.com/Developer-Simon/energy-node-dashboard/internal/systemactions"
)

// BridgeWatcher is implemented by *mqttclient.Client. It is its own
// interface (like MQTTReconfigurer) so tests can supply a fake without a
// real Paho connection.
type BridgeWatcher interface {
	SetBridgeWatch(remoteClientID string)
	BridgeStatus() mqttclient.BridgeConnectionState
}

const defaultBridgeTargetPath = "/etc/mosquitto/conf.d/bridge.conf"
const bridgeStagedFileName = "mosquitto-bridge.staged.conf"

// bridgeApplyRecord is the "last apply" (Zeit, Benutzer, Ergebnis) shown by
// GET /api/v1/mqtt/bridge/status. It is in-memory only, same as
// mqttclient.Status - no durable history is kept, consistent with
// AGENTS.md's exclusion of server-side history.
type bridgeApplyRecord struct {
	At    time.Time `json:"at"`
	User  string    `json:"user,omitempty"`
	OK    bool      `json:"ok"`
	Error string    `json:"error,omitempty"`
}

type bridgeApplyState struct {
	mu   sync.Mutex
	last *bridgeApplyRecord
}

func newBridgeApplyState() *bridgeApplyState { return &bridgeApplyState{} }

func (s *bridgeApplyState) record(rec bridgeApplyRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = &rec
}

func (s *bridgeApplyState) get() *bridgeApplyRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

type bridgeTopicPayload struct {
	Pattern   string `json:"pattern"`
	Direction string `json:"direction"`
	QoS       int    `json:"qos"`
	Comment   string `json:"comment,omitempty"`
}

// bridgeConnectionPayload is the flattened, single-connection shape the UI
// and API exchange. settings.BridgeConfig stores a list (see its doc
// comment on why), but exactly one connection is supported today, so the
// HTTP layer hides the list from callers.
type bridgeConnectionPayload struct {
	Configured         bool                 `json:"configured"`
	Enabled            bool                 `json:"enabled"`
	Name               string               `json:"name"`
	Address            string               `json:"address"`
	Port               int                  `json:"port"`
	RemoteClientID     string               `json:"remote_client_id"`
	RemoteUsername     string               `json:"remote_username,omitempty"`
	Topics             []bridgeTopicPayload `json:"topics"`
	TryPrivate         bool                 `json:"try_private"`
	StartTypeAuto      bool                 `json:"start_type_auto"`
	RestartTimeout     int                  `json:"restart_timeout"`
	KeepaliveSeconds   int                  `json:"keepalive_seconds"`
	CleanSession       bool                 `json:"cleansession"`
	PasswordConfigured bool                 `json:"password_configured"`
	AddressWarning     string               `json:"address_warning,omitempty"`
	Preview            string               `json:"preview,omitempty"`
}

func bridgeConnectionToPayload(value settings.BridgeConnection) bridgeConnectionPayload {
	topics := make([]bridgeTopicPayload, len(value.Topics))
	for i, topic := range value.Topics {
		topics[i] = bridgeTopicPayload{Pattern: topic.Pattern, Direction: topic.Direction, QoS: topic.QoS, Comment: topic.Comment}
	}
	return bridgeConnectionPayload{
		Enabled:          value.Enabled,
		Name:             value.Name,
		Address:          value.Address,
		Port:             value.Port,
		RemoteClientID:   value.RemoteClientID,
		RemoteUsername:   value.RemoteUsername,
		Topics:           topics,
		TryPrivate:       value.TryPrivate,
		StartTypeAuto:    value.StartTypeAuto,
		RestartTimeout:   value.RestartTimeout,
		KeepaliveSeconds: value.KeepaliveSeconds,
		CleanSession:     value.CleanSession,
	}
}

func bridgeConnectionFromPayload(value bridgeConnectionPayload) settings.BridgeConnection {
	topics := make([]settings.BridgeTopic, len(value.Topics))
	for i, topic := range value.Topics {
		topics[i] = settings.BridgeTopic{Pattern: topic.Pattern, Direction: topic.Direction, QoS: topic.QoS, Comment: topic.Comment}
	}
	return settings.BridgeConnection{
		Enabled:          value.Enabled,
		Name:             value.Name,
		Address:          value.Address,
		Port:             value.Port,
		RemoteClientID:   value.RemoteClientID,
		RemoteUsername:   value.RemoteUsername,
		Topics:           topics,
		TryPrivate:       value.TryPrivate,
		StartTypeAuto:    value.StartTypeAuto,
		RestartTimeout:   value.RestartTimeout,
		KeepaliveSeconds: value.KeepaliveSeconds,
		CleanSession:     value.CleanSession,
	}
}

func loadBridgeConnection(store *settings.Store) (settings.BridgeConnection, bool, error) {
	cfg, err := store.LoadBridge()
	if err != nil {
		return settings.BridgeConnection{}, false, err
	}
	if len(cfg.Connections) == 0 {
		return settings.BridgeConnection{}, false, nil
	}
	return cfg.Connections[0], true, nil
}

func bridgeCredentialsConfigured(credentials *mqttclient.CredentialStore) bool {
	if credentials == nil {
		return false
	}
	creds, err := credentials.Load()
	return err == nil && creds.Password != ""
}

func bridgeCredentialsPassword(credentials *mqttclient.CredentialStore) string {
	if credentials == nil {
		return ""
	}
	creds, err := credentials.Load()
	if err != nil {
		return ""
	}
	return creds.Password
}

// handleBridgeConfig serves the stored Mosquitto bridge configuration
// (bridge.json). GET requires mqtt_config and HTTPS (the preview includes
// topic patterns and the remote address, worth gating like the rest of the
// bridge surface); PUT additionally requires CSRF. PUT never touches
// /etc/mosquitto - see POST /api/v1/mqtt/bridge/apply for that.
func handleBridgeConfig(store *settings.Store, credentials *mqttclient.CredentialStore, authManager *auth.Manager, watcher BridgeWatcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if !requireRole(w, r, authManager, auth.RoleMQTTConfig, "bridge_forbidden", "Für die Bridge-Konfiguration fehlt die Berechtigung") {
				return
			}
			if !requireHTTPS(w, r) {
				return
			}
			connection, configured, err := loadBridgeConnection(store)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "bridge_invalid", err.Error())
				return
			}
			payload := bridgeConnectionToPayload(connection)
			payload.Configured = configured
			payload.PasswordConfigured = bridgeCredentialsConfigured(credentials)
			if configured {
				payload.AddressWarning = settings.BridgeAddressWarning(connection.Address)
				user, _ := auth.UserFromContext(r.Context())
				if rendered, err := mqttbridge.Render(mqttbridge.Input{
					Connection: connection,
					Password:   bridgeCredentialsPassword(credentials),
					Mask:       true,
					RenderedBy: user.Username,
					RenderedAt: time.Now(),
				}); err == nil {
					payload.Preview = rendered
				}
			}
			writeJSON(w, payload)
		case http.MethodPut:
			if !requireRole(w, r, authManager, auth.RoleMQTTConfig, "bridge_forbidden", "Für die Bridge-Konfiguration fehlt die Berechtigung") {
				return
			}
			if !requireHTTPS(w, r) {
				return
			}
			if !requireCSRF(w, r, authManager) {
				return
			}
			var body bridgeConnectionPayload
			if err := decodeBody(r, &body); err != nil {
				writeError(w, http.StatusBadRequest, "bridge_rejected", err.Error())
				return
			}
			connection := bridgeConnectionFromPayload(body)
			if err := store.SaveBridge(settings.BridgeConfig{Connections: []settings.BridgeConnection{connection}}); err != nil {
				writeError(w, http.StatusBadRequest, "bridge_rejected", err.Error())
				return
			}
			if watcher != nil {
				watcher.SetBridgeWatch(connection.RemoteClientID)
			}
			response := bridgeConnectionToPayload(connection)
			response.Configured = true
			response.PasswordConfigured = bridgeCredentialsConfigured(credentials)
			response.AddressWarning = settings.BridgeAddressWarning(connection.Address)
			user, _ := auth.UserFromContext(r.Context())
			if rendered, err := mqttbridge.Render(mqttbridge.Input{
				Connection: connection,
				Password:   bridgeCredentialsPassword(credentials),
				Mask:       true,
				RenderedBy: user.Username,
				RenderedAt: time.Now(),
			}); err == nil {
				response.Preview = rendered
			}
			writeJSON(w, response)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPut)
		}
	}
}

// handleBridgeCredentials sets or clears the bridge's remote_password,
// analogous to handleMQTTCredentials but for the separate
// mqtt_bridge_credentials.json store - the Hauptsystem broker's password is
// never the same secret as the dashboard's own broker connection.
func handleBridgeCredentials(credentials *mqttclient.CredentialStore, authManager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if credentials == nil {
			writeError(w, http.StatusNotImplemented, "bridge_unavailable", "Bridge-Zugangsdaten sind nicht verfügbar")
			return
		}
		if !requireRole(w, r, authManager, auth.RoleMQTTConfig, "bridge_forbidden", "Für Bridge-Zugangsdaten fehlt die Berechtigung") {
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
				writeError(w, http.StatusBadRequest, "bridge_rejected", err.Error())
				return
			}
			if strings.TrimSpace(body.Password) == "" {
				writeError(w, http.StatusBadRequest, "bridge_rejected", "Passwort darf nicht leer sein")
				return
			}
			if err := credentials.Save(mqttclient.Credentials{Password: body.Password}); err != nil {
				writeError(w, http.StatusBadRequest, "bridge_rejected", err.Error())
				return
			}
			writeJSON(w, map[string]bool{"password_configured": true})
		case http.MethodDelete:
			if err := credentials.Delete(); err != nil {
				writeError(w, http.StatusInternalServerError, "bridge_credentials_failed", err.Error())
				return
			}
			writeJSON(w, map[string]bool{"password_configured": false})
		default:
			methodNotAllowed(w, http.MethodPost, http.MethodDelete)
		}
	}
}

func writeStagedBridgeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".mosquitto-bridge-staged-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.WriteString(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}

// bridgeApplyErrorCode turns the root helper's exit code (see
// energy-node-dashboard-system-action's apply_bridge_config) into the
// error codes knowhow/dashboard/dashboard-mqtt-setup.md documents:
// bridge_helper_failed for a rejected staged file (exit 65) and
// bridge_restart_failed for a failed-but-rolled-back mosquitto restart
// (exit 75).
func bridgeApplyErrorCode(err error) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		switch exitErr.ExitCode() {
		case 75:
			return "bridge_restart_failed"
		}
	}
	return "bridge_helper_failed"
}

// handleBridgeApply renders the stored bridge configuration, stages it, and
// invokes the root helper via systemactions.ApplyBridgeConfig - the helper
// installs the file and restarts mosquitto, rolling back on failure. It
// requires both mqtt_config and system_actions (see the role table in
// knowhow/dashboard/dashboard-mqtt-setup.md): preparing a bridge
// configuration is harmless, but writing a root-owned file and restarting a
// system service is not.
func handleBridgeApply(store *settings.Store, credentials *mqttclient.CredentialStore, authManager *auth.Manager, executor SystemActionExecutor, watcher BridgeWatcher, dataDir string, state *bridgeApplyState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if !requireRole(w, r, authManager, auth.RoleMQTTConfig, "bridge_forbidden", "Für das Anwenden der Bridge fehlt die Berechtigung mqtt_config") {
			return
		}
		if !requireRole(w, r, authManager, auth.RoleSystemActions, "bridge_forbidden", "Für das Anwenden der Bridge fehlt die Berechtigung system_actions") {
			return
		}
		if !requireHTTPS(w, r) {
			return
		}
		if !requireCSRF(w, r, authManager) {
			return
		}
		if executor == nil {
			writeError(w, http.StatusNotImplemented, "bridge_unavailable", "Bridge-Anwendung ist nicht verfügbar")
			return
		}
		var body struct {
			Confirm bool `json:"confirm"`
		}
		if err := decodeBody(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "bridge_rejected", err.Error())
			return
		}
		if !body.Confirm {
			writeError(w, http.StatusBadRequest, "bridge_rejected", "confirm muss true sein")
			return
		}
		connection, configured, err := loadBridgeConnection(store)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "bridge_invalid", err.Error())
			return
		}
		if !configured {
			writeError(w, http.StatusBadRequest, "bridge_rejected", "keine Bridge-Konfiguration gespeichert")
			return
		}
		user, _ := auth.UserFromContext(r.Context())
		rendered, err := mqttbridge.Render(mqttbridge.Input{
			Connection: connection,
			Password:   bridgeCredentialsPassword(credentials),
			Mask:       false,
			RenderedBy: user.Username,
			RenderedAt: time.Now(),
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "bridge_rejected", err.Error())
			return
		}

		stagedPath := filepath.Join(dataDir, bridgeStagedFileName)
		if err := writeStagedBridgeFile(stagedPath, rendered); err != nil {
			writeError(w, http.StatusInternalServerError, "bridge_stage_failed", err.Error())
			return
		}

		applyErr := executor.Execute(r.Context(), systemactions.ApplyBridgeConfig)
		os.Remove(stagedPath)

		if watcher != nil {
			watcher.SetBridgeWatch(connection.RemoteClientID)
		}

		record := bridgeApplyRecord{At: time.Now().UTC(), User: user.Username}
		if applyErr != nil {
			record.Error = applyErr.Error()
			state.record(record)
			if errors.Is(applyErr, systemactions.ErrBusy) {
				writeError(w, http.StatusConflict, "bridge_busy", "Eine Systemaktion läuft bereits")
				return
			}
			writeError(w, http.StatusBadGateway, bridgeApplyErrorCode(applyErr), applyErr.Error())
			return
		}
		record.OK = true
		state.record(record)
		writeJSON(w, map[string]any{"ok": true})
	}
}

// handleBridgeRestart only restarts mosquitto (no file write), gated by
// system_actions alone - it is the "die Bridge steht, aber ich will
// trotzdem neu starten" escape hatch, not a configuration change.
func handleBridgeRestart(authManager *auth.Manager, executor SystemActionExecutor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if !requireRole(w, r, authManager, auth.RoleSystemActions, "bridge_forbidden", "Für den Mosquitto-Neustart fehlt die Berechtigung") {
			return
		}
		if !requireHTTPS(w, r) {
			return
		}
		if !requireCSRF(w, r, authManager) {
			return
		}
		if executor == nil {
			writeError(w, http.StatusNotImplemented, "bridge_unavailable", "Mosquitto-Neustart ist nicht verfügbar")
			return
		}
		if err := executor.Execute(r.Context(), systemactions.RestartMosquitto); err != nil {
			if errors.Is(err, systemactions.ErrBusy) {
				writeError(w, http.StatusConflict, "bridge_busy", "Eine Systemaktion läuft bereits")
				return
			}
			writeError(w, http.StatusBadGateway, "bridge_restart_failed", err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	}
}

type bridgeDriftStatus struct {
	Known     bool      `json:"known"`
	Matches   bool      `json:"matches"`
	CheckedAt time.Time `json:"checked_at"`
}

// bridgeDrift compares a freshly rendered configuration against the file
// actually installed at targetPath, ignoring comments (and therefore the
// header timestamp) via mqttbridge.DirectiveChecksum - "installed = saved"
// vs "abweichend, seit ...", see knowhow/dashboard/dashboard-mqtt-setup.md,
// Teil B, "Statusprüfung der Bridge".
func bridgeDrift(store *settings.Store, credentials *mqttclient.CredentialStore, targetPath string) bridgeDriftStatus {
	result := bridgeDriftStatus{CheckedAt: time.Now().UTC()}
	connection, configured, err := loadBridgeConnection(store)
	if err != nil || !configured {
		return result
	}
	installed, err := os.ReadFile(targetPath)
	if err != nil {
		return result
	}
	rendered, err := mqttbridge.Render(mqttbridge.Input{
		Connection: connection,
		Password:   bridgeCredentialsPassword(credentials),
		Mask:       false,
		RenderedBy: "drift-check",
		RenderedAt: time.Now(),
	})
	if err != nil {
		return result
	}
	result.Known = true
	result.Matches = mqttbridge.DirectiveChecksum(rendered) == mqttbridge.DirectiveChecksum(string(installed))
	return result
}

// handleBridgeStatus bundles the three status sources
// knowhow/dashboard/dashboard-mqtt-setup.md documents - service state,
// $SYS bridge connection state, and drift - none of which require root.
func handleBridgeStatus(store *settings.Store, credentials *mqttclient.CredentialStore, watcher BridgeWatcher, statusRunner systemactions.OutputRunner, targetPath string, state *bridgeApplyState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		response := map[string]any{
			"service_state": systemactions.ServiceIsActive(r.Context(), statusRunner, "mosquitto"),
			"drift":         bridgeDrift(store, credentials, targetPath),
		}
		if watcher != nil {
			response["bridge"] = watcher.BridgeStatus()
		} else {
			response["bridge"] = mqttclient.BridgeConnectionState{}
		}
		if record := state.get(); record != nil {
			response["last_apply"] = record
		}
		writeJSON(w, response)
	}
}

// handleBridgeSub serves the revisions/restore sub-routes, same shape as
// handleLayoutRevision and handleDeviceMapSub.
func handleBridgeSub(store *settings.Store, authManager *auth.Manager, watcher BridgeWatcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/mqtt/bridge/")
		parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
		if len(parts) == 1 && parts[0] == "revisions" && r.Method == http.MethodGet {
			if !requireRole(w, r, authManager, auth.RoleMQTTConfig, "bridge_forbidden", "Für Bridge-Revisionen fehlt die Berechtigung") {
				return
			}
			if !requireHTTPS(w, r) {
				return
			}
			revisions, err := store.BridgeRevisions()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "bridge_revisions_failed", err.Error())
				return
			}
			writeJSON(w, revisions)
			return
		}
		if len(parts) == 2 && parts[0] == "revisions" && r.Method == http.MethodGet {
			if !requireRole(w, r, authManager, auth.RoleMQTTConfig, "bridge_forbidden", "Für Bridge-Revisionen fehlt die Berechtigung") {
				return
			}
			if !requireHTTPS(w, r) {
				return
			}
			data, err := store.ReadBridgeRevision(parts[1])
			if err != nil {
				writeError(w, http.StatusNotFound, "bridge_revision_not_found", err.Error())
				return
			}
			writeJSON(w, data)
			return
		}
		if len(parts) == 1 && parts[0] == "restore" && r.Method == http.MethodPost {
			if !requireRole(w, r, authManager, auth.RoleMQTTConfig, "bridge_forbidden", "Für die Bridge-Wiederherstellung fehlt die Berechtigung") {
				return
			}
			if !requireHTTPS(w, r) {
				return
			}
			if !requireCSRF(w, r, authManager) {
				return
			}
			var request struct {
				Revision string `json:"revision"`
			}
			if err := decodeBody(r, &request); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
				return
			}
			value, err := store.RestoreBridge(request.Revision)
			if err != nil {
				writeError(w, http.StatusBadRequest, "bridge_restore_rejected", err.Error())
				return
			}
			if watcher != nil {
				if len(value.Connections) > 0 {
					watcher.SetBridgeWatch(value.Connections[0].RemoteClientID)
				} else {
					watcher.SetBridgeWatch("")
				}
			}
			writeJSON(w, value)
			return
		}
		methodNotAllowed(w)
	}
}
