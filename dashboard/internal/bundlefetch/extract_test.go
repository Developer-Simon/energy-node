package bundlefetch

import (
	"archive/tar"
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/bundlefetch/bundlefetchtest"
)

func writeArchive(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtractUnpacksADotRootedBundleAndKeepsTheExecutableBit(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "out")
	err := extractArchive(writeArchive(t, bundlefetchtest.BundleArchive(t, "v1", "armv6", true)), dest)
	if err != nil {
		t.Fatalf("extractArchive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "manifest.json")); err != nil {
		t.Fatalf("manifest.json missing: %v", err)
	}
	info, err := os.Stat(filepath.Join(dest, "bootstrap", "10-apt.sh"))
	if err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("10-apt.sh lost its executable bit: %v, %v", info, err)
	}
}

func TestExtractConfinesEveryEntryToTheDestination(t *testing.T) {
	cases := map[string][]bundlefetchtest.Entry{
		"parent traversal": {{Name: "../evil", Body: "x"}},
		"nested traversal": {{Name: "a/../../evil", Body: "x"}},
		"absolute path":    {{Name: "/tmp/evil", Body: "x"}},
		"symlink":          {{Name: "link", Type: tar.TypeSymlink, Body: "/etc/passwd"}},
	}
	for name, entries := range cases {
		root := t.TempDir()
		dest := filepath.Join(root, "out")
		err := extractArchive(writeArchive(t, bundlefetchtest.Archive(t, entries)), dest)
		if e := asError(err); e == nil || e.Code != CodeBundleInvalid {
			t.Errorf("%s: err = %v, want %s", name, err, CodeBundleInvalid)
		}
		if _, statErr := os.Stat(filepath.Join(root, "evil")); statErr == nil {
			t.Errorf("%s: a file escaped the destination", name)
		}
	}
}

func TestExtractRefusesANonGzipFile(t *testing.T) {
	err := extractArchive(writeArchive(t, []byte("not gzip")), filepath.Join(t.TempDir(), "out"))
	if e := asError(err); e == nil || e.Code != CodeBundleInvalid {
		t.Fatalf("err = %v, want %s", err, CodeBundleInvalid)
	}
}

func TestExtractEnforcesTheEntryAndSizeLimits(t *testing.T) {
	oldEntries, oldBytes := maxEntries, maxExtractedBytes
	t.Cleanup(func() { maxEntries, maxExtractedBytes = oldEntries, oldBytes })

	maxEntries = 2
	many := bundlefetchtest.Archive(t, []bundlefetchtest.Entry{{Name: "a", Body: "1"}, {Name: "b", Body: "2"}, {Name: "c", Body: "3"}})
	if e := asError(extractArchive(writeArchive(t, many), filepath.Join(t.TempDir(), "o"))); e == nil || e.Code != CodeBundleTooLarge {
		t.Errorf("entry limit: err = %v, want %s", e, CodeBundleTooLarge)
	}

	maxEntries, maxExtractedBytes = oldEntries, 5
	big := bundlefetchtest.Archive(t, []bundlefetchtest.Entry{{Name: "a", Body: "0123456789"}})
	if e := asError(extractArchive(writeArchive(t, big), filepath.Join(t.TempDir(), "o"))); e == nil || e.Code != CodeBundleTooLarge {
		t.Errorf("size limit: err = %v, want %s", e, CodeBundleTooLarge)
	}
}
