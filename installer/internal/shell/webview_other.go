//go:build !darwin && !linux && !windows

package shell

func init() { newWebview = newUnsupportedWebview }

// newUnsupportedWebview backs Stufe 1 on any platform with no WebView
// binding wired up. Returning ErrUnsupported here (rather than leaving
// newWebview nil) makes the "no Stufe 1 here" case an explicit, named
// outcome instead of an implicit one -- Open handles it identically to any
// other construction failure either way.
func newUnsupportedWebview(url string) (embeddedWindow, error) {
	return nil, ErrUnsupported
}
