package bundlefetch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/bundlefetch/bundlefetchtest"
)

const assetName = "energy-node-v9.9.9-armv6.tar.gz"

func newFetcher(t *testing.T, archive []byte) (*Fetcher, string, func() int32) {
	t.Helper()
	srv, downloads := bundlefetchtest.ReleasesServer(t, "v9.9.9", assetName, archive)
	dest := filepath.Join(t.TempDir(), "redeploy-candidate")
	f := &Fetcher{Client: &Client{APIBase: srv.URL, Repo: "o/r"}, Arch: "armv6", DestDir: dest}
	return f, dest, downloads.Load
}

func TestFetchDownloadsExtractsAndInstallsTheBundle(t *testing.T) {
	f, dest, downloads := newFetcher(t, bundlefetchtest.BundleArchive(t, "v9.9.9", "armv6", true))
	var lines []string
	if err := f.Fetch(context.Background(), func(l string) { lines = append(lines, l) }); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := candidateVersion(dest, "armv6"); got != "v9.9.9" {
		t.Fatalf("candidate version = %q, want v9.9.9", got)
	}
	if _, err := os.Stat(dest + ".work"); err == nil {
		t.Errorf("the .work scratch directory was not removed")
	}
	if downloads() != 1 {
		t.Errorf("downloads = %d, want 1", downloads())
	}
	if len(lines) < 3 || !strings.Contains(strings.Join(lines, "\n"), "liegt bereit") {
		t.Errorf("log = %q", lines)
	}
}

func TestFetchSkipsTheDownloadWhenTheSameVersionIsAlreadyThere(t *testing.T) {
	f, _, downloads := newFetcher(t, bundlefetchtest.BundleArchive(t, "v9.9.9", "armv6", true))
	if err := f.Fetch(context.Background(), nil); err != nil {
		t.Fatalf("first Fetch: %v", err)
	}
	if err := f.Fetch(context.Background(), nil); err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if downloads() != 1 {
		t.Errorf("downloads = %d, want 1 (the second run must reuse the candidate)", downloads())
	}
}

func TestAFailedFetchLeavesTheExistingCandidateUntouched(t *testing.T) {
	cases := map[string]struct {
		archive []byte
		code    string
	}{
		"wrong arch": {bundlefetchtest.BundleArchive(t, "v9.9.9", "amd64", true), CodeArchMismatch},
		"unsigned":   {bundlefetchtest.BundleArchive(t, "v9.9.9", "armv6", false), CodeBundleUnsigned},
		"not a gzip": {[]byte("garbage"), CodeBundleInvalid},
	}
	for name, c := range cases {
		f, dest, _ := newFetcher(t, c.archive)
		writeDir(t, dest, map[string]string{"manifest.json": `{"version":"v1.0.0","arch":"armv6"}`, "manifest.json.sig": "s"})
		err := f.Fetch(context.Background(), nil)
		if e := asError(err); e == nil || e.Code != c.code {
			t.Errorf("%s: err = %v, want %s", name, err, c.code)
		}
		if got := candidateVersion(dest, "armv6"); got != "v1.0.0" {
			t.Errorf("%s: the previous candidate was replaced or damaged (version %q)", name, got)
		}
		if _, statErr := os.Stat(dest + ".work"); statErr == nil {
			t.Errorf("%s: scratch directory left behind", name)
		}
	}
}

func TestFetchReportsNoReleaseForAnArchWithoutABundle(t *testing.T) {
	f, _, _ := newFetcher(t, bundlefetchtest.BundleArchive(t, "v9.9.9", "armv6", true))
	f.Arch = "arm64"
	err := f.Fetch(context.Background(), nil)
	if e := asError(err); e == nil || e.Code != CodeGitHubNoRelease {
		t.Fatalf("err = %v, want %s", err, CodeGitHubNoRelease)
	}
}
