// dashboard/cmd/dashboard/redeploy_test.go
package main

import (
	"context"
	"encoding/json"
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
	"github.com/Developer-Simon/energy-node-dashboard/internal/localize"
	"github.com/Developer-Simon/energy-node-dashboard/internal/updatecheck"
	"github.com/Developer-Simon/energy-node-dashboard/internal/updaterjob"
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
	err := prepareFunc(fetcher)(context.Background(), func(string, map[string]string) {})
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

func TestTheRedeployScreenFollowsTheDashboardLanguage(t *testing.T) {
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
		languages: []string{"de", "en"},
	})
	if err != nil {
		t.Fatalf("buildRedeployHandler: %v", err)
	}

	cases := []struct {
		name, cookie, accept, want string
	}{
		{"no cookie, no header", "", "", "de"},
		{"browser language", "", "en-GB,en;q=0.9", "en"},
		{"cookie beats the browser", "de", "en-GB,en;q=0.9", "de"},
		{"cookie alone", "en", "", "en"},
		{"unknown cookie is ignored", "xx", "en", "en"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/redeploy/api/bootstrap", nil)
		if c.cookie != "" {
			req.AddCookie(&http.Cookie{Name: localize.CookieName, Value: c.cookie})
		}
		if c.accept != "" {
			req.Header.Set("Accept-Language", c.accept)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		var got hostapi.Bootstrap
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("%s: body is not JSON: %v", c.name, err)
		}
		if got.Language != c.want || !got.LanguageFixed {
			t.Errorf("%s: language=%q fixed=%v, want %s and no own switch", c.name, got.Language, got.LanguageFixed, c.want)
		}
	}
}

// The changelog endpoint is found by a type assertion on the backend, so a
// wrong method signature would not fail to compile -- it would answer 501. This
// walks the real handler chain (hostapi.Server over updaterhost.Host).
func TestRedeployHandlerServesTheCandidatesChangelogWithWhatIsInstalled(t *testing.T) {
	root := t.TempDir()
	candidate := filepath.Join(root, "candidate")
	os.MkdirAll(candidate, 0o755)
	os.WriteFile(filepath.Join(candidate, "manifest.json"), []byte(`{"version":"v0.7.5","steps":[]}`), 0o644)
	os.WriteFile(filepath.Join(candidate, "changelog.json"), []byte(`{"schema_version":1,"components":[]}`), 0o644)
	os.WriteFile(filepath.Join(root, "installed-manifest.json"), []byte(`{"version":"v0.7.4","components":{"dashboard":"v0.7.4"}}`), 0o644)
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

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/redeploy/api/changelog", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		BundleVersion string            `json:"bundle_version"`
		Installed     map[string]string `json:"installed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.BundleVersion != "v0.7.5" || body.Installed["dashboard"] != "v0.7.4" {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestNewStepTitlerFollowsTheRequestLanguage(t *testing.T) {
	title, err := newStepTitler([]string{"de", "en"}, filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	de := httptest.NewRequest(http.MethodGet, "/", nil)
	de.Header.Set("Accept-Language", "de-DE")
	en := httptest.NewRequest(http.MethodGet, "/", nil)
	en.Header.Set("Accept-Language", "en-GB")
	if got := title(de, "60"); got != "Dashboard und MQTT-Brücke" {
		t.Fatalf("de title = %q", got)
	}
	if got := title(en, "60"); got == "" || got == "Dashboard und MQTT-Brücke" {
		t.Fatalf("en title = %q", got)
	}
	if got := title(de, "999"); got != "" {
		t.Fatalf("unknown step = %q, want empty", got)
	}
}

// Die Update-Leiste nennt einen Dienstschritt wie der Redeploy-Bildschirm
// (services.js stepLabel): über service.<service_id> aus dem installierten
// Manifest, weil es für 81 bis 88 kein step.<id> gibt.
func TestNewStepTitlerNamesServiceStepsFromTheInstalledManifest(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "installed-manifest.json")
	if err := os.WriteFile(manifest, []byte(`{"version":"1.5.0","steps":[{"id":"60"},{"id":"81","service_id":"apsystems"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	title, err := newStepTitler([]string{"de", "en"}, manifest)
	if err != nil {
		t.Fatal(err)
	}
	en := httptest.NewRequest(http.MethodGet, "/", nil)
	en.Header.Set("Accept-Language", "en-GB")
	if got := title(en, "81"); got != "APsystems" {
		t.Fatalf("service step title = %q, want APsystems", got)
	}
	if got := title(en, "60"); got != "Dashboard and MQTT bridge" {
		t.Fatalf("system step title = %q", got)
	}
	if got := title(en, "88"); got != "" {
		t.Fatalf("step missing from the manifest = %q, want empty", got)
	}
}

func TestUpdateInstallerStagesARedeployJobForTheOfferedVersion(t *testing.T) {
	archive := bundlefetchtest.BundleArchive(t, "v9.9.9", "armv6", true)
	srv, _ := bundlefetchtest.ReleasesServer(t, "v9.9.9", "energy-node-v9.9.9-armv6.tar.gz", archive)

	root := t.TempDir()
	candidate := filepath.Join(root, "redeploy-candidate")
	os.WriteFile(filepath.Join(root, "installed-manifest.json"), []byte(`{"version":"v9.0.0"}`), 0o644)
	os.WriteFile(filepath.Join(root, "selection.json"), []byte(`{"steps":{}}`), 0o644)
	jobDir := filepath.Join(root, "job")
	os.MkdirAll(jobDir, 0o755)

	fetcher := &bundlefetch.Fetcher{Client: &bundlefetch.Client{APIBase: srv.URL, Repo: "o/r"}, Arch: "armv6", DestDir: candidate}
	setup, err := buildRedeploy(redeployConfig{
		candidateBundleDir: candidate, installedManifestPath: filepath.Join(root, "installed-manifest.json"),
		selectionPath: filepath.Join(root, "selection.json"), jobDir: jobDir,
		prepare: prepareFunc(fetcher),
	})
	if err != nil {
		t.Fatalf("buildRedeploy: %v", err)
	}
	// Let the staged job's tail finish so its goroutine does not outlive the test.
	defer os.WriteFile(filepath.Join(jobDir, "status.json"), []byte(`{"result":"ok"}`), 0o644)

	cache := &updatecheck.Cache{}
	cache.Set(updatecheck.Result{Current: "9.0.0", Latest: "9.9.9", Available: true, CheckedAt: time.Now()})
	installer := newUpdateInstaller(setup, jobDir, cache, func(context.Context) {})
	if installer == nil {
		t.Fatal("newUpdateInstaller = nil, want an installer for a fetching host")
	}

	version, err := installer.Install(context.Background())
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if version != "9.9.9" {
		t.Fatalf("Install version = %q, want 9.9.9", version)
	}

	deadline := time.Now().Add(5 * time.Second)
	for !updaterjob.Pending(jobDir) {
		if time.Now().After(deadline) {
			t.Fatalf("no pending job after Install; status = %+v", installer.Status())
		}
		time.Sleep(20 * time.Millisecond)
	}
	job, err := updaterjob.ReadPending(jobDir)
	if err != nil {
		t.Fatalf("ReadPending: %v", err)
	}
	if job.BundleVersion != "v9.9.9" || job.Mode != "redeploy" {
		t.Fatalf("pending job = %+v, want bundle v9.9.9 in mode redeploy", job)
	}
}

func TestUpdateInstallerIsNilWithoutPrepare(t *testing.T) {
	root := t.TempDir()
	candidate := filepath.Join(root, "candidate")
	os.MkdirAll(candidate, 0o755)
	os.WriteFile(filepath.Join(root, "installed-manifest.json"), []byte(`{"version":"1.4.0"}`), 0o644)
	os.WriteFile(filepath.Join(root, "selection.json"), []byte(`{"steps":{}}`), 0o644)
	jobDir := filepath.Join(root, "job")
	os.MkdirAll(jobDir, 0o755)

	setup, err := buildRedeploy(redeployConfig{
		candidateBundleDir: candidate, installedManifestPath: filepath.Join(root, "installed-manifest.json"),
		selectionPath: filepath.Join(root, "selection.json"), jobDir: jobDir,
	})
	if err != nil {
		t.Fatalf("buildRedeploy: %v", err)
	}
	if installer := newUpdateInstaller(setup, jobDir, &updatecheck.Cache{}, func(context.Context) {}); installer != nil {
		t.Fatal("newUpdateInstaller != nil without a prepare seam, want nil")
	}
	if installer := newUpdateInstaller(nil, jobDir, &updatecheck.Cache{}, func(context.Context) {}); installer != nil {
		t.Fatal("newUpdateInstaller(nil setup) != nil, want nil")
	}
}
