package hostapi_test

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
	"github.com/Developer-Simon/energy-node-webui/hostapi/hostapitest"
)

// readEvents liest einen SSE-Strom, bis want Ereignisse angekommen sind oder
// der Strom endet.
func readEvents(t *testing.T, body *strings.Reader) []hostapi.Event {
	t.Helper()
	var out []hostapi.Event
	scanner := bufio.NewScanner(body)
	var current hostapi.Event
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			current.Type = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			var payload any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err != nil {
				t.Fatalf("event data is not JSON: %v", err)
			}
			current.Data = payload
		case line == "":
			if current.Type != "" {
				out = append(out, current)
				current = hostapi.Event{}
			}
		}
	}
	return out
}

func startRun(t *testing.T, server *hostapi.Server, body string) map[string]any {
	t.Helper()
	rec := do(t, server, http.MethodPost, "/api/run", body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	return payload
}

func waitForRunToFinish(t *testing.T, server *hostapi.Server) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, event := range server.Bus().Since(0) {
			if event.Type == "run-finished" {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the run did not finish within two seconds")
}

func TestRunPublishesTheWholeMarkerStream(t *testing.T) {
	server, fake := newTestServer(t, nil)
	connectFirst(t, server)
	fake.Script(
		hostapitest.FakeStep{ID: "10", State: "ok", Log: []string{"apt: nothing to do"}},
		hostapitest.FakeStep{ID: "40", State: "skip", Detail: "nicht ausgewaehlt"},
	)
	startRun(t, server, `{"mode":"install","mqtt_password":"hunter2","admin_password":"s3cret"}`)
	waitForRunToFinish(t, server)

	var types []string
	for _, event := range server.Bus().Since(0) {
		types = append(types, event.Type)
	}
	joined := strings.Join(types, ",")
	for _, want := range []string{"run-started", "step", "log", "run-finished"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the stream carries no %q event: %v", want, types)
		}
	}
}

func TestNoSecretEverReachesTheStream(t *testing.T) {
	server, fake := newTestServer(t, nil)
	connectFirst(t, server)
	fake.Script(hostapitest.FakeStep{
		ID:  "60",
		Log: []string{"mosquitto_passwd -b /etc/energy-node/mqtt.pw node hunter2", "admin password: s3cret"},
	})
	startRun(t, server, `{"mode":"install","mqtt_password":"hunter2","admin_password":"s3cret"}`)
	waitForRunToFinish(t, server)

	raw, err := json.Marshal(server.Bus().Since(0))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, secret := range []string{"hunter2", "s3cret"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("the event stream carries %q in the clear:\n%s", secret, raw)
		}
	}
	if !strings.Contains(string(raw), hostapi.Mask) {
		t.Errorf("nothing was masked at all - the redactor did not run")
	}
}

func TestASecondRunIsRefusedWhileOneIsRunning(t *testing.T) {
	server, fake := newTestServer(t, nil)
	connectFirst(t, server)
	fake.StepDelay = 200 * time.Millisecond
	fake.Script(hostapitest.FakeStep{ID: "10"}, hostapitest.FakeStep{ID: "20"})
	startRun(t, server, `{"mode":"install"}`)

	rec := do(t, server, http.MethodPost, "/api/run", `{"mode":"install"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 RUN_IN_PROGRESS", rec.Code)
	}
	do(t, server, http.MethodPost, "/api/cancel", "")
}

func TestCancelStopsTheRun(t *testing.T) {
	server, fake := newTestServer(t, nil)
	connectFirst(t, server)
	fake.StepDelay = 100 * time.Millisecond
	fake.Script(hostapitest.FakeStep{ID: "10"}, hostapitest.FakeStep{ID: "20"}, hostapitest.FakeStep{ID: "30"})
	startRun(t, server, `{"mode":"install"}`)

	if rec := do(t, server, http.MethodPost, "/api/cancel", ""); rec.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, want 200", rec.Code)
	}
	waitForRunToFinish(t, server)

	var finished map[string]any
	for _, event := range server.Bus().Since(0) {
		if event.Type == "run-finished" {
			finished, _ = event.Data.(map[string]any)
		}
	}
	if finished == nil {
		t.Fatalf("no run-finished event after cancel")
	}
	if ok, _ := finished["ok"].(bool); ok {
		t.Errorf("a cancelled run must not report ok=true: %v", finished)
	}
}

func TestAFailingStepEndsTheRunWithItsCode(t *testing.T) {
	server, fake := newTestServer(t, nil)
	connectFirst(t, server)
	fake.Script(hostapitest.FakeStep{ID: "50", State: "fail", Detail: "PIP_EXTERNALLY_MANAGED"})
	startRun(t, server, `{"mode":"install"}`)
	waitForRunToFinish(t, server)

	for _, event := range server.Bus().Since(0) {
		if event.Type != "run-finished" {
			continue
		}
		data := event.Data.(map[string]any)
		if data["code"] != "PIP_EXTERNALLY_MANAGED" || data["step_id"] != "50" {
			t.Fatalf("run-finished = %v, want the fault code and the step", data)
		}
		if data["ok"].(bool) {
			t.Fatalf("a failing run must report ok=false")
		}
		return
	}
	t.Fatalf("no run-finished event")
}

func TestEventsStreamStartsWithHelloAndReplaysFromSince(t *testing.T) {
	server, fake := newTestServer(t, nil)
	connectFirst(t, server)
	fake.Script(hostapitest.FakeStep{ID: "10"}, hostapitest.FakeStep{ID: "20"})
	startRun(t, server, `{"mode":"install"}`)
	waitForRunToFinish(t, server)

	// Der Strom wird mit einem Kontext gelesen, der gleich nach dem Rueckstand
	// endet - sonst wartete der Test auf das naechste Ereignis.
	req := httptest.NewRequest(http.MethodGet, "/api/events?since=0&once=1", nil)
	req.Header.Set("X-Installer-Token", testToken)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: hello") {
		t.Errorf("the stream does not start with hello:\n%s", body)
	}
	if !strings.Contains(body, "event: run-started") || !strings.Contains(body, "event: run-finished") {
		t.Errorf("the replay is incomplete:\n%s", body)
	}
	if !strings.Contains(body, "id: 1") {
		t.Errorf("the events carry no id: field - a browser cannot resume without it:\n%s", body)
	}

	events := readEvents(t, strings.NewReader(body))
	if len(events) < 4 {
		t.Errorf("parsed %d events, want the full replay", len(events))
	}
}

func TestEventsSinceSkipsWhatTheClientAlreadySaw(t *testing.T) {
	server, fake := newTestServer(t, nil)
	connectFirst(t, server)
	fake.Script(hostapitest.FakeStep{ID: "10"}, hostapitest.FakeStep{ID: "20"})
	startRun(t, server, `{"mode":"install"}`)
	waitForRunToFinish(t, server)

	last := server.Bus().Seq()
	req := httptest.NewRequest(http.MethodGet, "/api/events?since="+itoa(last)+"&once=1", nil)
	req.Header.Set("X-Installer-Token", testToken)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "event: run-started") {
		t.Errorf("since=<latest> must not replay old events:\n%s", body)
	}
	if !strings.Contains(body, "event: hello") {
		t.Errorf("hello must go out on every connection:\n%s", body)
	}
}

func itoa(v int64) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.Trim(strings.Join(strings.Fields(formatInt(v)), ""), " "), "\n", ""))
}

func formatInt(v int64) string {
	return jsonNumber(v)
}

func jsonNumber(v int64) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func TestRunIsRefusedWithoutAConnection(t *testing.T) {
	server, _ := newTestServer(t, nil)
	rec := do(t, server, http.MethodPost, "/api/run", `{"mode":"install"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 NOT_CONNECTED", rec.Code)
	}
}

func TestRunRejectsAnUnknownMode(t *testing.T) {
	server, _ := newTestServer(t, nil)
	connectFirst(t, server)
	rec := do(t, server, http.MethodPost, "/api/run", `{"mode":"reformat"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestDiagnoseReturnsTheChecklist(t *testing.T) {
	server, _ := newTestServer(t, nil)
	connectFirst(t, server)
	rec := do(t, server, http.MethodGet, "/api/diagnose", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got hostapi.DiagnoseView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if len(got.Checks) == 0 || got.Units["energy-node-dashboard.service"] == "" {
		t.Errorf("DiagnoseView = %+v", got)
	}
}

func TestRepairRunsExactlyOneStep(t *testing.T) {
	server, fake := newTestServer(t, nil)
	connectFirst(t, server)
	fake.Script(hostapitest.FakeStep{ID: "20", State: "ok"})
	startRun(t, server, `{"mode":"repair","only":"20"}`)
	waitForRunToFinish(t, server)

	if fake.LastRun.Only != "20" || fake.LastRun.Mode != hostapi.ModeRepair {
		t.Errorf("the backend got %+v, want a repair of step 20", fake.LastRun)
	}
}

func TestEveryStreamedEventCarriesItsTimestamp(t *testing.T) {
	server, fake := newTestServer(t, nil)
	connectFirst(t, server)
	fake.Script(hostapitest.FakeStep{ID: "10", State: "ok", Log: []string{"apt: nothing to do"}})
	startRun(t, server, `{"mode":"install"}`)
	waitForRunToFinish(t, server)

	rec := do(t, server, http.MethodGet, "/api/events?since=0&once=1", "")
	events := readEvents(t, strings.NewReader(rec.Body.String()))
	if len(events) < 4 {
		t.Fatalf("got %d events, want hello, run-started, step, log, run-finished", len(events))
	}
	for _, event := range events {
		data, ok := event.Data.(map[string]any)
		if !ok {
			t.Fatalf("%s: data is not an object: %#v", event.Type, event.Data)
		}
		at, ok := data["at"].(float64)
		if !ok || at <= 0 {
			t.Errorf("%s carries no usable at: %#v", event.Type, data)
		}
	}
}

func TestResumeRunPublishesRunFinishedAndClearsRunningState(t *testing.T) {
	fake := hostapitest.NewFake()
	server, err := hostapi.New(hostapi.Options{Backend: fake, Catalogs: testCatalogs(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	finish := server.ResumeRun("resumed-1")
	events, cancel := server.Bus().Subscribe(0)
	defer cancel()

	finish(nil)

	select {
	case event := <-events:
		if event.Type != "run-finished" {
			t.Fatalf("event.Type = %q, want run-finished", event.Type)
		}
		data := event.Data.(map[string]any)
		if data["run_id"] != "resumed-1" || data["ok"] != true {
			t.Fatalf("data = %+v", data)
		}
	default:
		t.Fatal("expected a run-finished event")
	}
}
