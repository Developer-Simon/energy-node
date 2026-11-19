package httpapi

import (
	"io"
	"net/http"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
)

const (
	automationFindingsPath        = "/api/v1/automations/findings"
	automationFindingsIgnorePath  = "/api/v1/automations/findings/ignore"
	automationFindingsPreviewPath = "/api/v1/automations/findings/preview"
	// maxAutomationPreviewBytes caps the rule document posted for a preview.
	maxAutomationPreviewBytes = 1 << 20
)

// automationFindingSource is the part of the rule check the handlers use. The
// checker of package autolearn implements it.
type automationFindingSource interface {
	Findings(now time.Time) []autolearn.Finding
	Preview(raw []byte) ([]autolearn.Finding, error)
	Ignore(id string) error
	Unignore(id string) error
}

// automationFindingsBody is the answer of GET /api/v1/automations/findings.
// Findings is never null.
type automationFindingsBody struct {
	Findings []autolearn.Finding `json:"findings"`
}

// automationFindingIgnoreBody is the body of POST
// /api/v1/automations/findings/ignore. Undo shows an ignored finding again.
type automationFindingIgnoreBody struct {
	ID   string `json:"id"`
	Undo bool   `json:"undo"`
}

// handleAutomationFindings serves GET /api/v1/automations/findings. The checker
// may be nil (no data directory), then the answer is 501.
func handleAutomationFindings(checker automationFindingSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if checker == nil {
			writeErrorKey(w, http.StatusNotImplemented, "automation_findings_unavailable", "error.automation_findings_unavailable", nil, "Die Regelprüfung ist auf dieser Instanz nicht verfügbar")
			return
		}
		findings := checker.Findings(time.Now())
		if findings == nil {
			findings = []autolearn.Finding{}
		}
		writeJSON(w, automationFindingsBody{Findings: findings})
	}
}

// handleAutomationFindingsPreview serves POST
// /api/v1/automations/findings/preview. The body is a rule document that is
// not saved yet, the answer its static findings. It changes nothing, so the
// view role is enough, with a CSRF token. Without a checker the answer is 501.
func handleAutomationFindingsPreview(manager *auth.Manager, checker automationFindingSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if checker == nil {
			writeErrorKey(w, http.StatusNotImplemented, "automation_findings_unavailable", "error.automation_findings_unavailable", nil, "Die Regelprüfung ist auf dieser Instanz nicht verfügbar")
			return
		}
		if !requireAutomationFindingsPreview(w, r, manager) {
			return
		}
		defer r.Body.Close()
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAutomationPreviewBytes))
		if err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "Die Regeln sind zu groß")
			return
		}
		findings, err := checker.Preview(raw)
		if err != nil {
			// The code shares its catalog text with the ignore endpoint, so the
			// preview answers without a key and says what is wrong here.
			writeError(w, http.StatusBadRequest, "automation_finding_invalid", "Die Regeln sind ungültig.")
			return
		}
		if findings == nil {
			findings = []autolearn.Finding{}
		}
		writeJSON(w, automationFindingsBody{Findings: findings})
	}
}

// handleAutomationFindingIgnore serves POST
// /api/v1/automations/findings/ignore. It needs the automations role and a
// CSRF token, and answers 204.
func handleAutomationFindingIgnore(manager *auth.Manager, checker automationFindingSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if checker == nil {
			writeErrorKey(w, http.StatusNotImplemented, "automation_findings_unavailable", "error.automation_findings_unavailable", nil, "Die Regelprüfung ist auf dieser Instanz nicht verfügbar")
			return
		}
		if !requireAutomationFindingsMutation(w, r, manager) {
			return
		}
		defer r.Body.Close()
		var body automationFindingIgnoreBody
		if err := decodeBody(r, &body); err != nil {
			writeErrorKey(w, http.StatusBadRequest, "automation_finding_invalid", "error.automation_finding_invalid", nil, "Die Kennung des Befunds ist ungültig.")
			return
		}
		if body.ID == "" {
			writeErrorKey(w, http.StatusBadRequest, "automation_finding_invalid", "error.automation_finding_invalid", nil, "Die Kennung des Befunds ist ungültig.")
			return
		}
		change := checker.Ignore
		if body.Undo {
			change = checker.Unignore
		}
		if err := change(body.ID); err != nil {
			writeErrorKey(w, http.StatusInternalServerError, "automation_findings_save_failed", "error.automation_findings_save_failed", nil, "Der Befund konnte nicht gespeichert werden.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// requireAutomationFindingsMutation gates the ignore writes. Like
// requireEnergyMutation, without an auth manager everything passes, otherwise
// the caller needs the automations role plus a valid CSRF token.
func requireAutomationFindingsMutation(w http.ResponseWriter, r *http.Request, manager *auth.Manager) bool {
	if manager == nil {
		return true
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
		return false
	}
	if !auth.HasRole(user, auth.RoleAutomations) {
		writeErrorKey(w, http.StatusForbidden, "automation_findings_forbidden", "error.automation_findings_forbidden", nil, "Für die Regelprüfung fehlt die Berechtigung")
		return false
	}
	return requireAutomationFindingsCSRF(w, r, manager)
}

// requireAutomationFindingsPreview gates the preview. Without an auth manager
// everything passes, otherwise the caller needs the view role plus a valid
// CSRF token. The automations role is not needed, nothing is written.
func requireAutomationFindingsPreview(w http.ResponseWriter, r *http.Request, manager *auth.Manager) bool {
	if manager == nil {
		return true
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
		return false
	}
	if !auth.HasRole(user, auth.RoleViewAutomations) {
		writeErrorKey(w, http.StatusForbidden, "automation_findings_forbidden", "error.automation_findings_forbidden", nil, "Für die Regelprüfung fehlt die Berechtigung")
		return false
	}
	return requireAutomationFindingsCSRF(w, r, manager)
}

// requireAutomationFindingsCSRF checks the CSRF token of the session cookie.
func requireAutomationFindingsCSRF(w http.ResponseWriter, r *http.Request, manager *auth.Manager) bool {
	cookie, err := r.Cookie(sessionCookieName(isSecureRequest(r)))
	if err != nil || !manager.ValidateCSRF(cookie.Value, r.Header.Get("X-CSRF-Token")) {
		writeError(w, http.StatusForbidden, "csrf_failed", "Sicherheitsprüfung fehlgeschlagen")
		return false
	}
	return true
}
