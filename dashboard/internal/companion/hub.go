// Package companion hält den Prüfkanal zur Home-Assistant-Companion-
// Integration. Die Integration öffnet einen SSE-Strom, auf dem das Dashboard
// Anmeldedaten zur Prüfung schickt, und antwortet über Resolve.
package companion

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
)

// VerifyResult ist die Antwort der Companion-Integration auf eine Prüfung.
type VerifyResult struct {
	OK       bool   `json:"ok"`
	HAUserID string `json:"ha_user_id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	IsAdmin  bool   `json:"is_admin"`
}

// PingInterval ist der Abstand der Keepalive-Ereignisse auf dem Strom.
const PingInterval = 25 * time.Second

var _ auth.HAVerifier = (*Hub)(nil)

// verifyRequest ist der Inhalt des Ereignisses "verify".
type verifyRequest struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// stream ist ein offener SSE-Strom der Companion-Integration.
type stream struct {
	out     chan verifyRequest
	done    chan struct{}
	endOnce sync.Once
}

func newStream() *stream {
	return &stream{out: make(chan verifyRequest), done: make(chan struct{})}
}

func (s *stream) end() {
	s.endOnce.Do(func() { close(s.done) })
}

// Hub verteilt Prüfaufträge an den aktuellen Strom und sammelt die Antworten.
type Hub struct {
	mu      sync.Mutex
	current *stream
	pending map[string]chan VerifyResult
}

// NewHub liefert einen Hub ohne Strom.
func NewHub() *Hub {
	return &Hub{pending: make(map[string]chan VerifyResult)}
}

// VerifyHA schickt Benutzername und Passwort über den Strom und wartet auf
// die Antwort. Ohne Strom, bei Ende des Stroms oder bei Ablauf von ctx
// kommt ErrHomeAssistantUnavailable zurück.
func (h *Hub) VerifyHA(ctx context.Context, username, password string) (auth.HAIdentity, error) {
	h.mu.Lock()
	s := h.current
	if s == nil {
		h.mu.Unlock()
		return auth.HAIdentity{}, auth.ErrHomeAssistantUnavailable
	}
	id, err := newRequestID()
	if err != nil {
		h.mu.Unlock()
		return auth.HAIdentity{}, auth.ErrHomeAssistantUnavailable
	}
	answer := make(chan VerifyResult, 1)
	h.pending[id] = answer
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
	}()

	req := verifyRequest{ID: id, Username: username, Password: password}
	select {
	case s.out <- req:
	case <-s.done:
		return auth.HAIdentity{}, auth.ErrHomeAssistantUnavailable
	case <-ctx.Done():
		return auth.HAIdentity{}, auth.ErrHomeAssistantUnavailable
	}

	select {
	case result := <-answer:
		if !result.OK {
			return auth.HAIdentity{}, auth.ErrInvalidCredentials
		}
		if result.HAUserID == "" {
			return auth.HAIdentity{}, auth.ErrHomeAssistantUnavailable
		}
		return auth.HAIdentity{
			UserID:   result.HAUserID,
			Username: result.Username,
			Name:     result.Name,
			Admin:    result.IsAdmin,
		}, nil
	case <-s.done:
		return auth.HAIdentity{}, auth.ErrHomeAssistantUnavailable
	case <-ctx.Done():
		return auth.HAIdentity{}, auth.ErrHomeAssistantUnavailable
	}
}

// ServeStream führt einen SSE-Strom für die Companion-Integration. Ein neuer
// Strom beendet den vorigen. onConnect (falls gesetzt) läuft, sobald der
// Strom registriert und die Kopfzeilen gesendet sind.
func (h *Hub) ServeStream(w http.ResponseWriter, r *http.Request, onConnect func()) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}

	s := newStream()
	h.mu.Lock()
	if h.current != nil {
		h.current.end()
	}
	h.current = s
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		if h.current == s {
			h.current = nil
		}
		h.mu.Unlock()
		s.end()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	flusher.Flush()

	if onConnect != nil {
		onConnect()
	}

	ticker := time.NewTicker(PingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.done:
			return
		case req := <-s.out:
			payload, err := json.Marshal(req)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "event: verify\ndata: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := fmt.Fprint(w, "event: ping\ndata: {}\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// Resolve nimmt die Antwort auf die Prüfung id entgegen. Es liefert false für
// unbekannte oder bereits beantwortete ids.
func (h *Hub) Resolve(id string, result VerifyResult) bool {
	h.mu.Lock()
	answer, ok := h.pending[id]
	if ok {
		delete(h.pending, id)
	}
	h.mu.Unlock()
	if !ok {
		return false
	}
	answer <- result
	return true
}

// Connected meldet, ob ein Strom offen ist.
func (h *Hub) Connected() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.current != nil
}

// Close beendet den aktuellen Strom. Offene Prüfungen enden mit
// ErrHomeAssistantUnavailable.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.current != nil {
		h.current.end()
		h.current = nil
	}
}

func newRequestID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
