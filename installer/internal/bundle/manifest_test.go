package bundle_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
)

func writeManifest(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing manifest.json: %v", err)
	}
}

func TestLoadManifestReadsAllFields(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{
	  "version": "v0.2.0",
	  "arch": "armv6",
	  "uname_machine": ["armv6l", "armv7l"],
	  "python_minor": "3.11",
	  "python_abi": "cp311",
	  "components": {"dashboard": "v0.6.1"},
	  "wheels": [{"file": "paho_mqtt-2.1.0-py3-none-any.whl", "url": "https://www.piwheels.org/simple/paho-mqtt/x.whl", "sha256": "def"}],
	  "caddy": {"version": "2.8.4", "file": "caddy-2.8.4-armv6.tar.gz", "sha256": "abc"},
	  "steps": [
	    {"id": "10", "optional": false},
	    {"id": "81", "optional": true, "default": true, "service_id": "apsystems", "dir": "apsystems_ez1", "unit": "apsystems-ez1.service"}
	  ],
	  "files": {"bootstrap/10-apt.sh": "deadbeef"}
	}`)

	m, err := bundle.LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Version != "v0.2.0" || m.Arch != "armv6" || m.PythonABI != "cp311" {
		t.Fatalf("basic fields not read: %+v", m)
	}
	if len(m.UnameMachine) != 2 || m.UnameMachine[0] != "armv6l" {
		t.Fatalf("uname_machine not read: %+v", m.UnameMachine)
	}
	if m.Caddy == nil || m.Caddy.Version != "2.8.4" {
		t.Fatalf("caddy info not read: %+v", m.Caddy)
	}
	entry, ok := m.StepByID("81")
	if !ok || entry.ServiceID != "apsystems" || entry.Unit != "apsystems-ez1.service" || !entry.Default {
		t.Fatalf("StepByID(81) = %+v, %v", entry, ok)
	}
	if _, ok := m.StepByID("99"); ok {
		t.Fatalf("StepByID(99) should not be found")
	}
}

func TestManifestParsesWheels(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{
	  "version": "v0.5.1",
	  "wheels": [
	    {"file": "paho_mqtt-2.1.0-py3-none-any.whl", "url": "https://www.piwheels.org/simple/paho-mqtt/a.whl", "sha256": "aaa"},
	    {"file": "requests-2.32.0-py3-none-any.whl", "url": "https://pypi.org/simple/requests/b.whl", "sha256": "bbb"}
	  ],
	  "steps": [],
	  "files": {}
	}`)

	m, err := bundle.LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	want := []bundle.WheelPin{
		{File: "paho_mqtt-2.1.0-py3-none-any.whl", URL: "https://www.piwheels.org/simple/paho-mqtt/a.whl", SHA256: "aaa"},
		{File: "requests-2.32.0-py3-none-any.whl", URL: "https://pypi.org/simple/requests/b.whl", SHA256: "bbb"},
	}
	if len(m.Wheels) != len(want) {
		t.Fatalf("wheels = %+v, want %+v", m.Wheels, want)
	}
	for i := range want {
		if m.Wheels[i] != want[i] {
			t.Fatalf("wheels[%d] = %+v, want %+v", i, m.Wheels[i], want[i])
		}
	}
}

func TestLoadManifestMissingFile(t *testing.T) {
	_, err := bundle.LoadManifest(t.TempDir())
	var bundleErr *bundle.Error
	if !errors.As(err, &bundleErr) || bundleErr.Code != bundle.FaultManifestMissing {
		t.Fatalf("expected FaultManifestMissing, got %v", err)
	}
}

func TestLoadManifestInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "not json")
	if _, err := bundle.LoadManifest(dir); err == nil {
		t.Fatalf("expected an error for invalid JSON")
	}
}

func TestStepEntryRoundTripsDashboardKey(t *testing.T) {
	entry := bundle.StepEntry{
		ID:           "81",
		Optional:     true,
		Default:      true,
		ServiceID:    "apsystems",
		Kind:         "device",
		DashboardKey: "apsystems",
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got bundle.StepEntry
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.DashboardKey != "apsystems" {
		t.Fatalf("DashboardKey = %q, erwartet %q", got.DashboardKey, "apsystems")
	}
}

func TestManifestParsesPackages(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{
	  "version": "v0.2.0",
	  "arch": "arm64",
	  "packages": {
	    "tailscale": {"version": "1.104.5", "file": "tailscale_1.104.5_arm64.deb", "url": "https://pkgs.tailscale.com/stable/debian/pool/tailscale_1.104.5_arm64.deb", "sha256": "7ce61e75d15c1269d0aebe3ed59524b83225121cba1b8ea682d04594195f6bc1", "update": "apt"},
	    "caddy": {"version": "2.11.9", "file": "caddy_2.11.9_linux_arm64.deb", "url": "https://github.com/caddyserver/caddy/releases/download/v2.11.9/caddy_2.11.9_linux_arm64.deb", "sha256": "a226cafaddf2db7f392d232cecccc8b3bfaf8b2919f65d80e5e6aed15a97fe66", "update": "apt"}
	  },
	  "steps": [],
	  "files": {}
	}`)

	m, err := bundle.LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Caddy != nil {
		t.Fatalf("manifest without caddy object must leave Caddy nil: %+v", m.Caddy)
	}
	ts, ok := m.Packages["tailscale"]
	if !ok || ts.Version != "1.104.5" || ts.File != "tailscale_1.104.5_arm64.deb" || ts.Update != "apt" {
		t.Fatalf("tailscale pin not read: %+v", ts)
	}
	if ts.SHA256 != "7ce61e75d15c1269d0aebe3ed59524b83225121cba1b8ea682d04594195f6bc1" {
		t.Fatalf("tailscale sha256 not read: %q", ts.SHA256)
	}
	caddy, ok := m.Packages["caddy"]
	if !ok || caddy.URL != "https://github.com/caddyserver/caddy/releases/download/v2.11.9/caddy_2.11.9_linux_arm64.deb" {
		t.Fatalf("caddy pin not read: %+v", caddy)
	}
}

func TestStepEntryOmitsEmptyDashboardKey(t *testing.T) {
	raw, err := json.Marshal(bundle.StepEntry{ID: "10", Optional: false})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got := string(raw); strings.Contains(got, "dashboard_key") {
		t.Fatalf("dashboard_key sollte bei leerem Wert fehlen: %s", got)
	}
}
