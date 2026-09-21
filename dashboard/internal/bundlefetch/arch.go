package bundlefetch

// ArchForGo maps runtime.GOARCH to make_bundle.sh's --arch. The dashboard
// binary inside a bundle is cross-compiled for exactly the bundle's arch
// (GOARCH=arm GOARM=6 for armv6), so the running binary's GOARCH identifies
// the bundle this node needs without asking uname.
func ArchForGo(goarch string) (string, bool) {
	switch goarch {
	case "arm":
		return "armv6", true
	case "arm64":
		return "arm64", true
	case "amd64":
		return "amd64", true
	}
	return "", false
}
