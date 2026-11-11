package hostapi

import (
	"encoding/json"
	"net/http"
	"net/url"
)

// maxOpenURL bounds the address a page may ask the host to open.
const maxOpenURL = 2048

// imagerStoreURL oeffnet den Raspberry Pi Imager im App-Store des Rechners.
// Die einzige Ausnahme von http und https; andere appstream-Adressen bleiben
// gesperrt.
const imagerStoreURL = "appstream://org.raspberrypi.rpi-imager"

type openURLRequest struct {
	URL string `json:"url"`
}

// handleOpenURL opens an http(s) address in the default browser. Anything else
// (file:, javascript:, a command line) is refused: the endpoint must not turn a
// page into a way to start programs.
func (s *Server) handleOpenURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	if s.opts.OpenURL == nil {
		writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "open-url")
		return
	}
	var req openURLRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxOpenURL*2)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	target := req.URL
	if req.URL != imagerStoreURL {
		parsed, err := url.Parse(req.URL)
		if err != nil || len(req.URL) > maxOpenURL || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "only http and https addresses can be opened")
			return
		}
		target = parsed.String()
	}
	if err := s.opts.OpenURL(target); err != nil {
		writeError(w, http.StatusInternalServerError, "BACKEND_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}
