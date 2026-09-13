package bundle_test

import (
	"errors"
	"os"
	"path/filepath"
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
	  "wheels": {"paho_mqtt": "2.1.0"},
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
