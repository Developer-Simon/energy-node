package bundlesource

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func asError(err error, target **Error) bool { return errors.As(err, target) }

// releasesServer serves a releases listing plus the asset bytes.
func releasesServer(t *testing.T, downloads *atomic.Int32) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[
		  {"tag_name":"installer-0.2.0","draft":false,"prerelease":false,"assets":[
		    {"name":"energy-node-installer_0.2.0_linux_amd64.tar.gz","browser_download_url":%q}]},
		  {"tag_name":"v0.8.0-rc1","draft":false,"prerelease":true,"assets":[
		    {"name":"energy-node-v0.8.0-rc1-armv6.tar.gz","browser_download_url":%q}]},
		  {"tag_name":"v0.7.0","draft":false,"prerelease":false,"assets":[
		    {"name":"energy-node-dashboard_v0.7.0_linux_arm64.tar.gz","browser_download_url":%q},
		    {"name":"energy-node-v0.7.0-armv6.tar.gz","browser_download_url":%q}]}
		]`, srv.URL+"/dl/x", srv.URL+"/dl/rc", srv.URL+"/dl/dash", srv.URL+"/dl/v070")
	})
	mux.HandleFunc("/dl/v070", func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		w.Write([]byte("bundle-bytes"))
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newGitHub(t *testing.T, srv *httptest.Server) *GitHub {
	return &GitHub{APIBase: srv.URL, Repo: "o/r", CacheDir: t.TempDir()}
}

func TestFindAssetSkipsPrereleasesAndNonBundleAssets(t *testing.T) {
	var n atomic.Int32
	g := newGitHub(t, releasesServer(t, &n))
	asset, err := g.FindAsset(context.Background(), "armv6")
	if err != nil {
		t.Fatalf("FindAsset: %v", err)
	}
	if asset.Tag != "v0.7.0" || asset.Name != "energy-node-v0.7.0-armv6.tar.gz" {
		t.Errorf("asset = %+v", asset)
	}
}

func TestFindAssetReportsNoReleaseForAnArchWithoutABundle(t *testing.T) {
	var n atomic.Int32
	g := newGitHub(t, releasesServer(t, &n))
	_, err := g.FindAsset(context.Background(), "arm64")
	var se *Error
	if !asError(err, &se) || se.Code != CodeGitHubNoRelease || se.Detail != "arm64" {
		t.Fatalf("err = %v, want %s for arm64", err, CodeGitHubNoRelease)
	}
}

func TestFetchDownloadsOnceAndThenUsesTheCache(t *testing.T) {
	var n atomic.Int32
	g := newGitHub(t, releasesServer(t, &n))
	var logged []string
	log := func(l string) { logged = append(logged, l) }

	first, err := g.Fetch(context.Background(), "armv6", log)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	raw, _ := os.ReadFile(first)
	if string(raw) != "bundle-bytes" {
		t.Fatalf("downloaded = %q", raw)
	}
	second, err := g.Fetch(context.Background(), "armv6", log)
	if err != nil || second != first {
		t.Fatalf("second Fetch = %q, %v; want the cached %q", second, err, first)
	}
	if n.Load() != 1 {
		t.Errorf("downloads = %d, want exactly 1", n.Load())
	}
	if len(logged) == 0 {
		t.Errorf("Fetch logged nothing")
	}
}

func TestAServerErrorIsGitHubUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	g := &GitHub{APIBase: srv.URL, Repo: "o/r", CacheDir: t.TempDir()}
	_, err := g.FindAsset(context.Background(), "armv6")
	var se *Error
	if !asError(err, &se) || se.Code != CodeGitHubUnreachable || !strings.Contains(se.Detail, "403") {
		t.Fatalf("err = %v, want %s mentioning 403", err, CodeGitHubUnreachable)
	}
}
