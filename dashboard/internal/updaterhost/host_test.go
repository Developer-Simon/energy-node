// dashboard/internal/updaterhost/host_test.go
package updaterhost_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

func asHostapiError(err error, target **hostapi.Error) bool {
	if e, ok := err.(*hostapi.Error); ok {
		*target = e
		return true
	}
	return false
}
