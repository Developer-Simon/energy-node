//go:build windows

package shell

import (
	"errors"

	webview2 "github.com/jchv/go-webview2"
)

func init() { newWebview = newEdgeWebview }

// newEdgeWebview backs Stufe 1 on Windows via go-webview2, a pure-Go
// binding onto the Edge WebView2 runtime (E6). Unlike glaze, New does not
// return an error -- it returns a nil WebView when the runtime cannot be
// found at all (the common "not installed" case), which is the signal
// this function turns into an error for Open.
//
// Honest limit (spec E6, "was ehrlich dagegen steht"): a rarer failure --
// the runtime starts creating an environment or controller and that
// asynchronous step later reports an error -- calls log.Fatal deep inside
// go-webview2 instead of returning one. That path is not recoverable here;
// it is the one way Stufe 1 can still cost more than convenience on
// Windows, and no Go-level fallback can intercept a log.Fatal.
func newEdgeWebview(url string) (embeddedWindow, error) {
	wv := webview2.New(false)
	if wv == nil {
		return nil, errors.New("shell: the WebView2 runtime is not available")
	}
	wv.SetTitle(windowTitle)
	wv.SetSize(windowWidth, windowHeight, webview2.HintNone)
	wv.Navigate(url)
	return wv, nil
}
