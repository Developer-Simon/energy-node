package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/servicelogs"
)

// ServiceLogReader reads the journal entries of one allowlisted service.
// *servicelogs.Reader implements it.
type ServiceLogReader interface {
	Read(context.Context, servicelogs.Query) (servicelogs.Result, error)
}

// requireSystemActionsRead gates the read-only service log endpoints behind
// the same checks as handleSystemAction, minus the CSRF token: reading a
// journal changes nothing. It sets Cache-Control: no-store because journal
// lines can contain anything a service logged.
func requireSystemActionsRead(w http.ResponseWriter, r *http.Request, manager *auth.Manager) bool {
	w.Header().Set("Cache-Control", "no-store")
	if manager == nil {
		writeError(w, http.StatusNotImplemented, "system_actions_unavailable", "Systemaktionen sind nicht konfiguriert")
		return false
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
		return false
	}
	if !auth.HasRole(user, auth.RoleSystemActions) {
		writeError(w, http.StatusForbidden, "system_actions_forbidden", "Für Systemaktionen fehlt die Berechtigung")
		return false
	}
	return true
}

// handleServiceLogs answers GET /api/v1/system/logs. Missing since and lines
// take their defaults, a missing unit fails validation.
func handleServiceLogs(manager *auth.Manager, reader ServiceLogReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if !requireSystemActionsRead(w, r, manager) {
			return
		}
		values := r.URL.Query()
		query := servicelogs.Query{
			Unit:         values.Get("unit"),
			SinceMinutes: servicelogs.DefaultSinceMinutes,
			Lines:        servicelogs.DefaultLines,
			Cursor:       values.Get("cursor"),
			Filter:       values.Get("q"),
		}
		if raw := values.Get("since"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "log_query_invalid", "Die Log-Abfrage ist ungültig.")
				return
			}
			query.SinceMinutes = n
		}
		if raw := values.Get("lines"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "log_query_invalid", "Die Log-Abfrage ist ungültig.")
				return
			}
			query.Lines = n
		}
		if err := query.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, "log_query_invalid", "Die Log-Abfrage ist ungültig.")
			return
		}
		result, err := reader.Read(r.Context(), query)
		if err != nil {
			writeServiceLogError(w, err)
			return
		}
		writeJSON(w, result)
	}
}

// writeServiceLogError maps the servicelogs errors to status and code. The
// checks run in the same order as the package's own error mapping.
func writeServiceLogError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, servicelogs.ErrInvalidQuery):
		writeError(w, http.StatusBadRequest, "log_query_invalid", "Die Log-Abfrage ist ungültig.")
	case errors.Is(err, servicelogs.ErrNotPermitted):
		writeError(w, http.StatusServiceUnavailable, "journal_not_permitted", "Das Dashboard darf das Journal nicht lesen. Nach dem nächsten Update ist die Berechtigung eingerichtet.")
	case errors.Is(err, servicelogs.ErrTimeout):
		writeError(w, http.StatusGatewayTimeout, "journal_timeout", "Das Journal hat nicht rechtzeitig geantwortet.")
	case errors.Is(err, servicelogs.ErrBusy):
		writeError(w, http.StatusTooManyRequests, "journal_busy", "Es laufen bereits zwei Log-Abfragen. Bitte gleich noch einmal versuchen.")
	default:
		writeError(w, http.StatusBadGateway, "journal_failed", "Das Journal konnte nicht gelesen werden: "+err.Error())
	}
}

// handleServiceLogUnits answers GET /api/v1/system/logs/units with the
// allowlists and defaults the log screen offers.
func handleServiceLogUnits(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if !requireSystemActionsRead(w, r, manager) {
			return
		}
		writeJSON(w, map[string]any{
			"units":                 servicelogs.Units,
			"since_minutes":         servicelogs.SinceMinutes,
			"lines":                 servicelogs.LineLimits,
			"default_unit":          servicelogs.DefaultUnit,
			"default_since_minutes": servicelogs.DefaultSinceMinutes,
			"default_lines":         servicelogs.DefaultLines,
		})
	}
}
