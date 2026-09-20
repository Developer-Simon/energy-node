//go:build darwin

package shell

import "github.com/crgimenes/glaze"

func init() { newWebview = newGlazeWebview }

// newGlazeWebview backs Stufe 1 on macOS via glaze (E6): a CGo-free binding
// onto Cocoa/WKWebView. Cocoa and WebKit ship with every macOS install, so
// glaze.New realistically never errors here -- the error return exists for
// the interface's sake and because Linux (webview_linux.go), which shares
// this function's shape, needs it for real.
func newGlazeWebview(url string) (embeddedWindow, error) {
	wv, err := glaze.New(false)
	if err != nil {
		return nil, err
	}
	wv.SetTitle(windowTitle)
	wv.SetSize(windowWidth, windowHeight, glaze.HintNone)
	wv.Navigate(url)
	return wv, nil
}
