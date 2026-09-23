package hostapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// heartbeat haelt die SSE-Verbindung offen, wenn ein Schritt lange schweigt.
const heartbeat = 15 * time.Second

// runState haelt den einen laufenden Lauf. Mehr als einen gibt es nie: der
// Betreiber sieht genau einen Fortschritt, und zwei gleichzeitige Laeufe gegen
// denselben Node waeren ohnehin ein Fehler.
type runState struct {
	mu      sync.Mutex
	running bool
	id      string
	cancel  context.CancelFunc
}

// busSink uebersetzt die Meldungen eines Backends in Ereignisse - und ist die
// eine Stelle, an der Geheimnisse gefiltert werden.
type busSink struct {
	bus      *Bus
	redactor *Redactor
}

func (s *busSink) Marker(stepID, state, detail string) {
	s.bus.Publish("step", map[string]string{
		"id":     stepID,
		"state":  state,
		"detail": s.redactor.Line(detail),
	})
}

func (s *busSink) Log(stepID, line string) {
	s.bus.Publish("log", map[string]string{
		"step_id": stepID,
		"line":    s.redactor.Line(line),
	})
}

func (s *busSink) Message(stepID, key string, args map[string]string) {
	// Werte aller Argumente sind zu redigieren.
	redacted := make(map[string]string)
	for k, v := range args {
		redacted[k] = s.redactor.Line(v)
	}
	s.bus.Publish("log", map[string]any{
		"step_id": stepID,
		"key":     key,
		"args":    redacted,
	})
}

// StartRun startet einen Lauf im Hintergrund und liefert seine ID. Ein Wirt,
// der einen Lauf ausserhalb von POST /api/run anstoesst, benutzt dieselbe
// Funktion - damit gibt es genau einen Weg, auf dem ein Lauf beginnt.
func (s *Server) StartRun(ctx context.Context, req RunRequest) (string, error) {
	s.run.mu.Lock()
	if s.run.running {
		s.run.mu.Unlock()
		return "", &Error{Code: "RUN_IN_PROGRESS", Status: http.StatusConflict}
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	id := fmt.Sprintf("run-%d", time.Now().UnixNano())
	s.run.running = true
	s.run.id = id
	s.run.cancel = cancel
	s.run.mu.Unlock()

	req.RunID = id
	sink := &busSink{bus: s.bus, redactor: NewRedactor(req.Secrets()...)}
	s.bus.Publish("run-started", map[string]any{"run_id": id, "mode": string(req.Mode), "only": req.Only})

	go func() {
		defer cancel()
		err := s.opts.Backend.Run(runCtx, req, sink)
		s.bus.Publish("run-finished", runFinishPayload(s.bus, id, err, sink.redactor))

		s.run.mu.Lock()
		s.run.running = false
		s.run.cancel = nil
		s.run.mu.Unlock()
	}()

	return id, nil
}

// lastStepID sucht den zuletzt gemeldeten fehlgeschlagenen Schritt. Der
// Fehlercode allein sagt nicht, wo er auftrat, und die Oberflaeche zeigt
// beides zusammen.
func lastStepID(bus *Bus) string {
	events := bus.Since(0)
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != "step" {
			continue
		}
		data, ok := events[i].Data.(map[string]string)
		if ok && data["state"] == "fail" {
			return data["id"]
		}
	}
	return ""
}

// runFinishPayload builds the "run-finished" event data both StartRun and
// ResumeRun publish, so a resumed job's result looks identical on the wire
// to a live one. redactor may be nil (ResumeRun has no secrets to redact --
// Plan D's jobs never carry any, see Global Constraints).
func runFinishPayload(bus *Bus, id string, err error, redactor *Redactor) map[string]any {
	payload := map[string]any{"run_id": id, "ok": err == nil}
	if err == nil {
		return payload
	}
	detail := err.Error()
	var typed *Error
	switch {
	case errors.As(err, &typed):
		payload["code"] = typed.Code
		payload["step_id"] = lastStepID(bus)
		detail = typed.Detail
	case errors.Is(err, context.Canceled):
		payload["code"] = "RUN_CANCELLED"
		detail = ""
	default:
		payload["code"] = "BACKEND_ERROR"
	}
	if detail != "" {
		if redactor != nil {
			detail = redactor.Line(detail)
		}
		payload["detail"] = detail
	}
	return payload
}

// ResumeRun re-arms runState for a job the previous process instance
// started before this one replaced it (Plan D: the updater unit keeps
// running across the dashboard's own self-update restart). Unlike
// StartRun, it does not call Backend.Run -- the steps are already running
// in the updater unit, a process outside this one entirely. The caller
// (dashboard/internal/updaterhost) drives the job the rest of the way by
// tailing job/log and publishing onto Bus() directly, then calls the
// returned finish func exactly once, when the job reaches a terminal
// state.
func (s *Server) ResumeRun(id string) (finish func(err error)) {
	s.run.mu.Lock()
	s.run.running = true
	s.run.id = id
	s.run.cancel = func() {} // nothing here to cancel; see Global Constraints
	s.run.mu.Unlock()

	return func(err error) {
		s.bus.Publish("run-finished", runFinishPayload(s.bus, id, err, nil))
		s.run.mu.Lock()
		s.run.running = false
		s.run.cancel = nil
		s.run.mu.Unlock()
	}
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	if !s.requireConnection(w) {
		return
	}
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	switch req.Mode {
	case ModeInstall, ModeRedeploy, ModeRepair, ModePrepare:
	default:
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "unbekannter mode: "+string(req.Mode))
		return
	}
	if req.Mode == ModeRepair && req.Only == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "repair ohne only")
		return
	}

	id, err := s.StartRun(r.Context(), req)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"run_id": id, "seq": s.bus.Seq()})
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	s.run.mu.Lock()
	cancel := s.run.cancel
	s.run.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": cancel != nil})
}

func (s *Server) handleDiagnose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	if !s.requireConnection(w) {
		return
	}
	view, err := s.opts.Backend.Diagnose(r.Context())
	if err != nil {
		writeBackendError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "BACKEND_ERROR", "der Server kann nicht streamen")
		return
	}

	since := int64(0)
	if raw := r.URL.Query().Get("since"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
			since = parsed
		}
	} else if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
			since = parsed
		}
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	// Ein Proxy, der puffert, macht den Strom nutzlos. Caddy und nginx
	// verstehen diesen Kopf.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	s.run.mu.Lock()
	running, runID := s.run.running, s.run.id
	s.run.mu.Unlock()
	writeSSE(w, Event{Seq: s.bus.Seq(), Type: "hello", At: time.Now().UnixMilli(), Data: map[string]any{
		"seq": s.bus.Seq(), "running": running, "run_id": runID, "bus": s.bus.ID(),
	}})
	flusher.Flush()

	events, cancel := s.bus.Subscribe(since)
	defer cancel()

	// once=1 schreibt den Rueckstand und beendet den Strom. Nur Tests benutzen
	// das - ein Browser will die offene Verbindung.
	if r.URL.Query().Get("once") == "1" {
		for {
			select {
			case event, open := <-events:
				if !open {
					return
				}
				writeSSE(w, event)
			default:
				flusher.Flush()
				return
			}
		}
	}

	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-events:
			if !open {
				return
			}
			writeSSE(w, event)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, event Event) {
	payload, err := json.Marshal(event.Data)
	if err != nil {
		payload = []byte(`{}`)
	}
	fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Seq, event.Type, withAt(payload, event.At))
}

// withAt setzt "at" als erstes Feld in ein JSON-Objekt ein. Alles, was kein
// Objekt ist, und ein Zeitpunkt 0 bleiben unberuehrt - der Vertrag kennt nur
// Objekte, und 0 hiesse "unbekannt", nicht 1970.
func withAt(payload []byte, at int64) []byte {
	trimmed := bytes.TrimLeft(payload, " \t\r\n")
	if at == 0 || len(trimmed) < 2 || trimmed[0] != '{' {
		return payload
	}
	stamp := []byte(fmt.Sprintf(`"at":%d`, at))
	rest := bytes.TrimLeft(trimmed[1:], " \t\r\n")
	out := make([]byte, 0, len(trimmed)+len(stamp)+1)
	out = append(out, '{')
	out = append(out, stamp...)
	if len(rest) > 0 && rest[0] != '}' {
		out = append(out, ',')
	}
	return append(out, rest...)
}
