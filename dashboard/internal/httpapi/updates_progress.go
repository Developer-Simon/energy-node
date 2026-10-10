package httpapi

import (
	"net/http"

	"github.com/Developer-Simon/energy-node-dashboard/internal/updateprogress"
)

// handleUpdatesProgress is GET /api/v1/updates/progress: how far a staged or
// running update is, and how the last one ended (internal/updateprogress).
// Every signed-in session may read it -- the bar that polls it tells every
// operator that the dashboard is about to restart. The step title comes from
// the shared installer catalogs in the request's language (stepTitle).
func handleUpdatesProgress(jobDir string, stepTitle func(*http.Request, string) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		progress, err := updateprogress.Read(jobDir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "updates_progress_unreadable", "Update-Fortschritt konnte nicht gelesen werden")
			return
		}
		if progress.Step != nil && stepTitle != nil {
			progress.Step.Title = stepTitle(r, progress.Step.ID)
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, progress)
	}
}
