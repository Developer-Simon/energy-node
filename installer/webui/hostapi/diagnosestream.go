package hostapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// DiagnoseSections sind die Abschnitte von diag.Sections in der Reihenfolge,
// in der diagnose.sh --stream sie druckt. Das webui-Modul importiert
// internal/diag nicht; dass beide Listen gleich sind, prueft der Test der
// Host-Schicht.
var DiagnoseSections = []string{"bundle", "units", "ports", "mqtt", "webhook", "config", "info", "tailscale", "reboot", "apt"}

// DiagnoseProgress ist ein Zwischenstand der Diagnose. View ist die Ansicht
// mit den bisher fertigen Abschnitten, Pending die noch offenen. Pending
// wird nie als null geschickt: leer ist [].
type DiagnoseProgress struct {
	View    *DiagnoseView `json:"view"`
	Pending []string      `json:"pending"`
}

// DiagnoseStreamer ist eine optionale Faehigkeit eines Backends. emit darf
// nicht mehr aufgerufen werden, sobald DiagnoseStream zurueckkehrt.
type DiagnoseStreamer interface {
	DiagnoseStream(ctx context.Context, emit func(DiagnoseProgress)) error
}

// checkSectionPorts erkennt Port-Checks, deren Subject nur aus Ziffern besteht.
var checkSectionPorts = regexp.MustCompile(`^[0-9]+$`)

// CheckSection ordnet eine Pruefung dem Abschnitt zu, aus dem sie kommt.
// Unbekannte Pruefungen liefern "".
func CheckSection(c Check) string {
	switch {
	case c.Group == "config":
		return "config"
	case c.Subject == "reboot":
		return "reboot"
	case strings.HasPrefix(c.Subject, "shelly-webhook-"):
		return "webhook"
	case strings.HasSuffix(c.Subject, ".service"):
		return "units"
	case checkSectionPorts.MatchString(c.Subject):
		return "ports"
	case c.Subject == "mqtt-login":
		return "mqtt"
	case c.Subject == "tailscale":
		return "tailscale"
	}
	return ""
}

// PartialView liefert eine Kopie der Ansicht, in der die offenen Abschnitte
// fehlen. Die Eingabe bleibt unveraendert. Pruefungen ohne Abschnitt fallen
// weg, solange etwas offen ist, weil sie zu einem offenen Abschnitt gehoeren
// koennten.
func PartialView(full *DiagnoseView, pending []string) *DiagnoseView {
	if full == nil {
		return nil
	}
	open := make(map[string]bool, len(pending))
	for _, section := range pending {
		open[section] = true
	}

	out := *full
	out.Checks = make([]Check, 0, len(full.Checks))
	for _, check := range full.Checks {
		section := CheckSection(check)
		if section == "" && len(pending) > 0 || section != "" && open[section] {
			continue
		}
		out.Checks = append(out.Checks, check)
	}
	if open["units"] {
		out.Units = nil
	}
	if open["ports"] {
		out.Ports = nil
	}
	if open["info"] {
		out.Versions = nil
		out.Devices = nil
	}
	if open["reboot"] {
		out.RebootRequired = false
	}
	if open["apt"] {
		out.SystemUpdates = nil
	}
	if open["bundle"] {
		out.BundleVersion = ""
	}
	return &out
}

// diagnoseSSE schreibt Rahmen fuer den Diagnose-Strom. Schreiben und Flush
// sind serialisiert, weil der Herzschlag aus einer zweiten Goroutine kommt.
type diagnoseSSE struct {
	mu sync.Mutex
	w  http.ResponseWriter
	f  http.Flusher
}

func (d *diagnoseSSE) write(format string, args ...any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fmt.Fprintf(d.w, format, args...)
	d.f.Flush()
}

// keepAlive schickt den Herzschlag, bis stop aufgerufen wird. stop kehrt erst
// zurueck, wenn der Herzschlag beendet ist.
func (d *diagnoseSSE) keepAlive() (stop func()) {
	done := make(chan struct{})
	var finished sync.WaitGroup
	finished.Add(1)
	go func() {
		defer finished.Done()
		ticker := time.NewTicker(heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				d.write(": ping\n\n")
			}
		}
	}()
	return func() {
		close(done)
		finished.Wait()
	}
}

// diagnoseRun waehlt den Weg: ein Streamer liefert Zwischenstaende, sonst
// wird Diagnose einmal aufgerufen und das Ergebnis als einziger Stand
// geschickt.
func (s *Server) diagnoseRun() func(ctx context.Context, emit func(DiagnoseProgress)) error {
	if streamer, ok := s.opts.Backend.(DiagnoseStreamer); ok {
		return streamer.DiagnoseStream
	}
	return func(ctx context.Context, emit func(DiagnoseProgress)) error {
		view, err := s.opts.Backend.Diagnose(ctx)
		if err != nil {
			return err
		}
		emit(DiagnoseProgress{View: view, Pending: []string{}})
		return nil
	}
}

// handleDiagnoseStream: GET streamt die Diagnose als SSE. Jeder Zwischenstand
// ist ein progress-Ereignis, ein Fehler endet mit einem end-Ereignis.
func (s *Server) handleDiagnoseStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", r.Method)
		return
	}
	if !s.requireConnection(w) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "BACKEND_ERROR", "der Server kann nicht streamen")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	stream := &diagnoseSSE{w: w, f: flusher}
	stream.write("retry: 3000\n\n")

	emit := func(progress DiagnoseProgress) {
		if progress.Pending == nil {
			progress.Pending = []string{}
		}
		data, err := json.Marshal(progress)
		if err != nil {
			return
		}
		stream.write("event: progress\ndata: %s\n\n", data)
	}

	// Der Herzschlag steht still, bevor der Handler zurueckkehrt, damit danach
	// nichts mehr geschrieben wird.
	stop := stream.keepAlive()
	err := s.diagnoseRun()(r.Context(), emit)
	stop()

	// Ist der Client weg, schreibt niemand mehr ein Ende.
	if err == nil || r.Context().Err() != nil {
		return
	}
	code, detail := "DIAGNOSE_FAILED", err.Error()
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
