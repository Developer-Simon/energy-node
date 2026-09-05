package httpapi

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/systemactions"
	"github.com/Developer-Simon/energy-node-dashboard/internal/tailscale"
)

// tailscaleActionRecord is the "last action" (Zeit, Benutzer, Ergebnis)
// shown by GET /api/v1/tailscale/status. In-memory only, same reasoning as
// bridgeApplyRecord in bridge.go: no durable history, consistent with
// AGENTS.md's exclusion of server-side history.
type tailscaleActionRecord struct {
	At     time.Time `json:"at"`
	User   string    `json:"user,omitempty"`
	Action string    `json:"action"`
	OK     bool      `json:"ok"`
	Error  string    `json:"error,omitempty"`
}

type tailscaleActionState struct {
	mu   sync.Mutex
	last *tailscaleActionRecord
}

func newTailscaleActionState() *tailscaleActionState { return &tailscaleActionState{} }

func (s *tailscaleActionState) record(rec tailscaleActionRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = &rec
}

func (s *tailscaleActionState) get() *tailscaleActionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// handleTailscaleStatus bundles `tailscale status --json` (via
// tailscale.Client, unprivileged - no role required, same as
// /api/v1/mqtt/status and the bridge status endpoint) with the in-memory
// last-action record.
func handleTailscaleStatus(client *tailscale.Client, state *tailscaleActionState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		status, err := client.Status(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, "tailscale_status_failed", err.Error())
			return
		}
		response := map[string]any{"status": status}
		if record := state.get(); record != nil {
			response["last_action"] = record
		}
		writeJSON(w, response)
	}
}

// handleTailscalePrereqs serves the "ist Tailscale installiert, läuft
// tailscaled, ist der Dienst aktiviert" check for the wizard's first step.
// Unprivileged, same as handleTailscaleStatus.
func handleTailscalePrereqs(client *tailscale.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, client.Prereqs(r.Context()))
	}
}

// handleTailscaleAction is the shared shape of the three privileged
// Tailscale actions (login, logout, restart): RoleSystemActions + HTTPS +
// CSRF, same three-gate pattern as handleBridgeApply/handleBridgeRestart,
// executed through the root system-action helper, and recorded in state
// regardless of outcome.
func handleTailscaleAction(authManager *auth.Manager, executor SystemActionExecutor, state *tailscaleActionState, action systemactions.Action, requireConfirm bool, successStatus int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if !requireRole(w, r, authManager, auth.RoleSystemActions, "tailscale_forbidden", "Für diese Tailscale-Aktion fehlt die Berechtigung") {
			return
		}
		if !requireHTTPS(w, r) {
			return
		}
		if !requireCSRF(w, r, authManager) {
			return
		}
		if executor == nil {
			writeError(w, http.StatusNotImplemented, "tailscale_unavailable", "Tailscale-Aktionen sind nicht verfügbar")
			return
		}
		if requireConfirm {
			var body struct {
				Confirm bool `json:"confirm"`
			}
			if err := decodeBody(r, &body); err != nil {
				writeError(w, http.StatusBadRequest, "tailscale_rejected", err.Error())
				return
			}
			if !body.Confirm {
				writeError(w, http.StatusBadRequest, "tailscale_rejected", "confirm muss true sein")
				return
			}
		}
		user, _ := auth.UserFromContext(r.Context())
		record := tailscaleActionRecord{At: time.Now().UTC(), User: user.Username, Action: string(action)}
		if err := executor.Execute(r.Context(), action); err != nil {
			record.Error = err.Error()
			state.record(record)
			if errors.Is(err, systemactions.ErrBusy) {
				writeError(w, http.StatusConflict, "tailscale_busy", "Eine Systemaktion läuft bereits")
				return
			}
			writeError(w, http.StatusBadGateway, "tailscale_action_failed", err.Error())
			return
		}
		record.OK = true
		state.record(record)
		writeJSONStatus(w, successStatus, map[string]any{"ok": true})
	}
}

// handleTailscaleLogin starts `tailscale up` (see systemactions.TailscaleUp
// and the energy-node-dashboard-system-action helper) - it backgrounds
// the CLI and returns 202 immediately; the caller polls
// GET /api/v1/tailscale/status for the resulting AuthURL.
func handleTailscaleLogin(authManager *auth.Manager, executor SystemActionExecutor, state *tailscaleActionState) http.HandlerFunc {
	return handleTailscaleAction(authManager, executor, state, systemactions.TailscaleUp, true, http.StatusAccepted)
}

// handleTailscaleLogout runs `tailscale logout`, gated by an explicit
// confirm flag per the idea-list requirement ("Abmelden mit Bestätigung").
func handleTailscaleLogout(authManager *auth.Manager, executor SystemActionExecutor, state *tailscaleActionState) http.HandlerFunc {
	return handleTailscaleAction(authManager, executor, state, systemactions.TailscaleLogout, true, http.StatusOK)
}

// handleTailscaleRestart restarts the tailscaled service, mirroring
// handleBridgeRestart: no body, no confirm - it is the "steht, aber ich
// will trotzdem neu starten" escape hatch, not a membership change.
func handleTailscaleRestart(authManager *auth.Manager, executor SystemActionExecutor, state *tailscaleActionState) http.HandlerFunc {
	return handleTailscaleAction(authManager, executor, state, systemactions.TailscaleRestart, false, http.StatusOK)
}
