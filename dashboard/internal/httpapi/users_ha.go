package httpapi

import (
	"net/http"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/companion"
)

// haAccountPrefix marks the path segment of a Home Assistant account:
// /api/v1/users/ha:<id>. Local account names cannot contain a colon.
const haAccountPrefix = "ha:"

// companionListing is the companion status of the account list. Connected
// tells whether the companion stream is open right now.
type companionStatusView struct {
	auth.CompanionStatus
	Connected bool `json:"connected"`
}

// companionListing returns the pairing state with the live connection.
func companionListing(manager *auth.Manager, hub *companion.Hub) companionStatusView {
	return companionStatusView{
		CompanionStatus: manager.CompanionStatus(),
		Connected:       hub != nil && hub.Connected(),
	}
}

// haRolesListing returns the presets for Home Assistant accounts, or nil
// while they are not set.
func haRolesListing(manager *auth.Manager) map[string][]string {
	admin, user, set := manager.HARoles()
	if !set {
		return nil
	}
	return map[string][]string{"admin": admin, "user": user}
}

// handleCompanionPairingCode creates (POST) or drops (DELETE) the open
// pairing code. The code is only in the POST answer, never in the list.
func handleCompanionPairingCode(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			if !requireUsersAccess(w, r, manager, true) {
				return
			}
			actor, _ := auth.UserFromContext(r.Context())
			code, expiresAt, err := manager.CreatePairingCode(actor.Username)
			if err != nil {
				writeUserError(w, err)
				return
			}
			writeJSON(w, map[string]any{"code": code, "expires_at": expiresAt})
		case http.MethodDelete:
			if !requireUsersAccess(w, r, manager, true) {
				return
			}
			actor, _ := auth.UserFromContext(r.Context())
			manager.DropPairingCode(actor.Username)
			writeJSON(w, map[string]string{"status": "pairing_code_dropped"})
		default:
			methodNotAllowed(w, http.MethodPost, http.MethodDelete)
		}
	}
}

// handleCompanionUnpair removes the pairing and closes the companion stream.
// The Home Assistant accounts and their presets stay.
func handleCompanionUnpair(manager *auth.Manager, hub *companion.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			methodNotAllowed(w, http.MethodDelete)
			return
		}
		if !requireUsersAccess(w, r, manager, true) {
			return
		}
		actor, _ := auth.UserFromContext(r.Context())
		if err := manager.Unpair(actor.Username); err != nil {
			writeUserError(w, err)
			return
		}
		if hub != nil {
			hub.Close()
		}
		writeJSON(w, map[string]string{"status": "unpaired"})
	}
}

// handleHARoles sets the presets for Home Assistant accounts without their own
// roles.
func handleHARoles(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			methodNotAllowed(w, http.MethodPut)
			return
		}
		if !requireUsersAccess(w, r, manager, true) {
			return
		}
		actor, _ := auth.UserFromContext(r.Context())
		var request struct {
			Admin []string `json:"admin"`
			User  []string `json:"user"`
		}
		if err := decodeBody(r, &request); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
			return
		}
		if err := manager.SetHARoles(actor.Username, request.Admin, request.User); err != nil {
			writeUserError(w, err)
			return
		}
		writeJSON(w, map[string]any{"ha_roles": haRolesListing(manager)})
	}
}

// handleHAAccount changes (PUT) or removes (DELETE) one Home Assistant account.
// The account has no password, so there is no password route for it.
func handleHAAccount(manager *auth.Manager, haUserID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			if !requireUsersAccess(w, r, manager, true) {
				return
			}
			actor, _ := auth.UserFromContext(r.Context())
			var request struct {
				Roles       []string `json:"roles"`
				Disabled    bool     `json:"disabled"`
				RolesCustom bool     `json:"roles_custom"`
			}
			if err := decodeBody(r, &request); err != nil {
				writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
				return
			}
			account, err := manager.UpdateHAAccount(actor.Username, haUserID, request.Roles, request.Disabled, request.RolesCustom)
			if err != nil {
				writeUserError(w, err)
				return
			}
			writeJSON(w, account)
		case http.MethodDelete:
			if !requireUsersAccess(w, r, manager, true) {
				return
			}
			actor, _ := auth.UserFromContext(r.Context())
			if err := manager.DeleteHAAccount(actor.Username, haUserID); err != nil {
				writeUserError(w, err)
				return
			}
			writeJSON(w, map[string]string{"status": "deleted"})
		default:
			methodNotAllowed(w, http.MethodPut, http.MethodDelete)
		}
	}
}
