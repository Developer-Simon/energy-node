// dashboard/cmd/dashboard/redeploy_test.go
package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/bundlefetch"
	"github.com/Developer-Simon/energy-node-dashboard/internal/bundlefetch/bundlefetchtest"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

func TestBuildRedeployHandlerServesTheBootstrapEndpointWithoutAToken(t *testing.T) {
	root := t.TempDir()
	candidate := filepath.Join(root, "candidate")
	os.MkdirAll(candidate, 0o755)
	os.WriteFile(filepath.Join(candidate, "manifest.json"), []byte(`{"version":"1.5.0","steps":[]}`), 0o644)
	os.WriteFile(filepath.Join(root, "installed-manifest.json"), []byte(`{"version":"1.4.0"}`), 0o644)
	os.WriteFile(filepath.Join(root, "selection.json"), []byte(`{"steps":{}}`), 0o644)
	jobDir := filepath.Join(root, "job")
	os.MkdirAll(jobDir, 0o755)

	handler, err := buildRedeployHandler(redeployConfig{
		candidateBundleDir: candidate, installedManifestPath: filepath.Join(root, "installed-manifest.json"),
		selectionPath: filepath.Join(root, "selection.json"), jobDir: jobDir,
	})
	if err != nil {
		t.Fatalf("buildRedeployHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/redeploy/api/bootstrap", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestPrepareRunFetchesTheNewestBundleIntoTheCandidateDirectory(t *testing.T) {
	archive := bundlefetchtest.BundleArchive(t, "v9.9.9", "armv6", true)
	srv, downloads := bundlefetchtest.ReleasesServer(t, "v9.9.9", "energy-node-v9.9.9-armv6.tar.gz", archive)

	root := t.TempDir()
	candidate := filepath.Join(root, "redeploy-candidate")
	os.WriteFile(filepath.Join(root, "installed-manifest.json"), []byte(`{"version":"v9.0.0"}`), 0o644)
	os.WriteFile(filepath.Join(root, "selection.json"), []byte(`{"steps":{}}`), 0o644)
	jobDir := filepath.Join(root, "job")
	os.MkdirAll(jobDir, 0o755)

	fetcher := &bundlefetch.Fetcher{Client: &bundlefetch.Client{APIBase: srv.URL, Repo: "o/r"}, Arch: "armv6", DestDir: candidate}
	handler, err := buildRedeployHandler(redeployConfig{
		candidateBundleDir: candidate, installedManifestPath: filepath.Join(root, "installed-manifest.json"),
		selectionPath: filepath.Join(root, "selection.json"), jobDir: jobDir,
		prepare: prepareFunc(fetcher),
	})
	if err != nil {
		t.Fatalf("buildRedeployHandler: %v", err)
	}
	get := func(path string) string {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Body.String()
	}

	boot := get("/redeploy/api/bootstrap")
	if !strings.Contains(boot, `"auto_prepare":true`) || strings.Contains(boot, `"bundle_version":"v9.9.9"`) {
		t.Fatalf("bootstrap before prepare: %s", boot)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/redeploy/api/run", strings.NewReader(`{"mode":"prepare"}`)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /api/run = %d: %s", rec.Code, rec.Body.String())
	}

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(get("/redeploy/api/bootstrap"), `"bundle_version":"v9.9.9"`) {
		if time.Now().After(deadline) {
			t.Fatalf("the fetched bundle never showed up in bootstrap; events: %s", get("/redeploy/api/events?once=1"))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if downloads.Load() != 1 {
		t.Errorf("downloads = %d, want 1", downloads.Load())
	}
	if _, err := os.Stat(filepath.Join(candidate, "manifest.json.sig")); err != nil {
		t.Errorf("the candidate lacks manifest.json.sig: %v", err)
	}
}

func TestPrepareFuncKeepsTheErrorCodeOfABundlefetchError(t *testing.T) {
	fetcher := &bundlefetch.Fetcher{Client: &bundlefetch.Client{APIBase: "http://127.0.0.1:1", Repo: "o/r"}, Arch: "armv6", DestDir: filepath.Join(t.TempDir(), "c")}
	err := prepareFunc(fetcher)(context.Background(), func(string) {})
	var typed *hostapi.Error
	if !errors.As(err, &typed) || typed.Code != bundlefetch.CodeGitHubUnreachable {
		t.Fatalf("err = %v, want a hostapi error with code %s", err, bundlefetch.CodeGitHubUnreachable)
	}
}

// A dashboard that restarts mid-job (step 60 replaces its own binary) must
// resume under the run id the page already follows, or the page never
// accepts the resumed run's run-finished.
func TestResumedRunKeepsTheRunIDOfTheStagedJob(t *testing.T) {
	root := t.TempDir()
	candidate := filepath.Join(root, "candidate")
	os.MkdirAll(candidate, 0o755)
	os.WriteFile(filepath.Join(candidate, "manifest.json"), []byte(`{"version":"1.5.0","steps":[]}`), 0o644)
	os.WriteFile(filepath.Join(root, "selection.json"), []byte(`{"steps":{}}`), 0o644)
	jobDir := filepath.Join(root, "job")
	os.MkdirAll(jobDir, 0o755)
	os.WriteFile(filepath.Join(jobDir, "current.json"), []byte(`{"bundle_version":"1.5.0","mode":"redeploy","steps":["60"],"run_id":"run-9"}`), 0o644)
	os.WriteFile(filepath.Join(jobDir, "log"), []byte("1000 ##STEP 60 begin\n"), 0o644)

	handler, err := buildRedeployHandler(redeployConfig{
		candidateBundleDir: candidate, installedManifestPath: filepath.Join(root, "installed-manifest.json"),
		selectionPath: filepath.Join(root, "selection.json"), jobDir: jobDir,
	})
	if err != nil {
		t.Fatalf("buildRedeployHandler: %v", err)
	}
	// Let the resumed tail finish so its goroutine does not outlive the test.
	defer os.WriteFile(filepath.Join(jobDir, "status.json"), []byte(`{"result":"ok"}`), 0o644)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/redeploy/api/events?since=0&once=1", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `"running":true`) || !strings.Contains(body, `"run_id":"run-9"`) {
		t.Fatalf("hello does not report the staged run: %s", body)
	}
	if !strings.Contains(body, "event: run-started") {
		t.Fatalf("the restored bus has no run-started for the page to pick up: %s", body)
	}
}
