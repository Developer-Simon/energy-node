package httpapi

import (
	"errors"
	"net/http"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/netmgr"
)

// networkGate is the gate of every network route: no-store, because the
// status lists WLAN networks and the profiles, and the network role.
func networkGate(w http.ResponseWriter, r *http.Request, manager *auth.Manager) bool {
	w.Header().Set("Cache-Control", "no-store")
	return requireRole(w, r, manager, auth.RoleNetwork, "network_forbidden", "error.network_forbidden", "Für die Netzwerkeinstellungen fehlt die Berechtigung")
}

// networkWriteGate is networkGate plus HTTPS and CSRF, the three gates of the
// WLAN actions, same order as the Tailscale actions.
func networkWriteGate(w http.ResponseWriter, r *http.Request, manager *auth.Manager) bool {
	if !networkGate(w, r, manager) {
		return false
	}
	if !requireHTTPS(w, r) {
		return false
	}
	return requireCSRF(w, r, manager)
}

// requireManaged answers 409 network_unmanaged when NetworkManager does not
// run. It returns the status, which connect needs for the scanned security.
func requireManaged(w http.ResponseWriter, r *http.Request, client *netmgr.Client) (netmgr.Status, bool) {
	status, err := client.Status(r.Context())
	if err != nil {
		writeNetworkError(w, err)
		return netmgr.Status{}, false
	}
	if !status.Managed {
		writeError(w, http.StatusConflict, "network_unmanaged", "Das Netz wird nicht von NetworkManager verwaltet")
		return netmgr.Status{}, false
	}
	return status, true
}

// writeNetworkError maps the netmgr errors to their status and code. Anything
// else is a failed helper run and answers 502 network_failed.
func writeNetworkError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, netmgr.ErrBusy):
		writeError(w, http.StatusConflict, "network_busy", "Eine WLAN-Umstellung läuft bereits")
	case errors.Is(err, netmgr.ErrUnmanaged):
		writeError(w, http.StatusConflict, "network_unmanaged", "Das Netz wird nicht von NetworkManager verwaltet")
	case errors.Is(err, netmgr.ErrInvalidRequest):
		writeErrorDetail(w, http.StatusBadRequest, "network_rejected", err)
	default:
		writeErrorDetail(w, http.StatusBadGateway, "network_failed", err)
	}
}

// handleNetworkStatus answers GET /api/v1/network/status with netmgr.Status.
func handleNetworkStatus(manager *auth.Manager, client *netmgr.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if !networkGate(w, r, manager) {
			return
		}
		if client == nil {
			writeError(w, http.StatusNotImplemented, "network_unavailable", "Netzwerkaktionen sind nicht verfügbar")
			return
		}
		status, err := client.Status(r.Context())
		if err != nil {
			writeNetworkError(w, err)
			return
		}
		writeJSON(w, status)
	}
}

// handleNetworkScan starts a WLAN scan through NetworkManager.
func handleNetworkScan(manager *auth.Manager, client *netmgr.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if !networkWriteGate(w, r, manager) {
			return
		}
		if client == nil {
			writeError(w, http.StatusNotImplemented, "network_unavailable", "Netzwerkaktionen sind nicht verfügbar")
			return
		}
		if _, ok := requireManaged(w, r, client); !ok {
			return
		}
		if err := client.Scan(r.Context()); err != nil {
			writeNetworkError(w, err)
			return
		}
		writeJSONStatus(w, http.StatusAccepted, map[string]any{"ok": true})
	}
}

// handleNetworkConnect starts a WLAN switch. The password is cleared from the
// request as soon as the handler returns, so no response or log line carries
// it. The security comes from the latest scan of the SSID, empty for a hidden
// network that was not seen.
func handleNetworkConnect(manager *auth.Manager, client *netmgr.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if !networkWriteGate(w, r, manager) {
			return
		}
		if client == nil {
			writeError(w, http.StatusNotImplemented, "network_unavailable", "Netzwerkaktionen sind nicht verfügbar")
			return
		}
		var req netmgr.ConnectRequest
		defer func() { req.Password = "" }()
		// The decoder error is not passed on: it can quote a byte of the password.
		if err := decodeBody(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "network_rejected", "Die Angaben zum WLAN sind ungültig")
			return
		}
		status, ok := requireManaged(w, r, client)
		if !ok {
			return
		}
		security := ""
		for _, network := range status.Networks {
			if network.SSID == req.SSID {
				security = network.Security
				break
			}
		}
		if err := client.Connect(r.Context(), req, security); err != nil {
			writeNetworkError(w, err)
			return
		}
		writeJSONStatus(w, http.StatusAccepted, map[string]any{"ok": true})
	}
}

// handleNetworkForget deletes a saved WLAN profile.
func handleNetworkForget(manager *auth.Manager, client *netmgr.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if !networkWriteGate(w, r, manager) {
			return
		}
		if client == nil {
			writeError(w, http.StatusNotImplemented, "network_unavailable", "Netzwerkaktionen sind nicht verfügbar")
			return
		}
		var body struct {
			ProfileUUID string `json:"profile_uuid"`
		}
		if err := decodeBody(r, &body); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "network_rejected", err)
			return
		}
		if _, ok := requireManaged(w, r, client); !ok {
			return
		}
		if err := client.Forget(r.Context(), body.ProfileUUID); err != nil {
			writeNetworkError(w, err)
			return
		}
		writeJSON(w, map[string]string{"profile_uuid": body.ProfileUUID})
	}
}

// handleNetworkSettings saves the WLAN fallback mode and the hotspot switch.
// The mode is checked in netmgr, so an unknown mode answers 400 network_rejected
// without the helper running.
func handleNetworkSettings(manager *auth.Manager, client *netmgr.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			methodNotAllowed(w, http.MethodPut)
			return
		}
		if !networkWriteGate(w, r, manager) {
			return
		}
		if client == nil {
			writeError(w, http.StatusNotImplemented, "network_unavailable", "Netzwerkaktionen sind nicht verfügbar")
			return
		}
		var settings netmgr.Settings
		if err := decodeBody(r, &settings); err != nil {
			writeError(w, http.StatusBadRequest, "network_rejected", "Die Einstellungen sind ungültig")
			return
		}
		if _, ok := requireManaged(w, r, client); !ok {
			return
		}
		if err := client.SaveSettings(r.Context(), settings); err != nil {
			writeNetworkError(w, err)
			return
		}
		writeJSON(w, settings)
	}
}

// handleHotspotSecret answers the SSID and password of the emergency hotspot.
// It is a read, so it needs the network role and HTTPS but no CSRF token, and
// the response is no-store like every network route.
func handleHotspotSecret(manager *auth.Manager, client *netmgr.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if !networkGate(w, r, manager) {
			return
		}
		if !requireHTTPS(w, r) {
			return
		}
		if client == nil {
			writeError(w, http.StatusNotImplemented, "network_unavailable", "Netzwerkaktionen sind nicht verfügbar")
			return
		}
		ssid, password, err := client.HotspotSecret(r.Context())
		if err != nil {
			writeNetworkError(w, err)
			return
		}
		writeJSON(w, map[string]string{"ssid": ssid, "password": password})
	}
}
