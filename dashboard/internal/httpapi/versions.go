package httpapi

import (
	"errors"
	"net/http"

	"github.com/Developer-Simon/energy-node-dashboard/internal/versions"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// handleVersions is GET /api/v1/versions: the bundle this node was installed
// from and the version of every component in it. Readable by every signed-in
// session, guests included -- it says nothing an operator may not see.
func handleVersions(stateDir, running string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		snapshot, err := versions.Load(stateDir, running)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "versions_unreadable", "Versionsdaten konnten nicht gelesen werden")
			return
		}
		writeJSON(w, snapshot)
	}
}

// handleChangelog is GET /api/v1/changelog[?component=<id>&component=<id>]:
// the installed changelog.json, optionally cut down to some components.
func handleChangelog(stateDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		raw, err := versions.Changelog(stateDir, r.URL.Query()["component"])
		if err != nil {
			var apiErr *hostapi.Error
			if errors.As(err, &apiErr) && apiErr.Code == "NO_CHANGELOG" {
				writeError(w, http.StatusNotFound, "no_changelog", "Dieses Paket enthält keinen Changelog")
				return
			}
			writeError(w, http.StatusInternalServerError, "changelog_unreadable", "Changelog konnte nicht gelesen werden")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}
}
