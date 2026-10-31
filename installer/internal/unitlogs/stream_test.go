package unitlogs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

var testUnits = []string{"energy-node-dashboard.service", "energy-node-updater.service"}

const activeStatus = "LoadState=loaded\nActiveState=active\nSubState=running\nMainPID=812\nActiveEnterTimestamp=@1730451662\n"

// fakeRunner antwortet auf hostname und systemctl show mit festen Texten. Der
// Journal-Befehl schreibt die vorgegebenen Zeilen und blockiert dann bis ctx
// endet, oder endet sofort mit journalFail auf stderr.
type fakeRunner struct {
	hostname     string
	status       string
	journalLines []string
	journalFail  string

	mu          sync.Mutex
	journalCmds []string
}

func (f *fakeRunner) Run(ctx context.Context, command string, stdout, stderr io.Writer) error {
	switch {
	case command == "hostname":
		_, err := io.WriteString(stdout, f.hostname+"\n")
		return err
	case strings.HasPrefix(command, "systemctl show"):
		_, err := io.WriteString(stdout, f.status)
		return err
	}
	return fmt.Errorf("unexpected command %q", command)
}

func (f *fakeRunner) RunWithStdin(ctx context.Context, command string, stdin io.Reader, stdout, stderr io.Writer) error {
	if !strings.Contains(command, "journalctl") {
		return fmt.Errorf("unexpected stdin command %q", command)
	}
	f.mu.Lock()
	f.journalCmds = append(f.journalCmds, command)
	f.mu.Unlock()
	if f.journalFail != "" {
		_, _ = io.WriteString(stderr, f.journalFail)
		return errors.New("exit status 1")
	}
	for _, line := range f.journalLines {
		if _, err := io.WriteString(stdout, line+"\n"); err != nil {
			return err
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

// recordingSink merkt sich alles, was Stream meldet.
type recordingSink struct {
	mu       sync.Mutex
	hello    string
	lines    []hostapi.UnitLogLine
	statuses []hostapi.UnitStatus
}

func (s *recordingSink) Hello(hostname string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hello = hostname
}

func (s *recordingSink) Line(line hostapi.UnitLogLine) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, line)
}

func (s *recordingSink) Status(status hostapi.UnitStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses = append(s.statuses, status)
}

func (s *recordingSink) snapshot() recordingSink {
	s.mu.Lock()
	defer s.mu.Unlock()
	return recordingSink{
		hello:    s.hello,
		lines:    append([]hostapi.UnitLogLine(nil), s.lines...),
		statuses: append([]hostapi.UnitStatus(nil), s.statuses...),
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func startStream(ctx context.Context, r Runner, redact *hostapi.Redactor, sink hostapi.UnitLogSink, poll time.Duration) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- Stream(ctx, r, hostapi.UnitLogsRequest{Units: testUnits, Backlog: 200}, redact, sink, poll)
	}()
	return done
}

func TestStreamSendsHelloStatusAndRedactedLines(t *testing.T) {
	runner := &fakeRunner{
		hostname: "energy-node",
		status:   activeStatus,
		journalLines: []string{
			`{"__CURSOR":"s=1;i=1","__REALTIME_TIMESTAMP":"1730451662123456","PRIORITY":"6","_SYSTEMD_UNIT":"energy-node-dashboard.service","MESSAGE":"login hunter2-secret"}`,
		},
	}
	sink := &recordingSink{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startStream(ctx, runner, hostapi.NewRedactor("hunter2-secret"), sink, time.Hour)

	waitFor(t, "one line and both statuses", func() bool {
		state := sink.snapshot()
		return len(state.lines) == 1 && len(state.statuses) == 2
	})
	cancel()
	<-done

	state := sink.snapshot()
	if state.hello != "energy-node" {
		t.Errorf("hello = %q, want energy-node", state.hello)
	}
	line := state.lines[0]
	if line.Message != "login ***" {
		t.Errorf("message = %q, want %q", line.Message, "login ***")
	}
	if line.Unit != "energy-node-dashboard.service" || line.At != 1730451662123 || line.Cursor != "s=1;i=1" {
		t.Errorf("line = %+v", line)
	}
	seen := map[string]int{}
	for _, st := range state.statuses {
		seen[st.Unit]++
		if st.PID != 812 || st.Active != "active" {
			t.Errorf("status = %+v", st)
		}
	}
	for _, unit := range testUnits {
		if seen[unit] != 1 {
			t.Errorf("statuses for %s = %d, want 1", unit, seen[unit])
		}
	}
	if got := runner.journalCmds; len(got) != 1 || !strings.HasPrefix(got[0], "sh -c ") {
		t.Errorf("journal command = %q, want one sh -c wrapped command", got)
	}
}

func TestStreamSendsStatusOnlyOnChange(t *testing.T) {
	runner := &fakeRunner{hostname: "energy-node", status: activeStatus}
	sink := &recordingSink{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startStream(ctx, runner, nil, sink, 10*time.Millisecond)

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	seen := map[string]int{}
	for _, st := range sink.snapshot().statuses {
		seen[st.Unit]++
	}
	for _, unit := range testUnits {
		if seen[unit] != 1 {
			t.Errorf("statuses for %s = %d, want exactly 1", unit, seen[unit])
		}
	}
}

func TestStreamReportsEarlyJournalExit(t *testing.T) {
	const detail = "No journal files were opened due to insufficient permissions."
	runner := &fakeRunner{
		hostname:    "energy-node",
		status:      activeStatus,
		journalFail: detail + "\n",
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	select {
	case err := <-startStream(ctx, runner, nil, &recordingSink{}, time.Hour):
		var apiErr *hostapi.Error
		if !errors.As(err, &apiErr) || apiErr.Code != "UNIT_LOGS_FAILED" || apiErr.Detail != detail {
			t.Fatalf("err = %#v, want UNIT_LOGS_FAILED with detail %q", err, detail)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stream did not return after journalctl exited")
	}
}

func TestStreamReturnsOnCancel(t *testing.T) {
	runner := &fakeRunner{hostname: "energy-node", status: activeStatus}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startStream(ctx, runner, nil, &recordingSink{}, time.Hour)

	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stream did not return within 1s of cancel")
	}
}
