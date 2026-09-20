// shell.go is the fallback-chain orchestrator for Schicht 4 (E6): it tries
// an embedded system WebView first, then the existing Candidates() list
// (--app= windows), then the default browser, and finally falls back to
// printing nothing more than the URL -- Open never fails, because every
// stage down to "just the address" is a defined, non-error outcome (see
// Komponente F in the spec).
package shell

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// windowTitle and the default window size are shared by every embedded
// WebView backend (webview_darwin.go, webview_linux.go, webview_windows.go).
// The spec scopes translation to the web content (E9); the native window
// chrome stays a fixed, untranslated string.
const (
	windowTitle  = "Energy Node Installer"
	windowWidth  = 1024
	windowHeight = 720
)

// ErrUnsupported is returned by newWebview on a platform with no Stufe-1
// backend (webview_other.go) -- Open treats it exactly like any other
// construction failure and falls through to Stufe 2.
var ErrUnsupported = errors.New("shell: no embedded webview backend for this platform")

// Mode reports which fallback stage answered a call to Open.
type Mode int

const (
	// ModeWebview: an embedded system WebView opened. Open blocked until it
	// closed; the caller's process should exit once Open returns this mode.
	ModeWebview Mode = iota
	// ModeApp: one of Candidates() opened a frameless --app= window.
	ModeApp
	// ModeBrowser: the OS default browser opened a normal tab.
	ModeBrowser
	// ModeURLOnly: nothing could be opened; the URL was only printed.
	ModeURLOnly
)

func (m Mode) String() string {
	switch m {
	case ModeWebview:
		return "webview"
	case ModeApp:
		return "app"
	case ModeBrowser:
		return "browser"
	case ModeURLOnly:
		return "url-only"
	default:
		return fmt.Sprintf("shell.Mode(%d)", int(m))
	}
}

// embeddedWindow is the minimal surface Stufe 1 needs from any platform's
// WebView library. Deliberately tiny (E6): the window is fully configured
// (title, size, URL) by the time newWebview returns it, so nothing but
// running the event loop and tearing it down is left to do.
type embeddedWindow interface {
	Run()
	Destroy()
}

// newWebview is set by exactly one of webview_darwin.go, webview_linux.go,
// webview_windows.go or webview_other.go, chosen at compile time by Go
// build tags -- only one of those files is ever part of a given build.
// Tests in shell_internal_test.go reassign it to exercise Open's fallback
// logic without a real display.
var newWebview func(url string) (embeddedWindow, error)

// Open shows url in a window and reports which fallback stage answered
// (see Mode). For ModeWebview it does not return until the window is
// closed -- the caller must have started its HTTP server and be ready to
// shut down the moment Open returns that mode (E6's process-lifetime
// rule). The other three modes return immediately, exactly as the
// previous Stufe-2-to-4-only Open did. error is reserved for a future
// caller-facing failure; every reachable branch today returns nil, since
// Stufe 4 (ModeURLOnly) is itself a defined, non-error outcome.
func Open(url string) (Mode, error) {
	if newWebview != nil {
		win, err := newWebview(url)
		if err == nil {
			win.Run()
			win.Destroy()
			return ModeWebview, nil
		}
		fmt.Fprintf(os.Stderr, "shell: no embedded window (%v); falling back\n", err)
	}

	for _, candidate := range Candidates(runtime.GOOS) {
		path, err := exec.LookPath(candidate.Name)
		if err != nil {
			continue
		}
		cmd := exec.Command(path, candidate.Args(url)...)
		if err := cmd.Start(); err != nil {
			continue
		}
		go func() { _ = cmd.Wait() }()
		return ModeApp, nil
	}

	if err := openInBrowser(url); err == nil {
		return ModeBrowser, nil
	}
	return ModeURLOnly, nil
}
