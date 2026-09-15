package hostapi

import (
	"encoding/json"
	"net/http"
)

// Die vier Endpunkte dieses Datei-Satzes veraendern auf dem Node nichts. Das
// ist die technische Seite der Zusicherung aus dem Ablauf der Spec ("bis
// hierher wird nichts veraendert") - kein Handler hier ruft Backend.Run auf.

func (s *Server) handlePrecheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	if !s.requireConnection(w) {
		return
	}
	report, err := s.opts.Backend.Precheck(r.Context())
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	view, err := s.opts.Backend.Manifest(r.Context())
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleSelection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		view, err := s.opts.Backend.Selection(r.Context())
		if err != nil {
			writeBackendError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	case http.MethodPut:
		var body struct {
			Steps map[string]bool `json:"steps"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
			return
		}
		if body.Steps == nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "steps fehlt")
			return
		}
		if err := s.opts.Backend.SaveSelection(r.Context(), body.Steps); err != nil {
			writeBackendError(w, err)
			return
		}
		view, err := s.opts.Backend.Selection(r.Context())
		if err != nil {
			writeBackendError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	default:
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
	}
}

func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	if !s.requireConnection(w) {
		return
	}
	view, err := s.opts.Backend.Plan(r.Context())
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
