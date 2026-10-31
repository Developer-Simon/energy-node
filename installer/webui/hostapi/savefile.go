package hostapi

import (
	"encoding/json"
	"net/http"
)

// maxSaveFile bounds the text a page may ask the host to save.
const maxSaveFile = 8 << 20

type saveFileRequest struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

// handleSaveFile saves a text file to the Downloads folder of the machine the
// window runs on. The host decides the folder and cleans the name, the page
// only suggests a file name.
func (s *Server) handleSaveFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	if s.opts.SaveFile == nil {
		writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "save-file")
		return
	}
	var req saveFileRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSaveFile+4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if len(req.Content) > maxSaveFile {
		writeError(w, http.StatusRequestEntityTooLarge, "BAD_REQUEST", "the file is too large")
		return
	}
	path, err := s.opts.SaveFile(req.Filename, []byte(req.Content))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "FILE_SAVE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}
