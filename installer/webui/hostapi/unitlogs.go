package hostapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sync"
	"time"
)

// LiveUnits sind die einzigen Units, deren Log die Diagnose zeigt. Fest auf
// dem Server: die anderen Dienste bleiben dem Dashboard vorbehalten.
var LiveUnits = []string{"energy-node-dashboard.service", "energy-node-updater.service"}

// LiveBacklog ist die Zahl gepufferter Zeilen, die ein neuer Strom vorab
// schickt.
const LiveBacklog = 200

// unitCursorPattern erlaubt nur Journal-Cursor-Zeichen. Ein Last-Event-ID mit
// anderen Zeichen wird ignoriert, sonst liefe er ungeprueft in den Befehl.
var unitCursorPattern = regexp.MustCompile(`^[A-Za-z0-9=;_\-]+$`)

// UnitLogsRequest beschreibt einen Strom. Backlog ist 0, wenn AfterCursor
// gesetzt ist: dann kommt nur, was nach dem Cursor steht.
type UnitLogsRequest struct {
	Units       []string
	Backlog     int
	AfterCursor string
}

// UnitLogLine ist eine Journal-Zeile. Cursor steht nicht im JSON, er wird als
// id des SSE-Rahmens geschickt.
type UnitLogLine struct {
	Unit     string `json:"unit"`
	At       int64  `json:"at"`              // Unix-ms aus __REALTIME_TIMESTAMP
	Priority int    `json:"prio"`            // 0..7, Vorgabe 6
	Ident    string `json:"ident,omitempty"` // SYSLOG_IDENTIFIER
	PID      string `json:"pid,omitempty"`   // _PID
	Message  string `json:"msg"`
	Cursor   string `json:"-"`
}

// UnitStatus ist der Zustand einer Unit, wie systemctl ihn meldet.
type UnitStatus struct {
	Unit       string `json:"unit"`
	Load       string `json:"load"`   // loaded | not-found | ...
	Active     string `json:"active"` // active | activating | failed | inactive ...
	Sub        string `json:"sub"`
	Result     string `json:"result"`
	ExitStatus int    `json:"exit_status"`
	Restarts   int    `json:"restarts"`
	PID        int    `json:"pid"`
	Since      int64  `json:"since"` // Unix-ms ActiveEnterTimestamp, 0 = unbekannt
}

// UnitLogSink nimmt, was ein Backend meldet. Die Reihenfolge ist: Hello
// einmal, dann Status und Line in beliebiger Mischung.
type UnitLogSink interface {
	Hello(hostname string)
	Line(line UnitLogLine)
	Status(status UnitStatus)
}

// UnitLogsProvider ist eine optionale Faehigkeit eines Backends. UnitLogs
// blockiert, bis ctx endet oder ein Fehler auftritt.
type UnitLogsProvider interface {
	UnitLogs(ctx context.Context, req UnitLogsRequest, sink UnitLogSink) error
}

// unitLogStream serialisiert Schreiben und Flush: Zeilen, Status und der
// Herzschlag kommen aus verschiedenen Goroutinen.
type unitLogStream struct {
	mu sync.Mutex
	w  http.ResponseWriter
	f  http.Flusher
}

func (u *unitLogStream) write(format string, args ...any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	fmt.Fprintf(u.w, format, args...)
	u.f.Flush()
}

func (u *unitLogStream) Hello(hostname string) {
	data, err := json.Marshal(map[string]any{"units": LiveUnits, "hostname": hostname})
	if err != nil {
		return
	}
	u.write("event: hello\ndata: %s\n\n", data)
}

func (u *unitLogStream) Line(line UnitLogLine) {
	data, err := json.Marshal(line)
	if err != nil {
		return
	}
	if !unitCursorPattern.MatchString(line.Cursor) {
		u.write("event: line\ndata: %s\n\n", data)
		return
	}
	u.write("id: %s\nevent: line\ndata: %s\n\n", line.Cursor, data)
}

func (u *unitLogStream) Status(status UnitStatus) {
	data, err := json.Marshal(status)
	if err != nil {
		return
	}
	u.write("event: status\ndata: %s\n\n", data)
}

// handleUnitLogs: GET streamt das Log der beiden Dienste als SSE. Die Units
// stehen fest, ein Parameter `unit` wird ignoriert.
func (s *Server) handleUnitLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	if !s.requireConnection(w) {
		return
	}
	provider, ok := s.opts.Backend.(UnitLogsProvider)
	if !ok {
		writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "BACKEND_ERROR", "der Server kann nicht streamen")
		return
	}

	req := UnitLogsRequest{Units: append([]string(nil), LiveUnits...), Backlog: LiveBacklog}
	if cursor := r.Header.Get("Last-Event-ID"); unitCursorPattern.MatchString(cursor) {
		req.Backlog = 0
		req.AfterCursor = cursor
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	stream := &unitLogStream{w: w, f: flusher}
	stream.write("retry: 3000\n\n")

	// Der Herzschlag laeuft neben dem Backend. Vor der Rueckkehr steht er
	// still, damit nach dem Handler nichts mehr geschrieben wird.
	done := make(chan struct{})
	var heartbeatDone sync.WaitGroup
	heartbeatDone.Add(1)
	go func() {
		defer heartbeatDone.Done()
		ticker := time.NewTicker(heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				stream.write(": ping\n\n")
			}
		}
	}()

	err := provider.UnitLogs(r.Context(), req, stream)
	close(done)
	heartbeatDone.Wait()

	// Ist der Client weg, schreibt niemand mehr ein Ende. Sonst meldet der
	// Strom den Fehler, und die Karte zeigt ihn statt eines leeren Logs.
	if err == nil || r.Context().Err() != nil {
		return
	}
	code, detail := "UNIT_LOGS_FAILED", err.Error()
	var typed *Error
	if errors.As(err, &typed) {
		code, detail = typed.Code, typed.Detail
	}
	data, marshalErr := json.Marshal(map[string]string{"code": code, "detail": detail})
	if marshalErr != nil {
		return
	}
	stream.write("event: end\ndata: %s\n\n", data)
}
