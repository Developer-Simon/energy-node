package devcli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/devcli"
)

func TestLoadTargetParsesKeyValueLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deploy-target.env")
	content := "# comment\nTARGET_USER=orgelbau\nTARGET_HOST=100.82.49.43\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	target, err := devcli.LoadTarget(path)
	if err != nil {
		t.Fatalf("LoadTarget: %v", err)
	}
	if target.User != "orgelbau" || target.Host != "100.82.49.43" {
		t.Fatalf("unexpected target: %+v", target)
	}
	if target.Base != "/home/orgelbau" {
		t.Fatalf("expected TARGET_BASE to default to /home/<user>, got %q", target.Base)
	}
}

func TestLoadTargetHonoursAnExplicitBase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deploy-target.env")
	content := "TARGET_USER=energynode\nTARGET_HOST=10.0.0.5\nTARGET_BASE=/opt/energy-node\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	target, err := devcli.LoadTarget(path)
	if err != nil {
		t.Fatalf("LoadTarget: %v", err)
	}
	if target.Base != "/opt/energy-node" {
		t.Fatalf("expected the explicit TARGET_BASE to win, got %q", target.Base)
	}
}

func TestLoadTargetReportsAMissingFile(t *testing.T) {
	if _, err := devcli.LoadTarget(filepath.Join(t.TempDir(), "does-not-exist.env")); err == nil {
		t.Fatalf("expected an error for a missing target file")
	}
}

func TestOverrideOnlyReplacesNonEmptyFields(t *testing.T) {
	base := devcli.Target{Host: "10.0.0.5", User: "energynode", Base: "/home/energynode"}
	got := base.Override("", "root", "")
	want := devcli.Target{Host: "10.0.0.5", User: "root", Base: "/home/energynode"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestDefaultDeployTargetPathJoinsSecretsDirectory(t *testing.T) {
	got := devcli.DefaultDeployTargetPath("/repo")
	want := filepath.Join("/repo", "secrets", "deploy-target.env")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
