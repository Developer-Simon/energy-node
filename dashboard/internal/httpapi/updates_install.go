package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/updateinstall"
)

// UpdateInstaller installs the update the dashboard currently offers and
// reports how that installation is going (internal/updateinstall).
type UpdateInstaller interface {
	Install(ctx context.Context) (string, error)
	Status() updateinstall.Status
}

// handleUpdatesInstall is POST /api/v1/updates/install. It installs exactly
// the offered update: the body and the query are ignored, so no client can
// name a different version. The answer is 202 with the offered version.
func handleUpdatesInstall(manager *auth.Manager, installer UpdateInstaller) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if !requireRole(w, r, manager, auth.RoleInstallUpdates, "install_updates_forbidden", "error.install_updates_forbidden", "Zum Installieren von Updates fehlt die Berechtigung.") {
			return
		}
		if !requireCSRF(w, r, manager) {
			return
		}
		if installer == nil {
			writeErrorKey(w, http.StatusNotImplemented, "install_unavailable", "error.install_unavailable", nil, "Updates lassen sich auf diesem Gerät nicht direkt installieren.")
			return
		}
		version, err := installer.Install(r.Context())
		switch {
		case errors.Is(err, updateinstall.ErrNoUpdate):
			writeErrorKey(w, http.StatusConflict, "no_update_available", "error.no_update_available", nil, "Es ist kein Update verfügbar.")
			return
		case errors.Is(err, updateinstall.ErrBusy):
			writeErrorKey(w, http.StatusConflict, "update_in_progress", "error.update_in_progress", nil, "Ein Update läuft bereits.")
			return
		case err != nil:
			writeErrorKey(w, http.StatusInternalServerError, "install_failed", "error.install_failed", nil, "Das Update konnte nicht gestartet werden.")
			return
		}
		writeJSONStatus(w, http.StatusAccepted, map[string]string{"version": version})
	}
}
