package hostapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
	"github.com/Developer-Simon/energy-node-webui/hostapi/hostapitest"
)

// unitStream ist ein gelesener SSE-Strom: je Rahmen der Rohtext ohne
// Leerzeile am Ende, dazu der Antwortkopf.
type unitStream struct {
	header http.Header
	frames []string
}

// streamUnitLogs oeffnet /api/unit-logs ueber einen echten Server, weil ein
// Recorder nicht gleichzeitig gelesen und abgebrochen werden kann. Liest bis
// zum Rahmen mit id stopAt (dann bricht der Kontext ab) oder bis zum Ende des
// Stroms, wenn stopAt leer ist.
func streamUnitLogs(t *testing.T, server *hostapi.Server, query string, headers map[string]string, stopAt string) unitStream {
	t.Helper()
	backend := httptest.NewServer(server.Handler())
	t.Cleanup(backend.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, backend.URL+"/api/unit-logs"+query, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("X-Installer-Token", testToken)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/unit-logs: %v", err)
	}
	defer resp.Body.Close()

	out := unitStream{header: resp.Header}
	scanner := bufio.NewScanner(resp.Body)
	var block []string
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			block = append(block, line)
			continue
		}
		if len(block) == 0 {
			continue
		}
		frame := strings.Join(block, "\n")
		block = nil
		out.frames = append(out.frames, frame)
		if stopAt != "" && hasID(frame, stopAt) {
			cancel()
			break
		}
	}
	return out
}

// frameData liefert den data-Teil eines Rahmens inklusive Zeilenumbruch.
func frameData(frame string) string {
	idx := strings.Index(frame, "\ndata: ")
	if idx < 0 {
		return ""
	}
	return frame[idx:]
}

// hasID meldet, ob ein Rahmen genau die Zeile "id: <id>" traegt.
func hasID(frame, id string) bool {
	for _, line := range strings.Split(frame, "\n") {
		if line == "id: "+id {
			return true
		}
	}
	return false
}

func unitLogsFixture(fake *hostapitest.FakeBackend) {
	fake.UnitLogHostname = "energy-node"
	fake.UnitLogLines = []hostapi.UnitLogLine{
		{Unit: "energy-node-dashboard.service", At: 1730000000000, Priority: 6, Ident: "energy-node", PID: "812", Message: "erste Zeile", Cursor: "c1"},
		{Unit: "energy-node-dashboard.service", At: 1730000001000, Priority: 4, Ident: "energy-node", PID: "812", Message: "zweite Zeile", Cursor: "c2"},
	}
	fake.UnitStatuses = []hostapi.UnitStatus{
		{Unit: "energy-node-dashboard.service", Load: "loaded", Active: "active", Sub: "running", Result: "success", Restarts: 9, PID: 812, Since: 1730000000000},
	}
}

func TestUnitLogsStreamsHelloStatusAndLines(t *testing.T) {
	server, fake := newTestServer(t, nil)
	unitLogsFixture(fake)
	connectFirst(t, server)

	got := streamUnitLogs(t, server, "", nil, "c2")

	if ct := got.header.Get("Content-Type"); ct != "text/event-stream; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	want := []string{"retry: 3000", "event: hello", "event: status", "id: c1\nevent: line", "id: c2\nevent: line"}
	if len(got.frames) < len(want) {
		t.Fatalf("frames = %q, want at least %d", got.frames, len(want))
	}
	for i, prefix := range want {
		if !strings.HasPrefix(got.frames[i], prefix) {
			t.Fatalf("frame %d = %q, want prefix %q", i, got.frames[i], prefix)
		}
	}
	if !strings.Contains(got.frames[1], `"hostname":"energy-node"`) {
		t.Fatalf("hello = %q, want the hostname", got.frames[1])
	}
	if !strings.Contains(got.frames[2], `"restarts":9`) {
		t.Fatalf("status = %q, want restarts 9", got.frames[2])
	}
	if strings.Contains(got.frames[1], "id: ") || strings.Contains(got.frames[2], "id: ") {
		t.Fatalf("hello and status must not carry an id: %q / %q", got.frames[1], got.frames[2])
	}
}

func TestUnitLogsUsesFixedUnitsAndBacklog(t *testing.T) {
	server, fake := newTestServer(t, nil)
	unitLogsFixture(fake)
	fake.UnitLogsErr = &hostapi.Error{Code: "UNIT_LOGS_FAILED", Detail: "stop after the hello"}
	connectFirst(t, server)

	streamUnitLogs(t, server, "?unit=ssh.service", nil, "")

	if len(fake.UnitLogsSeen) != 1 {
		t.Fatalf("calls = %d, want 1", len(fake.UnitLogsSeen))
	}
	seen := fake.UnitLogsSeen[0]
	if seen.Backlog != 200 || seen.AfterCursor != "" || len(seen.Units) != len(hostapi.LiveUnits) {
		t.Fatalf("request = %+v, want the fixed units with backlog 200", seen)
	}
	for i, unit := range hostapi.LiveUnits {
		if seen.Units[i] != unit {
			t.Fatalf("units = %v, want %v", seen.Units, hostapi.LiveUnits)
		}
	}
	if hostapi.LiveBacklog != 200 || len(hostapi.LiveUnits) != 2 {
		t.Fatalf("LiveBacklog = %d, LiveUnits = %v", hostapi.LiveBacklog, hostapi.LiveUnits)
	}
}

func TestUnitLogsResumesAfterLastEventID(t *testing.T) {
	server, fake := newTestServer(t, nil)
	unitLogsFixture(fake)
	connectFirst(t, server)

	got := streamUnitLogs(t, server, "", map[string]string{"Last-Event-ID": "c1"}, "c2")

	seen := fake.UnitLogsSeen[0]
	if seen.Backlog != 0 || seen.AfterCursor != "c1" {
		t.Fatalf("request = %+v, want backlog 0 after c1", seen)
	}
	for _, frame := range got.frames {
		if hasID(frame, "c1") {
			t.Fatalf("stream repeats c1: %q", frame)
		}
	}
	if !hasID(got.frames[len(got.frames)-1], "c2") {
		t.Fatalf("last frame = %q, want c2", got.frames[len(got.frames)-1])
	}
}

func TestUnitLogsRejectsAMalformedCursor(t *testing.T) {
	server, fake := newTestServer(t, nil)
	unitLogsFixture(fake)
	fake.UnitLogsErr = &hostapi.Error{Code: "UNIT_LOGS_FAILED", Detail: "stop"}
	connectFirst(t, server)

	streamUnitLogs(t, server, "", map[string]string{"Last-Event-ID": "a'b"}, "")

	seen := fake.UnitLogsSeen[0]
	if seen.Backlog != 200 || seen.AfterCursor != "" {
		t.Fatalf("request = %+v, want the malformed cursor ignored", seen)
	}
}

func TestUnitLogsEndEventCarriesBackendError(t *testing.T) {
	server, fake := newTestServer(t, nil)
	fake.UnitLogsErr = &hostapi.Error{Code: "UNIT_LOGS_FAILED", Detail: "No journal files were opened"}
	connectFirst(t, server)

	got := streamUnitLogs(t, server, "", nil, "")

	if len(got.frames) == 0 {
		t.Fatal("no frames at all")
	}
	last := got.frames[len(got.frames)-1]
	if !strings.HasPrefix(last, "event: end\ndata: ") {
		t.Fatalf("last frame = %q, want event: end", last)
	}
	var payload struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(frameData(last), "\ndata: ")), &payload); err != nil {
		t.Fatalf("end payload: %v (%q)", err, last)
	}
	if payload.Code != "UNIT_LOGS_FAILED" || payload.Detail != "No journal files were opened" {
		t.Fatalf("end payload = %+v", payload)
	}
}

// withoutUnitLogs ist ein Backend, das die Fähigkeit nicht hat: es legt nur
// die Pflichtmethoden des Vertrags offen, nicht die optionalen.
type withoutUnitLogs struct{ hostapi.Backend }

func TestUnitLogsNotSupported(t *testing.T) {
	server, err := hostapi.New(hostapi.Options{Backend: withoutUnitLogs{hostapitest.NewFake()}, Catalogs: testCatalogs(t), Token: testToken, Language: "de"})
	if err != nil {
		t.Fatalf("hostapi.New: %v", err)
	}
	connectFirst(t, server)

	rec := do(t, server, http.MethodGet, "/api/unit-logs", "")
	if rec.Code != http.StatusNotImplemented || !strings.Contains(rec.Body.String(), "NOT_SUPPORTED") {
		t.Fatalf("status = %d body = %s, want 501 NOT_SUPPORTED", rec.Code, rec.Body.String())
	}

	boot := do(t, server, http.MethodGet, "/api/bootstrap", "")
	if strings.Contains(boot.Body.String(), "unit_logs") {
		t.Fatalf("bootstrap advertises unit_logs without the capability: %s", boot.Body.String())
	}
}

func TestBootstrapAdvertisesUnitLogs(t *testing.T) {
	server, _ := newTestServer(t, nil)
	boot := do(t, server, http.MethodGet, "/api/bootstrap", "")
	if !strings.Contains(boot.Body.String(), `"unit_logs":true`) {
		t.Fatalf("bootstrap = %s, want unit_logs true", boot.Body.String())
	}
}

func TestUnitLogsNeedsConnection(t *testing.T) {
	server, _ := newTestServer(t, nil)
	if rec := do(t, server, http.MethodGet, "/api/unit-logs", ""); rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 NOT_CONNECTED", rec.Code)
	}
}

func TestUnitLogsMethodNotAllowed(t *testing.T) {
	server, _ := newTestServer(t, nil)
	connectFirst(t, server)
	if rec := do(t, server, http.MethodPost, "/api/unit-logs", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
