//go:build linux

package shell

import "testing"

// TestNewGlazeWebviewFailsGracefullyWithoutWebKitGTK proves Stufe 1 returns
// a plain error instead of panicking/crashing when neither WebKitGTK stack
// is present -- the state ci.yml's installer-go job asserts before running
// this test (see the "Assert WebKitGTK is absent" step).
func TestNewGlazeWebviewFailsGracefullyWithoutWebKitGTK(t *testing.T) {
	win, err := newGlazeWebview("http://127.0.0.1:1/")
	if err == nil {
		win.Destroy()
		t.Skip("WebKitGTK is present on this runner; the no-library path is not exercised here")
	}
	if win != nil {
		t.Errorf("win = %v, want nil alongside a non-nil error", win)
	}
}
