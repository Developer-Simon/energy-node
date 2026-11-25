//go:build !windows

package host

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/media"
)

// inProcessLauncher ersetzt den erhoehten Start: der Helfer laeuft als Goroutine im Testprozess.
func inProcessLauncher(t *testing.T) launcher {
	return func(exe string, args []string) (func() (int, error), error) {
		f, name, rest := splitHelperArgs(t, args)
		done := make(chan int, 1)
		go func() { done <- media.RunHelper(context.Background(), f, name, rest) }()
		return func() (int, error) { return <-done, nil }, nil
	}
}

// splitHelperArgs zerlegt die Argumente aus helperArgs wieder.
func splitHelperArgs(t *testing.T, args []string) (media.HelperFiles, string, []string) {
	t.Helper()
	if len(args) < 9 || args[0] != media.HelperName || args[7] != "--" {
		t.Fatalf("helper args: %q", args)
	}
	f := media.HelperFiles{Log: args[2], Cancel: args[4], Status: args[6]}
	return f, args[8], args[9:]
}

func TestRunElevatedTailsTheLogAndReturnsTheExitCode(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(t.TempDir(), "rpi-imager")
	body := "#!/bin/sh\necho line one\necho line two\nexit 3\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	out, wait, err := runElevated(context.Background(), inProcessLauncher(t), "installer", dir, script, nil)
	if err != nil {
		t.Fatalf("runElevated: %v", err)
	}
	defer out.Close()

	logged, err := io.ReadAll(out)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(logged) != "line one\nline two\n" {
		t.Errorf("log = %q", logged)
	}
	code, err := wait()
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
}
