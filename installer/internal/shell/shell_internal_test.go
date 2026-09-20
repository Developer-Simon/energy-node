// installer/internal/shell/shell_internal_test.go
package shell

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type fakeWindow struct{ ran, destroyed bool }

func (w *fakeWindow) Run()     { w.ran = true }
func (w *fakeWindow) Destroy() { w.destroyed = true }

// newFakeExecutable writes an executable shell script named name into dir
// that exits 0 immediately -- a stand-in for a real browser/app binary so
// Open's Candidates()/openInBrowser exec.LookPath calls can succeed without
// a real GUI program installed.
func newFakeExecutable(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing fake executable %s: %v", name, err)
	}
}

func TestOpenPrefersAWorkingEmbeddedWebview(t *testing.T) {
	prev := newWebview
	win := &fakeWindow{}
	newWebview = func(url string) (embeddedWindow, error) { return win, nil }
	t.Cleanup(func() { newWebview = prev })

	mode, err := Open("http://127.0.0.1:1/")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if mode != ModeWebview {
		t.Errorf("mode = %v, want ModeWebview", mode)
	}
	if !win.ran || !win.destroyed {
		t.Errorf("fakeWindow = %+v, want Run and Destroy both called", win)
	}
}

func TestOpenFallsBackToStage2WhenTheConstructorFails(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("this fake targets linux's Candidates() list")
	}
	prev := newWebview
	newWebview = func(url string) (embeddedWindow, error) {
		return nil, errors.New("no display")
	}
	t.Cleanup(func() { newWebview = prev })

	dir := t.TempDir()
	newFakeExecutable(t, dir, "google-chrome")
	t.Setenv("PATH", dir)

	mode, err := Open("http://127.0.0.1:1/")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if mode != ModeApp {
		t.Errorf("mode = %v, want ModeApp", mode)
	}
}

func TestOpenFallsBackToStage3WhenNoCandidateIsOnPath(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("this fake targets linux's openInBrowser command (xdg-open)")
	}
	prev := newWebview
	newWebview = func(url string) (embeddedWindow, error) {
		return nil, errors.New("no display")
	}
	t.Cleanup(func() { newWebview = prev })

	dir := t.TempDir()
	newFakeExecutable(t, dir, "xdg-open")
	t.Setenv("PATH", dir)

	mode, err := Open("http://127.0.0.1:1/")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if mode != ModeBrowser {
		t.Errorf("mode = %v, want ModeBrowser", mode)
	}
}

func TestOpenFallsBackToStage4WhenNothingIsOnPath(t *testing.T) {
	prev := newWebview
	newWebview = func(url string) (embeddedWindow, error) {
		return nil, errors.New("no display")
	}
	t.Cleanup(func() { newWebview = prev })

	t.Setenv("PATH", t.TempDir())

	mode, err := Open("http://127.0.0.1:1/")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if mode != ModeURLOnly {
		t.Errorf("mode = %v, want ModeURLOnly", mode)
	}
}
