package bundlesource

import "strings"

// ArchForMachine maps the node's `uname -m` to make_bundle.sh's --arch.
// armv7l maps to armv6 because the armv6 bundle lists both in its
// manifest's uname_machine.
func ArchForMachine(machine string) (string, bool) {
	switch strings.TrimSpace(machine) {
	case "armv6l", "armv7l":
		return "armv6", true
	case "aarch64", "arm64":
		return "arm64", true
	case "x86_64", "amd64":
		return "amd64", true
	}
	return "", false
}
