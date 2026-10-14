package httpapi

import (
	"errors"
	"net/http"
	"strconv"

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

// handleChangelog is GET /api/v1/changelog[?component=<id>&bundles=<n>]: the
// installed changelog, optionally cut down to some components and to the
// newest n bundle versions (0 or missing: all).
func handleChangelog(stateDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		limit := 0
		if raw := r.URL.Query().Get("bundles"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 0 {
				writeError(w, http.StatusBadRequest, "invalid_bundles", "bundles muss eine Zahl ab 0 sein")
				return
			}
			limit = n
		}
		raw, err := versions.Changelog(stateDir, r.URL.Query()["component"], limit)
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

// handleChangelogHighlights is GET /api/v1/changelog/highlights: the
// highlighted changes of the last update (versions.Highlights). Readable by
// every signed-in session, like the versions page it belongs to.
func handleChangelogHighlights(stateDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		view, err := versions.Highlights(stateDir)
		if err != nil {
			var apiErr *hostapi.Error
			if errors.As(err, &apiErr) && apiErr.Code == "NO_CHANGELOG" {
				writeError(w, http.StatusNotFound, "no_changelog", "Dieses Paket enthält keinen Changelog")
				return
			}
			writeError(w, http.StatusInternalServerError, "changelog_unreadable", "Changelog konnte nicht gelesen werden")
			return
		}
		writeJSON(w, view)
	}
}
