package hostapi

import (
	"encoding/json"
	"net/http"
	"sync"
)

// connection haelt fest, ob eine Verbindung besteht. Der Zustand gehoert dem
// Server, nicht dem Backend: nur der Server weiss, ob ueberhaupt jemand
// Connect aufgerufen hat.
type connection struct {
	mu sync.RWMutex
	ok bool
}

func (c *connection) set(ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ok = ok
}

func (c *connection) get() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ok
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	var req ConnectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if req.Host == "" || req.User == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "host und user sind Pflicht")
		return
	}

	result, err := s.opts.Backend.Connect(r.Context(), req)
	if err != nil {
		s.conn.set(false)
		s.bus.Publish("connection", map[string]bool{"connected": false})
		writeBackendError(w, err)
		return
	}
	s.conn.set(result.Connected)
	s.bus.Publish("connection", map[string]bool{"connected": result.Connected})

	// Die Antwort traegt nie das Passwort zurueck - ConnectResult hat gar kein
	// Feld dafuer, und das ist Absicht.
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleKeypair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	if !s.requireConnection(w) {
		return
	}
	result, err := s.opts.Backend.GenerateKeypair(r.Context())
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// requireConnection weist einen Aufruf ab, der eine Verbindung braucht, wenn
// keine besteht. Wirte ohne Verbindungsbildschirm (Dashboard) durchlaufen die
// Pruefung immer.
func (s *Server) requireConnection(w http.ResponseWriter) bool {
	if !s.opts.Backend.Describe().NeedsConnection {
		return true
	}
	if s.conn.get() {
		return true
	}
	writeError(w, http.StatusConflict, "NOT_CONNECTED", "")
	return false
}
