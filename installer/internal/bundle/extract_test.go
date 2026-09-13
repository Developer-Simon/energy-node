package bundle_test

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
)

// writeTestArchive builds a .tar.gz containing the given path -> content
// entries, creating intermediate directories as tar headers the way a real
// tarball would.
func writeTestArchive(t *testing.T, archivePath string, files map[string]string) {
	t.Helper()
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("creating archive: %v", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("writing header for %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("writing content for %s: %v", name, err)
		}
	}
}

func TestExtractArchiveWritesAllFiles(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "bundle.tar.gz")
	writeTestArchive(t, archive, map[string]string{
		"manifest.json":       `{"version":"v0.2.0"}`,
		"bootstrap/10-apt.sh": "#!/bin/sh\necho hallo\n",
	})

	dest := filepath.Join(dir, "extracted")
	if err := bundle.ExtractArchive(archive, dest); err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}

	manifest, err := os.ReadFile(filepath.Join(dest, "manifest.json"))
	if err != nil {
		t.Fatalf("reading extracted manifest.json: %v", err)
	}
	if string(manifest) != `{"version":"v0.2.0"}` {
		t.Fatalf("unexpected manifest.json content: %q", manifest)
	}

	script, err := os.ReadFile(filepath.Join(dest, "bootstrap", "10-apt.sh"))
	if err != nil {
		t.Fatalf("reading extracted bootstrap/10-apt.sh: %v", err)
	}
	if string(script) != "#!/bin/sh\necho hallo\n" {
		t.Fatalf("unexpected script content: %q", script)
	}
}

func TestExtractArchiveRejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "evil.tar.gz")
	writeTestArchive(t, archive, map[string]string{
		"../../etc/passwd": "root:x:0:0::/root:/bin/sh\n",
	})

	dest := filepath.Join(dir, "extracted")
	if err := bundle.ExtractArchive(archive, dest); err == nil {
		t.Fatalf("expected an error for a path-traversing archive entry")
	}
	if _, err := os.Stat(filepath.Join(dir, "etc", "passwd")); err == nil {
		t.Fatalf("path traversal actually wrote outside the destination")
	}
}

func TestExtractArchiveRejectsAMissingFile(t *testing.T) {
	dir := t.TempDir()
	if err := bundle.ExtractArchive(filepath.Join(dir, "does-not-exist.tar.gz"), filepath.Join(dir, "out")); err == nil {
		t.Fatalf("expected an error for a missing archive")
	}
}
