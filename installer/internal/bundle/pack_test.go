package bundle_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
)

func writeFile(t *testing.T, root, rel, body string, mode os.FileMode) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(body), mode); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestHasSignatureIsTrueOnlyWithAManifestSigFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "manifest.json", "{}", 0o644)
	if bundle.HasSignature(dir) {
		t.Fatalf("no manifest.json.sig, HasSignature must be false")
	}
	writeFile(t, dir, "manifest.json.sig", "sig", 0o644)
	if !bundle.HasSignature(dir) {
		t.Fatalf("manifest.json.sig present, HasSignature must be true")
	}
}

func TestPackDirRoundTripsThroughExtractArchive(t *testing.T) {
	src := t.TempDir()
	writeFile(t, src, "manifest.json", `{"version":"v1"}`, 0o644)
	writeFile(t, src, "bootstrap/run.sh", "#!/bin/sh\n", 0o755)

	archive := filepath.Join(t.TempDir(), "b.tar.gz")
	if err := bundle.PackDir(src, archive); err != nil {
		t.Fatalf("PackDir: %v", err)
	}
	dest := t.TempDir()
	if err := bundle.ExtractArchive(archive, dest); err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "manifest.json"))
	if err != nil || string(got) != `{"version":"v1"}` {
		t.Fatalf("manifest.json = %q, %v", got, err)
	}
	info, err := os.Stat(filepath.Join(dest, "bootstrap", "run.sh"))
	if err != nil {
		t.Fatalf("stat run.sh: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("run.sh lost its executable bit: %v", info.Mode())
	}
}
