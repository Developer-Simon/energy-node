// dashboard/internal/updaterhost/host_test.go
package updaterhost_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/updaterhost"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

type recordingSink struct{ markers, logs []string }

func (s *recordingSink) Marker(stepID, state, detail string) {
	s.markers = append(s.markers, stepID+" "+state)
}
func (s *recordingSink) Log(stepID, line string) { s.logs = append(s.logs, line) }
func (s *recordingSink) Message(stepID, key string, args map[string]string) {
	s.logs = append(s.logs, key)
}

func setupNode(t *testing.T) updaterhost.Config {
	t.Helper()
	root := t.TempDir()
	candidate := filepath.Join(root, "candidate")
	os.MkdirAll(candidate, 0o755)
	manifest := map[string]any{
		"version": "1.5.0", "arch": "armv6",
		"components": map[string]string{"dashboard": "1.5.0"},
		"steps": []map[string]any{
			{"id": "50", "optional": false},
			{"id": "60", "optional": false},
		},
	}
	raw, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(candidate, "manifest.json"), raw, 0o644)

	installed := filepath.Join(root, "installed-manifest.json")
	os.WriteFile(installed, []byte(`{"version":"1.4.0"}`), 0o644)

	selection := filepath.Join(root, "selection.json")
	os.WriteFile(selection, []byte(`{"steps":{"50":true,"60":true}}`), 0o644)

	jobDir := filepath.Join(root, "job")
	os.MkdirAll(jobDir, 0o755)

	return updaterhost.Config{
		CandidateBundleDir:    candidate,
		InstalledManifestPath: installed,
		SelectionPath:         selection,
		JobDir:                jobDir,
	}
}

func TestDescribeReportsTheDashboardHostWithNoConnectionNeeded(t *testing.T) {
	h, err := updaterhost.New(setupNode(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	desc := h.Describe()
	if desc.Host != hostapi.HostDashboard || desc.NeedsConnection {
		t.Fatalf("Describe() = %+v", desc)
	}
}

func TestRunStagesTheJobAndFollowsItToCompletion(t *testing.T) {
	cfg := setupNode(t)
	h, err := updaterhost.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Simulate the updater unit: watch for pending.json, then behave like
	// energy-node-updater.sh would (Task 4 proves the real script; this
	// test proves Host.Run's own staging + tailing contract in isolation).
	go func() {
		for {
			if _, err := os.Stat(filepath.Join(cfg.JobDir, "pending.json")); err == nil {
				break
			}
			time.Sleep(2 * time.Millisecond)
		}
		os.Rename(filepath.Join(cfg.JobDir, "pending.json"), filepath.Join(cfg.JobDir, "current.json"))
		os.WriteFile(filepath.Join(cfg.JobDir, "log"), []byte(
			"1000 ##STEP 50 begin\n1001 pip install\n1002 ##STEP 50 ok\n"+
				"1003 ##STEP 60 begin\n1004 dashboard installed\n1005 ##STEP 60 ok\n"), 0o644)
		os.WriteFile(filepath.Join(cfg.JobDir, "status.json"), []byte(`{"result":"ok"}`), 0o644)
	}()

	sink := &recordingSink{}
	err = h.Run(context.Background(), hostapi.RunRequest{Mode: hostapi.ModeRedeploy}, sink)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.markers) != 4 {
		t.Fatalf("markers = %v", sink.markers)
	}
}

func TestRunReturnsTheHostapiErrorFromAFailedStatus(t *testing.T) {
	cfg := setupNode(t)
	h, err := updaterhost.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	go func() {
		for {
			if _, err := os.Stat(filepath.Join(cfg.JobDir, "pending.json")); err == nil {
				break
			}
			time.Sleep(2 * time.Millisecond)
		}
		os.Rename(filepath.Join(cfg.JobDir, "pending.json"), filepath.Join(cfg.JobDir, "current.json"))
		os.WriteFile(filepath.Join(cfg.JobDir, "log"), []byte("1000 ##STEP 60 fail DASHBOARD_START_FAILED\n"), 0o644)
		os.WriteFile(filepath.Join(cfg.JobDir, "status.json"), []byte(`{"result":"fail","step":"60","code":"DASHBOARD_START_FAILED"}`), 0o644)
	}()

	err = h.Run(context.Background(), hostapi.RunRequest{Mode: hostapi.ModeRedeploy}, &recordingSink{})
	var typed *hostapi.Error
	if err == nil {
		t.Fatal("expected an error")
	}
	if !asHostapiError(err, &typed) || typed.Code != "DASHBOARD_START_FAILED" {
		t.Fatalf("err = %v", err)
	}
}

func TestDescribeAdvertisesAutoPrepareOnlyWithAPrepareFunc(t *testing.T) {
	cfg := setupNode(t)
	host, _ := updaterhost.New(cfg)
	if host.Describe().AutoPrepare {
		t.Errorf("AutoPrepare without Config.Prepare")
	}
	cfg.Prepare = func(context.Context, func(string, map[string]string)) error { return nil }
	host, _ = updaterhost.New(cfg)
	if !host.Describe().AutoPrepare {
		t.Errorf("AutoPrepare must be true when Config.Prepare is set")
	}
}

func TestRunPrepareStreamsTheFetchLogAndMarksThePackageStep(t *testing.T) {
	cfg := setupNode(t)
	cfg.Prepare = func(_ context.Context, note func(string, map[string]string)) error {
		note("package.log.extract", nil)
		return nil
	}
	host, _ := updaterhost.New(cfg)
	sink := &recordingSink{}
	if err := host.Run(context.Background(), hostapi.RunRequest{Mode: hostapi.ModePrepare}, sink); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Join(sink.markers, ",") != "package begin,package ok" {
		t.Errorf("markers = %v", sink.markers)
	}
	if len(sink.logs) != 1 || sink.logs[0] != "package.log.extract" {
		t.Errorf("logs = %v, want the note as a keyed message", sink.logs)
	}
}

func TestRunPrepareKeepsATypedErrorAndWrapsAnUntypedOne(t *testing.T) {
	cfg := setupNode(t)
	host, _ := updaterhost.New(cfg)

	cfg.Prepare = func(context.Context, func(string, map[string]string)) error {
		return &hostapi.Error{Code: "GITHUB_UNREACHABLE", Detail: "HTTP 403"}
	}
	host, _ = updaterhost.New(cfg)
	err := host.Run(context.Background(), hostapi.RunRequest{Mode: hostapi.ModePrepare}, &recordingSink{})
	var typed *hostapi.Error
	if !asHostapiError(err, &typed) || typed.Code != "GITHUB_UNREACHABLE" {
		t.Fatalf("err = %v, want the typed GITHUB_UNREACHABLE", err)
	}

	cfg.Prepare = func(context.Context, func(string, map[string]string)) error { return errors.New("boom") }
	host, _ = updaterhost.New(cfg)
	sink := &recordingSink{}
	err = host.Run(context.Background(), hostapi.RunRequest{Mode: hostapi.ModePrepare}, sink)
	if !asHostapiError(err, &typed) || typed.Code != "PREPARE_FAILED" || typed.Detail != "boom" {
		t.Fatalf("err = %v, want PREPARE_FAILED with detail boom", err)
	}
	if sink.markers[len(sink.markers)-1] != "package fail" {
		t.Errorf("markers = %v, want the run to end with package fail", sink.markers)
	}
}

func TestRunPrepareRefusesWhileAJobIsPendingAndDoesNotFetch(t *testing.T) {
	cfg := setupNode(t)
	called := false
	cfg.Prepare = func(context.Context, func(string, map[string]string)) error { called = true; return nil }
	if err := os.WriteFile(filepath.Join(cfg.JobDir, "pending.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	host, _ := updaterhost.New(cfg)
	err := host.Run(context.Background(), hostapi.RunRequest{Mode: hostapi.ModePrepare}, &recordingSink{})
	var typed *hostapi.Error
	if !asHostapiError(err, &typed) || typed.Code != "JOB_IN_PROGRESS" {
		t.Fatalf("err = %v, want JOB_IN_PROGRESS", err)
	}
	if called {
		t.Errorf("Prepare must not run while a job is pending")
	}
}

func TestRunPrepareWithoutAPrepareFuncIsNotSupported(t *testing.T) {
	host, _ := updaterhost.New(setupNode(t))
	err := host.Run(context.Background(), hostapi.RunRequest{Mode: hostapi.ModePrepare}, &recordingSink{})
	var typed *hostapi.Error
	if !asHostapiError(err, &typed) || typed.Code != "NOT_SUPPORTED" {
		t.Fatalf("err = %v, want NOT_SUPPORTED", err)
	}
}

func asHostapiError(err error, target **hostapi.Error) bool {
	if e, ok := err.(*hostapi.Error); ok {
		*target = e
		return true
	}
	return false
}

// writeServiceNode replaces setupNode's manifests with a bundle that carries
// a device service, a plain service and a library, and an installed
// manifest from the previous package.
func writeServiceNode(t *testing.T, cfg updaterhost.Config) {
	t.Helper()
	manifest := map[string]any{
		"version": "1.5.0", "arch": "armv6",
		"components": map[string]string{"dashboard": "1.5.0", "bootstrap": "0.2.0", "energy_node_common": "0.4.7"},
		"steps": []map[string]any{
			{"id": "60", "optional": false},
			{"id": "83", "optional": true, "default": true, "service_id": "shelly", "kind": "device",
				"dir": "shelly", "unit": "shelly-rpc.service", "version": "0.3.0"},
			{"id": "88", "optional": true, "default": true, "service_id": "automation", "kind": "service",
				"dir": "automation", "unit": "automation.service", "version": "0.1.0"},
		},
	}
	raw, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(cfg.CandidateBundleDir, "manifest.json"), raw, 0o644)
	os.WriteFile(cfg.InstalledManifestPath, []byte(`{"version":"1.4.0",
		"components":{"dashboard":"1.4.0","bootstrap":"0.1.9"},
		"steps":[{"id":"83","version":"0.2.0"}]}`), 0o644)
	os.WriteFile(cfg.SelectionPath, []byte(`{"steps":{"83":true,"88":true}}`), 0o644)
}

func TestManifestCarriesTheServiceFieldsOfEachStep(t *testing.T) {
	cfg := setupNode(t)
	writeServiceNode(t, cfg)
	host, _ := updaterhost.New(cfg)
	view, err := host.Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []hostapi.StepView{
		{ID: "60"},
		{ID: "83", ServiceID: "shelly", Kind: "device", Dir: "shelly", Unit: "shelly-rpc.service", Optional: true, Default: true},
		{ID: "88", ServiceID: "automation", Kind: "service", Dir: "automation", Unit: "automation.service", Optional: true, Default: true},
	}
	if len(view.Steps) != len(want) {
		t.Fatalf("steps = %+v, want %+v", view.Steps, want)
	}
	for i := range want {
		if view.Steps[i] != want[i] {
			t.Errorf("step %d = %+v, want %+v", i, view.Steps[i], want[i])
		}
	}
}

func TestPlanTakesComponentFromVersionsFromTheInstalledManifest(t *testing.T) {
	cfg := setupNode(t)
	writeServiceNode(t, cfg)
	host, _ := updaterhost.New(cfg)
	view, err := host.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"dashboard": "1.4.0", "bootstrap": "0.1.9"} {
		got := view.Components[name]
		if got.From == nil || *got.From != want {
			t.Errorf("%s from = %v, want %s", name, got.From, want)
		}
	}
	if from := view.Components["energy_node_common"].From; from != nil {
		t.Errorf("energy_node_common from = %q, want nil (not in the installed manifest)", *from)
	}
}

func TestPlanLeavesComponentFromNilWithoutAnInstalledManifest(t *testing.T) {
	cfg := setupNode(t)
	writeServiceNode(t, cfg)
	os.Remove(cfg.InstalledManifestPath)
	host, _ := updaterhost.New(cfg)
	view, err := host.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if from := view.Components["dashboard"].From; from != nil {
		t.Errorf("dashboard from = %q, want nil", *from)
	}
}

func TestPlanStepsCarryUnitAndVersions(t *testing.T) {
	cfg := setupNode(t)
	writeServiceNode(t, cfg)
	host, _ := updaterhost.New(cfg)
	view, err := host.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]hostapi.PlanStep{}
	for _, s := range view.Steps {
		byID[s.ID] = s
	}
	if s := byID["83"]; s.Unit != "shelly-rpc.service" || s.From != "0.2.0" || s.To != "0.3.0" {
		t.Errorf("step 83 = %+v, want unit shelly-rpc.service, 0.2.0 -> 0.3.0", s)
	}
	if s := byID["88"]; s.Unit != "automation.service" || s.From != "" || s.To != "0.1.0" {
		t.Errorf("step 88 = %+v, want unit automation.service, no from, to 0.1.0", s)
	}
}

func TestRunWritesTheRunIDIntoTheJob(t *testing.T) {
	cfg := setupNode(t)
	h, _ := updaterhost.New(cfg)
	go func() {
		for {
			if _, err := os.Stat(filepath.Join(cfg.JobDir, "pending.json")); err == nil {
				break
			}
			time.Sleep(2 * time.Millisecond)
		}
		os.Rename(filepath.Join(cfg.JobDir, "pending.json"), filepath.Join(cfg.JobDir, "current.json"))
		os.WriteFile(filepath.Join(cfg.JobDir, "status.json"), []byte(`{"result":"ok"}`), 0o644)
	}()

	if err := h.Run(context.Background(), hostapi.RunRequest{Mode: hostapi.ModeRedeploy, RunID: "run-5"}, &recordingSink{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(cfg.JobDir, "current.json"))
	if !strings.Contains(string(raw), `"run_id":"run-5"`) {
		t.Fatalf("current.json = %s, want the run id", raw)
	}
}

func writeManifests(t *testing.T, cfg updaterhost.Config, candidate, installed string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(cfg.CandidateBundleDir, "manifest.json"), []byte(candidate), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.InstalledManifestPath, []byte(installed), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPlanReportsTheInstalledVersionAsFrom(t *testing.T) {
	cfg := setupNode(t)
	writeManifests(t, cfg,
		`{"version":"v0.7.5","arch":"armv6","components":{"dashboard":"v0.7.5","services":"v0.4.0","bootstrap":"v0.1.6"},"steps":[{"id":"60","optional":false}]}`,
		`{"version":"v0.7.4","components":{"dashboard":"v0.7.4","services":"v0.4.0"}}`)
	h, err := updaterhost.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := h.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dashboard := plan.Components["dashboard"]
	if dashboard.From == nil || *dashboard.From != "v0.7.4" || dashboard.To != "v0.7.5" {
		t.Fatalf("dashboard = %+v, want from v0.7.4 to v0.7.5", dashboard)
	}
	if services := plan.Components["services"]; services.From == nil || *services.From != "v0.4.0" {
		t.Fatalf("an unchanged component still has a from: %+v", services)
	}
	if bootstrap := plan.Components["bootstrap"]; bootstrap.From != nil {
		t.Fatalf("a component the installed manifest does not list must have no from (null = new): %+v", bootstrap)
	}
}

func TestPlanWithoutAnInstalledManifestHasNoFrom(t *testing.T) {
	cfg := setupNode(t)
	if err := os.Remove(cfg.InstalledManifestPath); err != nil {
		t.Fatal(err)
	}
	h, _ := updaterhost.New(cfg)
	plan, err := h.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Components["dashboard"].From != nil {
		t.Fatalf("from = %v, want nil", *plan.Components["dashboard"].From)
	}
}

func TestChangelogReadsTheCandidateAndTheInstalledManifest(t *testing.T) {
	cfg := setupNode(t)
	writeManifests(t, cfg,
		`{"version":"v0.7.5","arch":"armv6","components":{"dashboard":"v0.7.5"},"steps":[]}`,
		`{"version":"v0.7.4","components":{"dashboard":"v0.7.4"},"steps":[{"id":"83","dir":"shelly","version":"v0.4.0"}]}`)
	const document = `{"schema_version":1,"components":[{"id":"dashboard"}]}`
	if err := os.WriteFile(filepath.Join(cfg.CandidateBundleDir, "changelog.json"), []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
	h, _ := updaterhost.New(cfg)
	view, err := h.Changelog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.BundleVersion != "v0.7.5" || string(view.Document) != document {
		t.Fatalf("view = %+v", view)
	}
	if view.Installed["dashboard"] != "v0.7.4" || view.Installed["service:shelly"] != "v0.4.0" {
		t.Fatalf("installed = %v, want the components and the service version", view.Installed)
	}
}

func TestChangelogWithoutAnInstalledManifestHasNothingInstalled(t *testing.T) {
	cfg := setupNode(t)
	if err := os.Remove(cfg.InstalledManifestPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.CandidateBundleDir, "changelog.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	h, _ := updaterhost.New(cfg)
	view, err := h.Changelog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.Installed == nil || len(view.Installed) != 0 {
		t.Fatalf("installed = %#v, want an empty non-nil map", view.Installed)
	}
}

func TestChangelogOfACandidateWithoutOneIsNoChangelog(t *testing.T) {
	h, _ := updaterhost.New(setupNode(t))
	_, err := h.Changelog(context.Background())
	var apiErr *hostapi.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "NO_CHANGELOG" {
		t.Fatalf("err = %#v, want NO_CHANGELOG", err)
	}
}

// Ein Systemschritt, den die Auswahl auf dem Node noch nicht kennt (15 kam
// mit einem Update), gilt mit seiner Manifest-Vorgabe, wie in step.sh,
// plan.sh und der Oberflaeche. Ein Dienst ohne Schluessel bleibt aus.
func TestASystemStepMissingFromTheSelectionFollowsItsDefault(t *testing.T) {
	cfg := setupNode(t)
	writeManifests(t, cfg,
		`{"version":"1.5.0","arch":"armv6","steps":[
			{"id":"10","optional":false},
			{"id":"15","optional":true,"default":true},
			{"id":"35","optional":true,"default":false},
			{"id":"89","optional":true,"default":true,"service_id":"modbus"}]}`,
		`{"version":"1.4.0"}`)
	os.WriteFile(cfg.SelectionPath, []byte(`{"steps":{"10":true}}`), 0o644)
	h, _ := updaterhost.New(cfg)

	view, err := h.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]hostapi.PlanStep{}
	for _, s := range view.Steps {
		byID[s.ID] = s
	}
	if s := byID["15"]; !s.Selected || s.State != "pending" {
		t.Errorf("step 15 = %+v, want selected and pending", s)
	}
	for _, id := range []string{"35", "89"} {
		if s := byID[id]; s.Selected || s.State != "deselected" {
			t.Errorf("step %s = %+v, want deselected", id, s)
		}
	}

	go func() {
		for {
			if _, err := os.Stat(filepath.Join(cfg.JobDir, "pending.json")); err == nil {
				break
			}
			time.Sleep(2 * time.Millisecond)
		}
		os.Rename(filepath.Join(cfg.JobDir, "pending.json"), filepath.Join(cfg.JobDir, "current.json"))
		os.WriteFile(filepath.Join(cfg.JobDir, "status.json"), []byte(`{"result":"ok"}`), 0o644)
	}()
	if err := h.Run(context.Background(), hostapi.RunRequest{Mode: hostapi.ModeRedeploy, RunID: "run-6"}, &recordingSink{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(cfg.JobDir, "current.json"))
	if !strings.Contains(string(raw), `"steps":["10","15"]`) {
		t.Fatalf("current.json = %s, want steps 10 and 15", raw)
	}
}

// Die Systempakete zaehlt das Dashboard auf Abfrage mit demselben Skript wie
// der Installer: bootstrap/lib/apt_pending.py aus dem Kandidaten-Bundle. Die
// Vorschau (Plan) wartet nicht darauf.
func TestSystemUpdatesCountWithTheCandidateHelperOnlyWhenAsked(t *testing.T) {
	cfg := setupNode(t)
	lib := filepath.Join(cfg.CandidateBundleDir, "bootstrap", "lib")
	os.MkdirAll(lib, 0o755)
	os.WriteFile(filepath.Join(lib, "apt_pending.py"), []byte(`print('{"count": 2, "checked_at": "2026-10-04T06:12:00+00:00", "packages": [{"name": "libssl3", "from": "3.0.11", "to": "3.0.13"}, {"name": "openssl", "from": "3.0.11", "to": "3.0.13"}]}')
`), 0o644)
	h, _ := updaterhost.New(cfg)
	if _, err := h.Plan(context.Background()); err != nil {
		t.Fatal(err)
	}
	u, err := h.SystemUpdates(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if u == nil || u.Count != 2 || u.CheckedAt != "2026-10-04T06:12:00+00:00" || len(u.Packages) != 2 || u.Packages[1].Name != "openssl" {
		t.Fatalf("system updates = %+v", u)
	}
}

func TestSystemUpdatesWithoutAWorkingHelperAreUnknown(t *testing.T) {
	for name, script := range map[string]string{
		"missing": "",
		"null":    "print('null')\n",
		"broken":  "raise SystemExit(3)\n",
		"garbage": "print('kein json')\n",
	} {
		cfg := setupNode(t)
		if script != "" {
			lib := filepath.Join(cfg.CandidateBundleDir, "bootstrap", "lib")
			os.MkdirAll(lib, 0o755)
			os.WriteFile(filepath.Join(lib, "apt_pending.py"), []byte(script), 0o644)
		}
		h, _ := updaterhost.New(cfg)
		u, err := h.SystemUpdates(context.Background(), false)
		if err != nil {
			t.Fatalf("%s: must not fail: %v", name, err)
		}
		if u != nil {
			t.Errorf("%s: system updates = %+v, want nil", name, u)
		}
	}
}

// Frisch abrufen braucht root (apt-get update); das Dashboard laeuft als
// Dienstbenutzer und bietet es nicht an.
func TestSystemUpdatesRefreshIsNotSupportedOnTheDashboard(t *testing.T) {
	h, _ := updaterhost.New(setupNode(t))
	_, err := h.SystemUpdates(context.Background(), true)
	var apiErr *hostapi.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "NOT_SUPPORTED" || apiErr.Status != http.StatusNotImplemented {
		t.Fatalf("err = %#v, want NOT_SUPPORTED 501", err)
	}
}
