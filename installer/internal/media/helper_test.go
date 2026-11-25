//go:build !windows

package media

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeImagerScript legt ein Shellskript namens rpi-imager an und liefert seinen Pfad.
func writeImagerScript(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "rpi-imager")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunHelperWritesOutputAndExitCode(t *testing.T) {
	dir := t.TempDir()
	f, err := PrepareHelperFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	script := writeImagerScript(t, dir, `printf '  Writing: [>] 10 %%\r'; exit 3`)

	code := RunHelper(context.Background(), f, script, nil)
	if code != 3 {
		t.Fatalf("exit code = %d, want 3", code)
	}
	log, err := os.ReadFile(f.Log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "  Writing: [>] 10 %") {
		t.Fatalf("log = %q, want the writing line", log)
	}
	got, ok := ReadHelperStatus(f.Status)
	if !ok || got != 3 {
		t.Fatalf("ReadHelperStatus = %d, %v, want 3, true", got, ok)
	}
}

func TestPrepareHelperFilesCreatesLogAndStatusOnly(t *testing.T) {
	dir := t.TempDir()
	f, err := PrepareHelperFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if f.Log != filepath.Join(dir, "imager.log") || f.Status != filepath.Join(dir, "status") || f.Cancel != filepath.Join(dir, "cancel") {
		t.Fatalf("paths = %+v", f)
	}
	for _, p := range []string{f.Log, f.Status} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s not created: %v", p, err)
		}
		if info.Size() != 0 {
			t.Fatalf("%s size = %d, want 0", p, info.Size())
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %v, want 0600", p, info.Mode().Perm())
		}
	}
	if _, err := os.Stat(f.Cancel); !os.IsNotExist(err) {
		t.Fatalf("cancel file must not exist, stat err = %v", err)
	}
	if _, ok := ReadHelperStatus(f.Status); ok {
		t.Fatal("empty status file must read as not ok")
	}
}

func TestRunHelperKillsOnCancelFile(t *testing.T) {
	dir := t.TempDir()
	f, err := PrepareHelperFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	script := writeImagerScript(t, dir, "exec sleep 30")

	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = os.WriteFile(f.Cancel, nil, 0o600)
	}()

	start := time.Now()
	code := RunHelper(context.Background(), f, script, nil)
	if code == 0 {
		t.Fatal("killed imager must not return 0")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("RunHelper took %v after cancel", elapsed)
	}
}

func TestRunHelperDoesNotStartWhenAlreadyCancelled(t *testing.T) {
	dir := t.TempDir()
	f, err := PrepareHelperFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.Cancel, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "started")
	script := writeImagerScript(t, dir, "touch "+marker)

	code := RunHelper(context.Background(), f, script, nil)
	if code == 0 {
		t.Fatal("cancelled run must not return 0")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("imager must not start, stat err = %v", err)
	}
	log, _ := os.ReadFile(f.Log)
	if !strings.Contains(string(log), "Error: cancelled before start") {
		t.Fatalf("log = %q", log)
	}
}

func TestRunHelperRefusesOtherPrograms(t *testing.T) {
	dir := t.TempDir()
	f, err := PrepareHelperFiles(dir)
	if err != nil {
		t.Fatal(err)
	}

	code := RunHelper(context.Background(), f, "/bin/sh", nil)
	if code != 125 {
		t.Fatalf("exit code = %d, want 125", code)
	}
	log, _ := os.ReadFile(f.Log)
	if !strings.HasPrefix(string(log), "Error: refusing to run sh") {
		t.Fatalf("log = %q", log)
	}
}

func TestTailLogFollowsUntilDone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "imager.log")
	done := make(chan struct{})

	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = os.WriteFile(path, []byte("first\n"), 0o600)
		time.Sleep(150 * time.Millisecond)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
		if err == nil {
			_, _ = f.WriteString("second\n")
			_ = f.Close()
		}
		close(done)
	}()

	rc := TailLog(path, done)
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first\nsecond\n" {
		t.Fatalf("tail = %q", data)
	}
}

func TestRunShimRefusesOtherPrograms(t *testing.T) {
	if code := RunShim("/bin/sh", nil); code != 125 {
		t.Fatalf("exit code = %d, want 125", code)
	}
}
