// Package bundlefetchtest holds the fixtures the bundlefetch, updaterhost and
// cmd/dashboard tests share: a bundle archive builder and a fake GitHub
// releases server. It must not import bundlefetch (bundlefetch's own tests
// import this package).
package bundlefetchtest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// Entry is one tar entry. Type 0 means a regular file, Mode 0 means 0644.
type Entry struct {
	Name string
	Body string // file content, or the link target for a symlink
	Mode int64
	Type byte
}

// Archive builds a .tar.gz from entries.
func Archive(t testing.TB, entries []Entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.Type
		if typ == 0 {
			typ = tar.TypeReg
		}
		mode := e.Mode
		if mode == 0 {
			mode = 0o644
		}
		hdr := &tar.Header{Name: e.Name, Typeflag: typ, Mode: mode}
		switch typ {
		case tar.TypeReg:
			hdr.Size = int64(len(e.Body))
		case tar.TypeSymlink:
			hdr.Linkname = e.Body
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header %q: %v", e.Name, err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.Body)); err != nil {
				t.Fatalf("tar body %q: %v", e.Name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// BundleArchive builds what make_bundle.sh produces, reduced to the parts
// bundlefetch looks at: ./-rooted entries, manifest.json, an optional
// manifest.json.sig and one executable script.
func BundleArchive(t testing.TB, version, arch string, signed bool) []byte {
	t.Helper()
	entries := []Entry{
		{Name: "./", Type: tar.TypeDir, Mode: 0o755},
		{Name: "./manifest.json", Body: fmt.Sprintf(`{"version":%q,"arch":%q}`, version, arch)},
		{Name: "./bootstrap/", Type: tar.TypeDir, Mode: 0o755},
		{Name: "./bootstrap/10-apt.sh", Body: "#!/bin/sh\n", Mode: 0o755},
	}
	if signed {
		entries = append(entries, Entry{Name: "./manifest.json.sig", Body: "sig"})
	}
	return Archive(t, entries)
}

// ReleasesServer fakes the two GitHub endpoints bundlefetch uses. The
// returned counter counts asset downloads.
func ReleasesServer(t testing.TB, tag, assetName string, archive []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var downloads atomic.Int32
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `[{"tag_name":%q,"draft":false,"prerelease":false,"assets":[{"name":%q,"browser_download_url":%q,"size":%d}]}]`,
			tag, assetName, srv.URL+"/dl", len(archive))
	})
	mux.HandleFunc("/dl", func(w http.ResponseWriter, _ *http.Request) {
		downloads.Add(1)
		_, _ = w.Write(archive)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &downloads
}
