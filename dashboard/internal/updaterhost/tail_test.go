// dashboard/internal/updaterhost/tail_test.go
package updaterhost

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type recordingSink struct {
	markers []string
	logs    []string
}

func (s *recordingSink) Marker(stepID, state, detail string) {
	s.markers = append(s.markers, stepID+" "+state+" "+detail)
}
func (s *recordingSink) Log(stepID, line string) { s.logs = append(s.logs, stepID+": "+line) }

func TestTailJobLogStopsAtOkStatus(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log")
	statusPath := filepath.Join(dir, "status.json")
	os.WriteFile(logPath, []byte("1000 ##STEP 60 begin\n1001 doing the thing\n1002 ##STEP 60 ok\n"), 0o644)
	os.WriteFile(statusPath, []byte(`{"result":"ok"}`), 0o644)

	sink := &recordingSink{}
	err := tailJobLog(context.Background(), logPath, statusPath, 0, sink, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("tailJobLog: %v", err)
	}
	if len(sink.markers) != 2 || sink.markers[0] != "60 begin " {
		t.Fatalf("markers = %v", sink.markers)
	}
	if len(sink.logs) != 1 {
		t.Fatalf("logs = %v", sink.logs)
	}
}

func TestTailJobLogReportsAFailStatusAsAnError(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log")
	statusPath := filepath.Join(dir, "status.json")
	os.WriteFile(logPath, []byte("1000 ##STEP 60 fail DASHBOARD_START_FAILED\n"), 0o644)
	os.WriteFile(statusPath, []byte(`{"result":"fail","step":"60","code":"DASHBOARD_START_FAILED"}`), 0o644)

	err := tailJobLog(context.Background(), logPath, statusPath, 0, &recordingSink{}, 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected an error for a failed job")
	}
}

func TestTailJobLogWaitsForNewLinesBeforeTheStatusAppears(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log")
	statusPath := filepath.Join(dir, "status.json")
	os.WriteFile(logPath, []byte("1000 ##STEP 50 begin\n"), 0o644)

	done := make(chan error, 1)
	sink := &recordingSink{}
	go func() { done <- tailJobLog(context.Background(), logPath, statusPath, 0, sink, 5*time.Millisecond) }()

	time.Sleep(20 * time.Millisecond)
	f, _ := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("1001 ##STEP 50 ok\n")
	f.Close()
	os.WriteFile(statusPath, []byte(`{"result":"ok"}`), 0o644)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("tailJobLog: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tailJobLog did not notice the appended line and status")
	}
	if len(sink.markers) != 2 {
		t.Fatalf("markers = %v, want begin+ok", sink.markers)
	}
}
