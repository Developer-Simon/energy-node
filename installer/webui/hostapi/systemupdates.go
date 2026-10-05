package hostapi

import (
	"context"
	"net/http"
)

// SystemUpdatesProvider ist eine optionale Faehigkeit eines Backends: die
// ausstehenden Systempakete (Schritt 15) auf Abfrage. Die Vorschau zaehlt
// sie nicht von sich aus, auf einem Pi 1 dauert schon die Simulation.
// refresh=false zaehlt auf dem letzten Stand der Paketlisten, refresh=true
// holt die Listen vorher frisch (apt-get update, braucht root). Ein nil-View
// ohne Fehler heisst "nicht ermittelbar".
type SystemUpdatesProvider interface {
	SystemUpdates(ctx context.Context, refresh bool) (*SystemUpdates, error)
}

// handleSystemUpdates: GET zaehlt auf dem letzten Stand.
func (s *Server) handleSystemUpdates(w http.ResponseWriter, r *http.Request) {
	s.serveSystemUpdates(w, r, http.MethodGet, false)
}

// handleSystemUpdatesRefresh: POST holt die Listen jetzt frisch. POST, weil
// es den Node veraendert (apt-get update).
func (s *Server) handleSystemUpdatesRefresh(w http.ResponseWriter, r *http.Request) {
	s.serveSystemUpdates(w, r, http.MethodPost, true)
}

func (s *Server) serveSystemUpdates(w http.ResponseWriter, r *http.Request, method string, refresh bool) {
	if r.Method != method {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	if !s.requireConnection(w) {
		return
	}
	provider, ok := s.opts.Backend.(SystemUpdatesProvider)
	if !ok {
		writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "")
		return
	}
	view, err := provider.SystemUpdates(r.Context(), refresh)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
