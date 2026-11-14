package hostapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
	"github.com/Developer-Simon/energy-node-webui/hostapi/hostapitest"
)

// streamingFake legt zusaetzlich DiagnoseStream offen, damit der Handler den
// Streamer-Weg nimmt.
type streamingFake struct {
	*hostapitest.FakeBackend
	stream func(ctx context.Context, emit func(hostapi.DiagnoseProgress)) error
}

func (f streamingFake) DiagnoseStream(ctx context.Context, emit func(hostapi.DiagnoseProgress)) error {
	return f.stream(ctx, emit)
}

// sseFrames liest einen SSE-Strom bis zu seinem Ende. Je Rahmen der Rohtext
// ohne Leerzeile am Ende.
func sseFrames(body io.Reader) []string {
	var frames []string
	scanner := bufio.NewScanner(body)
	var block []string
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			block = append(block, line)
			continue
		}
		if len(block) > 0 {
			frames = append(frames, strings.Join(block, "\n"))
			block = nil
		}
	}
	return frames
}

// diagnoseStreamGet oeffnet /api/diagnose/stream ueber einen echten Server und
// liest den Strom bis zu seinem Ende, hoechstens 5 s.
func diagnoseStreamGet(t *testing.T, server *hostapi.Server) (http.Header, []string) {
	t.Helper()
	backend := httptest.NewServer(server.Handler())
	t.Cleanup(backend.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, backend.URL+"/api/diagnose/stream", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("X-Installer-Token", testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/diagnose/stream: %v", err)
	}
	defer resp.Body.Close()
	return resp.Header, sseFrames(resp.Body)
}

// progressPayload liest den JSON-Teil eines progress-Rahmens.
func progressPayload(t *testing.T, frame string) hostapi.DiagnoseProgress {
	t.Helper()
	var progress hostapi.DiagnoseProgress
	if err := json.Unmarshal([]byte(strings.TrimPrefix(frameData(frame), "\ndata: ")), &progress); err != nil {
		t.Fatalf("progress payload: %v (%q)", err, frame)
	}
	return progress
}

func TestCheckSectionCoversEveryChecklistSubject(t *testing.T) {
	cases := []struct {
		check hostapi.Check
		want  string
	}{
		{hostapi.Check{Group: "services", Subject: "energy-node-dashboard.service"}, "units"},
		{hostapi.Check{Group: "system", Subject: "1883"}, "ports"},
		{hostapi.Check{Group: "system", Subject: "shelly-webhook-firewall:8082"}, "webhook"},
		{hostapi.Check{Group: "system", Subject: "shelly-webhook-listener:8082"}, "webhook"},
		{hostapi.Check{Group: "system", Subject: "reboot"}, "reboot"},
		{hostapi.Check{Group: "config", Subject: "config.json"}, "config"},
		{hostapi.Check{Group: "system", Subject: "mqtt-login"}, "mqtt"},
		{hostapi.Check{Group: "system", Subject: "tailscale"}, "tailscale"},
		{hostapi.Check{Group: "system", Subject: "unbekannt"}, ""},
	}
	for _, tc := range cases {
		if got := hostapi.CheckSection(tc.check); got != tc.want {
			t.Errorf("CheckSection(%q) = %q, want %q", tc.check.Subject, got, tc.want)
		}
	}
}

func TestPartialViewHidesPendingSections(t *testing.T) {
	full := &hostapi.DiagnoseView{
		BundleVersion: "v1",
		Units:         map[string]string{"energy-node-dashboard.service": "active"},
		Ports:         map[string]bool{"1883": true},
		Checks: []hostapi.Check{
			{Name: "unit energy-node-dashboard.service", OK: true, Group: "services", Subject: "energy-node-dashboard.service"},
			{Name: "port 1883", OK: true, Group: "system", Subject: "1883"},
			{Name: "tailscale login", OK: false, Group: "system", Subject: "tailscale"},
			{Name: "config.json", OK: false, Group: "config", Subject: "config.json"},
		},
		Versions:      &hostapi.DiagnoseVersions{},
		Devices:       map[string][]hostapi.DeviceEntry{},
		SystemUpdates: &hostapi.SystemUpdates{Count: 2},
	}
	before, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := hostapi.PartialView(full, []string{"config", "tailscale", "apt"})

	for _, check := range got.Checks {
		if check.Subject == "tailscale" || check.Subject == "config.json" {
			t.Fatalf("pending check still present: %+v", check)
		}
	}
	if got.SystemUpdates != nil {
		t.Fatalf("SystemUpdates = %+v, want nil", got.SystemUpdates)
	}
	if !hasCheck(got, "energy-node-dashboard.service") || !hasCheck(got, "1883") {
		t.Fatalf("checks = %+v, want the units and ports checks", got.Checks)
	}
	after, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("input changed:\n before %s\n after  %s", before, after)
	}
}

func hasCheck(view *hostapi.DiagnoseView, subject string) bool {
	for _, check := range view.Checks {
		if check.Subject == subject {
			return true
		}
	}
	return false
}

func TestPartialViewClearsEveryPendingField(t *testing.T) {
	full := &hostapi.DiagnoseView{
		BundleVersion:  "v1",
		Units:          map[string]string{"a.service": "active"},
		Ports:          map[string]bool{"1883": true},
		Versions:       &hostapi.DiagnoseVersions{},
		Devices:        map[string][]hostapi.DeviceEntry{},
		RebootRequired: true,
		SystemUpdates:  &hostapi.SystemUpdates{Count: 1},
	}
	pending := []string{"bundle", "units", "ports", "info", "reboot", "apt"}

	got := hostapi.PartialView(full, pending)

	if got.BundleVersion != "" || got.Units != nil || got.Ports != nil || got.Versions != nil || got.Devices != nil || got.RebootRequired || got.SystemUpdates != nil {
		t.Fatalf("partial view = %+v, want every pending field cleared", got)
	}
	if !full.RebootRequired || full.Units == nil {
		t.Fatalf("input was modified: %+v", full)
	}
}

func TestDiagnoseStreamSendsProgressEvents(t *testing.T) {
	server, _ := newTestServer(t, func(o *hostapi.Options) {
		o.Backend = streamingFake{
			FakeBackend: hostapitest.NewFake(),
			stream: func(ctx context.Context, emit func(hostapi.DiagnoseProgress)) error {
				emit(hostapi.DiagnoseProgress{Pending: []string{"units", "ports"}})
				emit(hostapi.DiagnoseProgress{View: &hostapi.DiagnoseView{BundleVersion: "v1"}})
				return nil
			},
		}
	})
	connectFirst(t, server)

	_, frames := diagnoseStreamGet(t, server)

	var progress []string
	for _, frame := range frames {
		if strings.HasPrefix(frame, "event: progress") {
			progress = append(progress, frame)
		}
	}
	if len(progress) != 2 {
		t.Fatalf("progress frames = %d, want 2: %q", len(progress), frames)
	}
	last := progressPayload(t, progress[len(progress)-1])
	if last.Pending == nil || len(last.Pending) != 0 {
		t.Fatalf("last pending = %#v, want empty", last.Pending)
	}
	if !strings.Contains(progress[len(progress)-1], `"pending":[]`) {
		t.Fatalf("last frame = %q, want pending serialized as []", progress[len(progress)-1])
	}
}

func TestDiagnoseStreamFallsBackToDiagnose(t *testing.T) {
	server, fake := newTestServer(t, nil)
	connectFirst(t, server)

	_, frames := diagnoseStreamGet(t, server)

	if len(frames) < 2 || frames[0] != "retry: 3000" || !strings.HasPrefix(frames[1], "event: progress") {
		t.Fatalf("frames = %q, want retry then one progress", frames)
	}
	if len(frames) != 2 {
		t.Fatalf("frames = %d, want exactly one progress: %q", len(frames), frames)
	}
	progress := progressPayload(t, frames[1])
	if progress.View == nil || progress.View.BundleVersion != fake.DiagnoseView.BundleVersion {
		t.Fatalf("view = %+v, want the full diagnose view", progress.View)
	}
	if progress.Pending == nil || len(progress.Pending) != 0 {
		t.Fatalf("pending = %#v, want empty", progress.Pending)
	}
	if !strings.Contains(frames[1], `"pending":[]`) {
		t.Fatalf("frame = %q, want pending serialized as []", frames[1])
	}
}

func TestDiagnoseStreamEndsWithTheErrorCode(t *testing.T) {
	server, _ := newTestServer(t, func(o *hostapi.Options) {
		o.Backend = streamingFake{
			FakeBackend: hostapitest.NewFake(),
			stream: func(ctx context.Context, emit func(hostapi.DiagnoseProgress)) error {
				emit(hostapi.DiagnoseProgress{Pending: []string{"apt"}})
				return &hostapi.Error{Code: "DIAGNOSE_FAILED", Detail: "Abschnitt kaputt"}
			},
		}
	})
	connectFirst(t, server)

	_, frames := diagnoseStreamGet(t, server)

	if len(frames) == 0 {
		t.Fatal("no frames at all")
	}
	last := frames[len(frames)-1]
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
	if payload.Code != "DIAGNOSE_FAILED" || payload.Detail != "Abschnitt kaputt" {
		t.Fatalf("end payload = %+v", payload)
	}
}

func TestDiagnoseStreamEndsWithADefaultCodeForPlainErrors(t *testing.T) {
	server, _ := newTestServer(t, func(o *hostapi.Options) {
		o.Backend = streamingFake{
			FakeBackend: hostapitest.NewFake(),
			stream: func(ctx context.Context, emit func(hostapi.DiagnoseProgress)) error {
				return context.DeadlineExceeded
			},
		}
	})
	connectFirst(t, server)

	_, frames := diagnoseStreamGet(t, server)

	last := frames[len(frames)-1]
	if !strings.Contains(last, `"code":"DIAGNOSE_FAILED"`) {
		t.Fatalf("last frame = %q, want the DIAGNOSE_FAILED default", last)
	}
}

func TestDiagnoseStreamStopsWhenTheClientLeaves(t *testing.T) {
	returned := make(chan struct{})
	server, _ := newTestServer(t, func(o *hostapi.Options) {
		o.Backend = streamingFake{
			FakeBackend: hostapitest.NewFake(),
			stream: func(ctx context.Context, emit func(hostapi.DiagnoseProgress)) error {
				defer close(returned)
				emit(hostapi.DiagnoseProgress{Pending: []string{"apt"}})
				<-ctx.Done()
				return ctx.Err()
			},
		}
	})
	connectFirst(t, server)
	backend := httptest.NewServer(server.Handler())
	t.Cleanup(backend.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, backend.URL+"/api/diagnose/stream", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("X-Installer-Token", testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/diagnose/stream: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if scanner.Text() == "event: progress" {
			break
		}
	}
	cancel()

	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("the handler did not stop within 1 s of the client leaving")
	}
}

func TestDiagnoseStreamNeedsConnection(t *testing.T) {
	server, _ := newTestServer(t, nil)
	if rec := do(t, server, http.MethodGet, "/api/diagnose/stream", ""); rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 NOT_CONNECTED", rec.Code)
	}
}

func TestDiagnoseStreamMethodNotAllowed(t *testing.T) {
	server, _ := newTestServer(t, nil)
	connectFirst(t, server)
	if rec := do(t, server, http.MethodPost, "/api/diagnose/stream", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
