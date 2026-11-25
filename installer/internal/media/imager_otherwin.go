//go:build !windows

package media

// LocateWindowsImager reports no imager outside Windows.
func LocateWindowsImager() (Imager, bool) {
	return Imager{}, false
}
