package unitlogs

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

func TestJournalCommandBacklog(t *testing.T) {
	cmd := JournalCommand(hostapi.UnitLogsRequest{
		Units:   []string{"energy-node-dashboard.service", "energy-node-updater.service"},
		Backlog: 200,
	})
	if !strings.Contains(cmd, "--lines=200") {
		t.Errorf("missing --lines=200:\n%s", cmd)
	}
	if !strings.Contains(cmd, "-u 'energy-node-dashboard.service'") {
		t.Errorf("dashboard unit not quoted:\n%s", cmd)
	}
	if !strings.Contains(cmd, "-u 'energy-node-updater.service'") {
		t.Errorf("updater unit not quoted:\n%s", cmd)
	}
	if strings.Contains(cmd, "--after-cursor") {
		t.Errorf("unexpected --after-cursor:\n%s", cmd)
	}
}

func TestJournalCommandAfterCursor(t *testing.T) {
	cmd := JournalCommand(hostapi.UnitLogsRequest{
		Units:       []string{"energy-node-dashboard.service"},
		AfterCursor: "s=abc;i=1",
	})
	if !strings.Contains(cmd, "--after-cursor='s=abc;i=1'") {
		t.Errorf("missing quoted cursor:\n%s", cmd)
	}
	if strings.Contains(cmd, "--lines") {
		t.Errorf("unexpected --lines with cursor:\n%s", cmd)
	}
}

func TestStatusCommandQuotesUnit(t *testing.T) {
	want := "systemctl show --timestamp=unix --property=LoadState,ActiveState,SubState,Result,ExecMainStatus,NRestarts,MainPID,ActiveEnterTimestamp -- 'energy-node-updater.service'"
	if got := StatusCommand("energy-node-updater.service"); got != want {
		t.Errorf("StatusCommand =\n%s\nwant\n%s", got, want)
	}
}

func TestStreamCommandKillsJournalWhenStdinCloses(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	dir := t.TempDir()
	writeStub(t, filepath.Join(dir, "journalctl"),
		"#!/bin/sh\necho '{\"__CURSOR\":\"c1\",\"__REALTIME_TIMESTAMP\":\"1\",\"_SYSTEMD_UNIT\":\"energy-node-dashboard.service\",\"MESSAGE\":\"x\"}'\nexec sleep 30\n")
	writeStub(t, filepath.Join(dir, "sudo"), "#!/bin/sh\nexit 1\n")

	cmd := exec.Command("sh", "-c", JournalCommand(hostapi.UnitLogsRequest{
		Units:   []string{"energy-node-dashboard.service", "energy-node-updater.service"},
		Backlog: 200,
	}))
	cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		t.Fatalf("no first line from stub journalctl: %v", scanner.Err())
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, _ = io.Copy(io.Discard, stdout)
		done <- cmd.Wait()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("remote script did not end within 2s after stdin closed")
	}
}

func writeStub(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}
