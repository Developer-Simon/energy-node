package main

import (
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/shell"
)

func TestNewUIConfigDefaults(t *testing.T) {
	cfg, err := newUIConfig(nil)
	if err != nil {
		t.Fatalf("newUIConfig: %v", err)
	}
	if cfg.addr != "127.0.0.1:0" {
		t.Errorf("addr = %q, want an OS-assigned port on the loopback interface", cfg.addr)
	}
	if cfg.openWindow != true {
		t.Errorf("the window opens by default")
	}
}

func TestNewUIConfigFlags(t *testing.T) {
	cfg, err := newUIConfig([]string{"--bundle", "/tmp/b", "--no-window", "--port", "8123", "--lang", "en"})
	if err != nil {
		t.Fatalf("newUIConfig: %v", err)
	}
	if cfg.bundleDir != "/tmp/b" || cfg.openWindow || cfg.language != "en" {
		t.Errorf("cfg = %+v", cfg)
	}
	if cfg.addr != "127.0.0.1:8123" {
		t.Errorf("addr = %q, want the requested port on the loopback interface", cfg.addr)
	}
}

func TestTheServerNeverBindsBeyondLoopback(t *testing.T) {
	// Es gibt bewusst keinen --host-Schalter. Ein Installer, der auf 0.0.0.0
	// lauscht, gaebe jedem im Netz eine Oberflaeche, die Root-Rechte auf einem
	// fremden Node ausuebt.
	cfg, err := newUIConfig([]string{"--port", "9000"})
	if err != nil {
		t.Fatalf("newUIConfig: %v", err)
	}
	if !strings.HasPrefix(cfg.addr, "127.0.0.1:") {
		t.Fatalf("addr = %q, want 127.0.0.1", cfg.addr)
	}
}

func TestNewTokenIsLongAndDifferentEveryTime(t *testing.T) {
	first, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	second, _ := newToken()
	if len(first) < 32 {
		t.Errorf("token length = %d, want at least 32 characters", len(first))
	}
	if first == second {
		t.Errorf("two tokens were identical")
	}
}

func TestDescribeShellModeStopsWaitingOnlyAfterAWebviewClosed(t *testing.T) {
	cases := []struct {
		mode          shell.Mode
		wantWait      bool
		wantNoteEmpty bool
	}{
		{shell.ModeWebview, false, true},
		{shell.ModeApp, true, true},
		{shell.ModeBrowser, true, false},
		{shell.ModeURLOnly, true, false},
	}
	for _, c := range cases {
		note, wait := describeShellMode(c.mode)
		if wait != c.wantWait {
			t.Errorf("mode %v: waitForSignal = %v, want %v", c.mode, wait, c.wantWait)
		}
		if (note == "") != c.wantNoteEmpty {
			t.Errorf("mode %v: note = %q, wantEmpty = %v", c.mode, note, c.wantNoteEmpty)
		}
	}
}
