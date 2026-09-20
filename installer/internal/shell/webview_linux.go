//go:build linux

package shell

import "github.com/crgimenes/glaze"

func init() { newWebview = newGlazeWebview }

// newGlazeWebview backs Stufe 1 on Linux via glaze (E6): a CGo-free binding
// that dlopen's the system GTK4+WebKitGTK-6.0 stack, falling back to
// GTK3+WebKit2GTK-4.x. glaze.New's error is exactly the "no window" signal
// Open needs when neither stack can be loaded -- the state of a typical
// headless CI runner, proven in webview_linux_test.go.
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
