package httpapi

import (
	"net/http"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/updateinstall"
	"github.com/Developer-Simon/energy-node-dashboard/internal/updateprogress"
)

// installProgress is the answer of GET /api/v1/updates/progress while the
// installer of the offered update is preparing or has just failed.
type installProgress struct {
	State      string     `json:"state"`
	Version    string     `json:"version,omitempty"`
	Code       string     `json:"code,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// handleUpdatesProgress is GET /api/v1/updates/progress: how far a staged or
// running update is, and how the last one ended (internal/updateprogress).
// Every signed-in session may read it -- the bar that polls it tells every
// operator that the dashboard is about to restart. The step title comes from
// the shared installer catalogs in the request's language (stepTitle).
//
// The installer (installer may be nil) can be ahead of the job directory: it
// reports preparing while its prepare run works, and a failure it recorded
// shows instead of the job when that failure is newer than the job's end.
func handleUpdatesProgress(jobDir string, stepTitle func(*http.Request, string) string, installer UpdateInstaller) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		w.Header().Set("Cache-Control", "no-store")

		var installed updateinstall.Status
		if installer != nil {
			installed = installer.Status()
			if installed.State == "preparing" {
				writeJSON(w, installProgress{State: "preparing", Version: installed.Version})
				return
			}
		}
		progress, err := updateprogress.Read(jobDir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "updates_progress_unreadable", "Update-Fortschritt konnte nicht gelesen werden")
			return
		}
		// A job that is pending or running is in flight and always stays
		// visible; only a finished or absent job can be older than a failure.
		inFlight := progress.State == "pending" || progress.State == "running"
		if installed.State == "fail" && installed.FinishedAt != nil && !inFlight && (progress.FinishedAt == nil || installed.FinishedAt.After(*progress.FinishedAt)) {
			writeJSON(w, installProgress{State: "fail", Version: installed.Version, Code: installed.Code, FinishedAt: installed.FinishedAt})
			return
		}
		if progress.Step != nil && stepTitle != nil {
			progress.Step.Title = stepTitle(r, progress.Step.ID)
		}
		writeJSON(w, progress)
	}
}
