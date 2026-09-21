package bundlefetch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func asError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return nil
}

func TestFindAssetSkipsPrereleasesDraftsAndForeignAssets(t *testing.T) {
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `[
		  {"tag_name":"installer-0.2.0","draft":false,"prerelease":false,"assets":[
		    {"name":"energy-node-installer_0.2.0_linux_amd64.tar.gz","browser_download_url":"%[1]s/x"}]},
		  {"tag_name":"v0.9.0","draft":true,"prerelease":false,"assets":[
		    {"name":"energy-node-v0.9.0-armv6.tar.gz","browser_download_url":"%[1]s/draft"}]},
		  {"tag_name":"v0.8.0-rc1","draft":false,"prerelease":true,"assets":[
		    {"name":"energy-node-v0.8.0-rc1-armv6.tar.gz","browser_download_url":"%[1]s/rc"}]},
		  {"tag_name":"v0.7.0","draft":false,"prerelease":false,"assets":[
		    {"name":"energy-node-dashboard_v0.7.0_linux_arm64.tar.gz","browser_download_url":"%[1]s/dash"},
		    {"name":"energy-node-v0.7.0-armv6.tar.gz","browser_download_url":"%[1]s/v070","size":1234}]}
		]`, srv.URL)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := &Client{APIBase: srv.URL, Repo: "o/r"}

	asset, err := c.FindAsset(context.Background(), "armv6")
	if err != nil {
		t.Fatalf("FindAsset: %v", err)
	}
	if asset.Tag != "v0.7.0" || asset.Version != "v0.7.0" || asset.Name != "energy-node-v0.7.0-armv6.tar.gz" || asset.Size != 1234 {
		t.Errorf("asset = %+v", asset)
	}
	if !strings.HasSuffix(asset.URL, "/v070") {
		t.Errorf("asset.URL = %q", asset.URL)
	}

	_, err = c.FindAsset(context.Background(), "arm64")
	if e := asError(err); e == nil || e.Code != CodeGitHubNoRelease || e.Detail != "arm64" {
		t.Fatalf("err = %v, want %s for arm64 (the dashboard asset must not match)", err, CodeGitHubNoRelease)
	}
}

func TestAServerErrorIsGitHubUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	_, err := (&Client{APIBase: srv.URL, Repo: "o/r"}).FindAsset(context.Background(), "armv6")
	if e := asError(err); e == nil || e.Code != CodeGitHubUnreachable || !strings.Contains(e.Detail, "403") {
		t.Fatalf("err = %v, want %s mentioning 403", err, CodeGitHubUnreachable)
	}
}

func TestDownloadWritesTheFileAndLogsProgress(t *testing.T) {
	body := strings.Repeat("x", 1000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
	t.Cleanup(srv.Close)
	dest := filepath.Join(t.TempDir(), "a.tar.gz")
	var lines []string
	err := (&Client{}).Download(context.Background(), Asset{URL: srv.URL}, dest, func(l string) { lines = append(lines, l) })
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if got, _ := os.ReadFile(dest); string(got) != body {
		t.Errorf("downloaded %d bytes, want %d", len(got), len(body))
	}
	if len(lines) == 0 || lines[len(lines)-1] != "100 % geladen" {
		t.Errorf("progress lines = %q, want them to end at 100 %%", lines)
	}
}

func TestDownloadRefusesAnArchiveOverTheCap(t *testing.T) {
	old := maxArchiveBytes
	maxArchiveBytes = 10
	t.Cleanup(func() { maxArchiveBytes = old })

	sized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 100)))
	}))
	t.Cleanup(sized.Close)
	chunked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.(http.Flusher).Flush() // no Content-Length: the cap must hold while reading
		_, _ = w.Write([]byte(strings.Repeat("x", 100)))
	}))
	t.Cleanup(chunked.Close)

	for name, url := range map[string]string{"content-length": sized.URL, "chunked": chunked.URL} {
		err := (&Client{}).Download(context.Background(), Asset{URL: url}, filepath.Join(t.TempDir(), "a"), func(string) {})
		if e := asError(err); e == nil || e.Code != CodeBundleTooLarge {
			t.Errorf("%s: err = %v, want %s", name, err, CodeBundleTooLarge)
		}
	}
}
