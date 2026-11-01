package unitlogs

import (
	"strings"
	"testing"
	"unicode/utf8"
)

var liveUnits = []string{"energy-node-dashboard.service", "energy-node-updater.service"}

func TestParseJournalLineServiceMessage(t *testing.T) {
	raw := []byte(`{"__CURSOR":"s=abc;i=1","__REALTIME_TIMESTAMP":"1730451662123456","PRIORITY":"3","_SYSTEMD_UNIT":"energy-node-dashboard.service","MESSAGE":"mqtt: connection refused"}`)
	line, ok := ParseJournalLine(raw, liveUnits)
	if !ok {
		t.Fatal("line dropped")
	}
	if line.At != 1730451662123 {
		t.Errorf("At = %d, want 1730451662123", line.At)
	}
	if line.Priority != 3 {
		t.Errorf("Priority = %d, want 3", line.Priority)
	}
	if line.Unit != "energy-node-dashboard.service" {
		t.Errorf("Unit = %q", line.Unit)
	}
	if line.Message != "mqtt: connection refused" {
		t.Errorf("Message = %q", line.Message)
	}
	if line.Cursor != "s=abc;i=1" {
		t.Errorf("Cursor = %q", line.Cursor)
	}
}

func TestParseJournalLineSystemdAboutUnit(t *testing.T) {
	raw := []byte(`{"__CURSOR":"c2","__REALTIME_TIMESTAMP":"1730451662000000","PRIORITY":"6","_SYSTEMD_UNIT":"init.scope","UNIT":"energy-node-updater.service","SYSLOG_IDENTIFIER":"systemd","_PID":"1","MESSAGE":"Started."}`)
	line, ok := ParseJournalLine(raw, liveUnits)
	if !ok {
		t.Fatal("line dropped")
	}
	if line.Unit != "energy-node-updater.service" {
		t.Errorf("Unit = %q", line.Unit)
	}
	if line.Ident != "systemd" {
		t.Errorf("Ident = %q", line.Ident)
	}
	if line.PID != "1" {
		t.Errorf("PID = %q", line.PID)
	}
}

func TestParseJournalLineForeignUnitDropped(t *testing.T) {
	raw := []byte(`{"__CURSOR":"c3","__REALTIME_TIMESTAMP":"1","PRIORITY":"6","_SYSTEMD_UNIT":"ssh.service","MESSAGE":"x"}`)
	if _, ok := ParseJournalLine(raw, liveUnits); ok {
		t.Fatal("foreign unit line was kept")
	}
}

func TestParseJournalLineByteArrayMessage(t *testing.T) {
	raw := []byte(`{"__CURSOR":"c4","__REALTIME_TIMESTAMP":"1","_SYSTEMD_UNIT":"energy-node-dashboard.service","MESSAGE":[104,105,255]}`)
	line, ok := ParseJournalLine(raw, liveUnits)
	if !ok {
		t.Fatal("line dropped")
	}
	if line.Message != "hi�" {
		t.Errorf("Message = %q, want %q", line.Message, "hi�")
	}
}

func TestParseJournalLineTruncates(t *testing.T) {
	long := strings.Repeat("ä", 2500)
	raw := []byte(`{"__CURSOR":"c5","__REALTIME_TIMESTAMP":"1","_SYSTEMD_UNIT":"energy-node-dashboard.service","MESSAGE":"` + long + `"}`)
	line, ok := ParseJournalLine(raw, liveUnits)
	if !ok {
		t.Fatal("line dropped")
	}
	if n := utf8.RuneCountInString(line.Message); n != 2001 {
		t.Fatalf("rune count = %d, want 2001 (2000 plus ellipsis)", n)
	}
	if !strings.HasSuffix(line.Message, "…") {
		t.Errorf("Message does not end with ellipsis")
	}
}

func TestParseJournalLineWithoutCursorOrTimeDropped(t *testing.T) {
	noCursor := []byte(`{"__REALTIME_TIMESTAMP":"1","_SYSTEMD_UNIT":"energy-node-dashboard.service","MESSAGE":"x"}`)
	if _, ok := ParseJournalLine(noCursor, liveUnits); ok {
		t.Error("line without cursor was kept")
	}
	noTime := []byte(`{"__CURSOR":"c","_SYSTEMD_UNIT":"energy-node-dashboard.service","MESSAGE":"x"}`)
	if _, ok := ParseJournalLine(noTime, liveUnits); ok {
		t.Error("line without timestamp was kept")
	}
}

func TestParseJournalLineInvalidJSONDropped(t *testing.T) {
	if _, ok := ParseJournalLine([]byte(`{not json`), liveUnits); ok {
		t.Fatal("invalid JSON was kept")
	}
}

func TestParseStatusCrashLoop(t *testing.T) {
	out := "LoadState=loaded\nActiveState=activating\nSubState=auto-restart\nResult=exit-code\nExecMainStatus=1\nNRestarts=9\nMainPID=0\nActiveEnterTimestamp=\n"
	st := ParseStatus("energy-node-dashboard.service", out)
	if st.Unit != "energy-node-dashboard.service" {
		t.Errorf("Unit = %q", st.Unit)
	}
	if st.Load != "loaded" || st.Active != "activating" || st.Sub != "auto-restart" {
		t.Errorf("states = %q/%q/%q", st.Load, st.Active, st.Sub)
	}
	if st.Result != "exit-code" {
		t.Errorf("Result = %q", st.Result)
	}
	if st.ExitStatus != 1 {
		t.Errorf("ExitStatus = %d", st.ExitStatus)
	}
	if st.Restarts != 9 {
		t.Errorf("Restarts = %d", st.Restarts)
	}
	if st.PID != 0 {
		t.Errorf("PID = %d", st.PID)
	}
	if st.Since != 0 {
		t.Errorf("Since = %d, want 0", st.Since)
	}
}

func TestParseStatusRunning(t *testing.T) {
	out := "LoadState=loaded\nActiveState=active\nSubState=running\nResult=success\nExecMainStatus=0\nNRestarts=0\nMainPID=812\nActiveEnterTimestamp=@1730451662\n"
	st := ParseStatus("energy-node-updater.service", out)
	if st.Since != 1730451662000 {
		t.Errorf("Since = %d, want 1730451662000", st.Since)
	}
	if st.PID != 812 {
		t.Errorf("PID = %d, want 812", st.PID)
	}
}

func TestParseStatusNotFound(t *testing.T) {
	out := "LoadState=not-found\nActiveState=inactive\nSubState=dead\nResult=success\nExecMainStatus=0\nNRestarts=0\nMainPID=0\nActiveEnterTimestamp=n/a\n"
	st := ParseStatus("energy-node-updater.service", out)
	if st.Load != "not-found" {
		t.Errorf("Load = %q", st.Load)
	}
	if st.Since != 0 {
		t.Errorf("Since = %d, want 0 for n/a", st.Since)
	}
}

func TestParseStatusMissingFieldsStayZero(t *testing.T) {
	st := ParseStatus("energy-node-dashboard.service", "ActiveState=active\nWeird=1\n")
	if st.Active != "active" || st.Load != "" || st.PID != 0 || st.Restarts != 0 {
		t.Errorf("unexpected status %+v", st)
	}
}
