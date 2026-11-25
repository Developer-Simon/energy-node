package versions_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/versions"
)

const testOSRelease = "PRETTY_NAME=\"Raspbian GNU/Linux 12 (bookworm)\"\nNAME=\"Raspbian GNU/Linux\"\nVERSION_ID=\"12\"\nVERSION_CODENAME=bookworm\n"

const testManifest = `{"version":"v0.11.20","packages":{"caddy":{"version":"2.11.9"},"tailscale":{"version":"1.62.0"}}}`

func stanza(name, status, version string) string {
	return "Package: " + name + "\nStatus: " + status + "\nVersion: " + version + "\nDescription: x\n more text\n Package: not-a-field\n\n"
}

func systemSources(t *testing.T, dpkg, osRelease, manifest string) versions.SystemSources {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if body != "" {
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return path
	}
	write("installed-manifest.json", manifest)
	return versions.SystemSources{DpkgStatus: write("status", dpkg), OSRelease: write("os-release", osRelease), StateDir: dir}
}

func byID(entries []versions.SystemEntry) map[string]versions.SystemEntry {
	out := map[string]versions.SystemEntry{}
	for _, entry := range entries {
		out[entry.ID] = entry
	}
	return out
}

func TestSystemListsOSAndPackagesAndMarksAnOutdatedPin(t *testing.T) {
	dpkg := stanza("caddy", "hold ok installed", "2.6.2-5+deb12u1") +
		stanza("tailscale", "hold ok installed", "1.62.0") +
		stanza("mosquitto", "install ok installed", "1:2.0.11-1.2+deb12u1") +
		stanza("python3", "install ok installed", "3.11.2-1+b1")
	got := versions.System(systemSources(t, dpkg, testOSRelease, testManifest))
	want := []versions.SystemEntry{
		{ID: "os", Label: "Raspbian GNU/Linux", Version: "12 (bookworm)", Installed: true},
		{ID: "caddy", Label: "Caddy", Version: "2.6.2", Installed: true, Pinned: "2.11.9", Outdated: true},
		{ID: "tailscale", Label: "Tailscale", Version: "1.62.0", Installed: true, Pinned: "1.62.0"},
		{ID: "mosquitto", Label: "Mosquitto", Version: "2.0.11", Installed: true},
		{ID: "python3", Label: "Python", Version: "3.11.2", Installed: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestSystemDoesNotMarkNewerEqualOrUncomparableVersions(t *testing.T) {
	cases := map[string]string{"newer": "2.12.0-1", "equal with revision": "2.11.9-2", "no number": "unstable"}
	for name, version := range cases {
		got := byID(versions.System(systemSources(t, stanza("caddy", "install ok installed", version), "", testManifest)))
		if got["caddy"].Outdated {
			t.Errorf("%s: %q marked outdated", name, version)
		}
	}
}

func TestSystemIgnoresPackageLeftovers(t *testing.T) {
	dpkg := stanza("caddy", "deinstall ok config-files", "2.6.2-5") + stanza("tailscale", "install ok half-installed", "1.0.0")
	got := byID(versions.System(systemSources(t, dpkg, "", testManifest)))
	for _, id := range []string{"caddy", "tailscale"} {
		entry := got[id]
		if entry.Installed || entry.Version != "" || entry.Outdated || entry.Pinned == "" {
			t.Errorf("%s = %+v, want not installed, no version, no mark, pin kept", id, entry)
		}
	}
}

func TestSystemTakesTheInstalledStanzaOfAMultiarchPackage(t *testing.T) {
	dpkg := stanza("python3", "deinstall ok config-files", "3.9.0-1") + stanza("python3", "install ok installed", "3.11.2-1")
	got := byID(versions.System(systemSources(t, dpkg, "", "")))
	if !got["python3"].Installed || got["python3"].Version != "3.11.2" {
		t.Fatalf("python3 = %+v", got["python3"])
	}
}

func TestSystemSurvivesAVeryLongLine(t *testing.T) {
	dpkg := "Package: big\nProvides: " + strings.Repeat("x", 200_000) + "\n\n" + stanza("caddy", "install ok installed", "2.11.9")
	got := byID(versions.System(systemSources(t, dpkg, "", "")))
	if got["caddy"].Version != "2.11.9" {
		t.Fatalf("caddy = %+v", got["caddy"])
	}
}

func TestSystemKeepsRowsWithoutAUsableManifest(t *testing.T) {
	for name, manifest := range map[string]string{"missing": "", "old": `{"version":"v0.9.0"}`, "broken": `{`} {
		got := byID(versions.System(systemSources(t, stanza("caddy", "install ok installed", "2.6.2"), "", manifest)))
		caddy := got["caddy"]
		if !caddy.Installed || caddy.Pinned != "" || caddy.Outdated {
			t.Errorf("%s: caddy = %+v", name, caddy)
		}
	}
}

func TestSystemShowsOnlyWhatIsReadable(t *testing.T) {
	onlyOS := versions.System(systemSources(t, "", testOSRelease, testManifest))
	if len(onlyOS) != 1 || onlyOS[0].ID != "os" {
		t.Fatalf("without dpkg: %+v", onlyOS)
	}
	if got := versions.System(systemSources(t, "", "", testManifest)); len(got) != 0 {
		t.Fatalf("nothing readable: %+v", got)
	}
	rolling := versions.System(systemSources(t, "", "NAME=Debian\nVERSION_CODENAME=forky\n", ""))
	if len(rolling) != 1 || rolling[0].Version != "forky" {
		t.Fatalf("without VERSION_ID: %+v", rolling)
	}
}
