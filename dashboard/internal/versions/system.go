package versions

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const (
	// DefaultDpkgStatus is dpkg's package database, readable by every user.
	DefaultDpkgStatus = "/var/lib/dpkg/status"
	// DefaultOSRelease names the distribution and its release.
	DefaultOSRelease = "/etc/os-release"
	// maxDpkgLine bounds one line of the database, far above real fields.
	maxDpkgLine = 4 << 20
)

// SystemSources names the files System reads. An empty DpkgStatus or
// OSRelease means the default path.
type SystemSources struct {
	DpkgStatus string
	OSRelease  string
	StateDir   string
}

// SystemEntry is one row of the system card on the versions page.
type SystemEntry struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
	// Pinned is the version the installed bundle pins, empty without a pin.
	Pinned   string `json:"pinned,omitempty"`
	Outdated bool   `json:"outdated"`
}

// systemPackages are the Debian packages the node depends on, in page order.
var systemPackages = []struct{ id, label string }{
	{"caddy", "Caddy"},
	{"tailscale", "Tailscale"},
	{"mosquitto", "Mosquitto"},
	{"python3", "Python"},
}

// System reports the operating system and the system packages. What cannot be
// read is left out, so a machine without dpkg shows no package rows.
func System(src SystemSources) []SystemEntry {
	if src.DpkgStatus == "" {
		src.DpkgStatus = DefaultDpkgStatus
	}
	if src.OSRelease == "" {
		src.OSRelease = DefaultOSRelease
	}
	var out []SystemEntry
	if entry, ok := osEntry(src.OSRelease); ok {
		out = append(out, entry)
	}
	installed, ok := dpkgVersions(src.DpkgStatus)
	if !ok {
		return out
	}
	pins := packagePins(src.StateDir)
	for _, pkg := range systemPackages {
		version, present := installed[pkg.id]
		entry := SystemEntry{ID: pkg.id, Label: pkg.label, Version: upstreamVersion(version), Installed: present, Pinned: pins[pkg.id]}
		entry.Outdated = present && isOlder(entry.Version, entry.Pinned)
		out = append(out, entry)
	}
	return out
}

// isOlder is true only when both versions start with a number and a is lower.
func isOlder(a, b string) bool {
	return versionNumbers.MatchString(a) && versionNumbers.MatchString(b) && compareVersions(a, b) < 0
}

// upstreamVersion drops the epoch and the Debian revision of a dpkg version.
func upstreamVersion(version string) string {
	if _, rest, found := strings.Cut(version, ":"); found {
		version = rest
	}
	if i := strings.LastIndex(version, "-"); i >= 0 {
		version = version[:i]
	}
	return version
}

func osEntry(path string) (SystemEntry, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return SystemEntry{}, false
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if key, value, found := strings.Cut(strings.TrimSpace(line), "="); found {
			fields[key] = strings.Trim(value, `"'`)
		}
	}
	label := fields["NAME"]
	if label == "" {
		label = "Linux"
	}
	version, codename := fields["VERSION_ID"], fields["VERSION_CODENAME"]
	switch {
	case version == "":
		version = codename
	case codename != "":
		version += " (" + codename + ")"
	}
	return SystemEntry{ID: "os", Label: label, Version: version, Installed: true}, true
}

// dpkgVersions returns the version of every wanted package whose dpkg state
// ends in "installed" ("install ok installed", "hold ok installed"). ok is
// false when the database cannot be read.
func dpkgVersions(path string) (map[string]string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()

	wanted := map[string]bool{}
	for _, pkg := range systemPackages {
		wanted[pkg.id] = true
	}
	out := map[string]string{}
	var name, status, version string
	flush := func() {
		state := strings.Fields(status)
		if _, seen := out[name]; !seen && wanted[name] && len(state) > 0 && state[len(state)-1] == "installed" {
			out[name] = version
		}
		name, status, version = "", "", ""
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), maxDpkgLine)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		// Continuation lines start with a space and match no field name.
		switch key, value, _ := strings.Cut(line, ": "); key {
		case "Package":
			name = value
		case "Status":
			status = value
		case "Version":
			version = value
		}
	}
	if scanner.Err() != nil {
		return nil, false
	}
	flush()
	return out, true
}

// packagePins reads the package versions the installed bundle pins. A missing,
// old or broken manifest means no pins.
func packagePins(stateDir string) map[string]string {
	out := map[string]string{}
	raw, err := os.ReadFile(filepath.Join(stateDir, "installed-manifest.json"))
	if err != nil {
		return out
	}
	var manifest struct {
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
	}
	if json.Unmarshal(raw, &manifest) != nil {
		return out
	}
	for name, pin := range manifest.Packages {
		out[name] = pin.Version
	}
	return out
}
