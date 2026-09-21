package updaterjob_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/updaterjob"
)

func TestStageWritesJobJSONAndPendingTrigger(t *testing.T) {
	dir := t.TempDir()
	bundle := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundle, "manifest.json"), []byte(`{"version":"1.5.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	job := updaterjob.Job{BundleVersion: "1.5.0", Mode: "redeploy", Steps: []string{"50", "60"}}

	if err := updaterjob.Stage(dir, job, bundle); err != nil {
		t.Fatalf("Stage: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "pending.json")); err != nil {
		t.Fatalf("pending.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bundle", "manifest.json")); err != nil {
		t.Fatalf("bundle not copied: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "pending.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(raw), `"bundle_version":"1.5.0"`) || !contains(string(raw), `"60"`) {
		t.Fatalf("pending.json = %s", raw)
	}
	// target_user/target_base must never travel in this file: it is
	// dashboard-writable and unsigned, and both values steer root-run
	// mkdir/install/chown inside 60-node-install.sh.
	if contains(string(raw), "target_user") || contains(string(raw), "target_base") {
		t.Fatalf("pending.json carries a target the signature does not cover: %s", raw)
	}
}

func TestStageRefusesAConcurrentJob(t *testing.T) {
	dir := t.TempDir()
	bundle := t.TempDir()
	os.WriteFile(filepath.Join(bundle, "manifest.json"), []byte(`{}`), 0o644)
	job := updaterjob.Job{BundleVersion: "1.0.0"}
	if err := updaterjob.Stage(dir, job, bundle); err != nil {
		t.Fatal(err)
	}
	if err := updaterjob.Stage(dir, job, bundle); err == nil {
		t.Fatal("expected the second Stage to fail while pending.json still exists")
	}
}

func TestStageProceedsWhenAPreviousJobHasFinished(t *testing.T) {
	dir := t.TempDir()
	bundle := t.TempDir()
	os.WriteFile(filepath.Join(bundle, "manifest.json"), []byte(`{}`), 0o644)
	os.WriteFile(filepath.Join(dir, "current.json"), []byte(`{}`), 0o644)
	os.WriteFile(filepath.Join(dir, "status.json"), []byte(`{"result":"ok"}`), 0o644)
	job := updaterjob.Job{BundleVersion: "2.0.0"}
	if err := updaterjob.Stage(dir, job, bundle); err != nil {
		t.Fatalf("Stage should proceed once the previous job finished (InFlight()==false), got: %v", err)
	}
}

func TestReadStatusReportsNotYetFinished(t *testing.T) {
	dir := t.TempDir()
	status, done, err := updaterjob.ReadStatus(dir)
	if err != nil || done || status != nil {
		t.Fatalf("ReadStatus on an empty dir = %+v, %v, %v", status, done, err)
	}
}

func TestReadStatusParsesAFinishedJob(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "status.json"), []byte(`{"result":"fail","step":"60","code":"DASHBOARD_START_FAILED"}`), 0o644)
	status, done, err := updaterjob.ReadStatus(dir)
	if err != nil || !done {
		t.Fatalf("ReadStatus: %+v, %v, %v", status, done, err)
	}
	if status.Result != "fail" || status.Step != "60" || status.Code != "DASHBOARD_START_FAILED" {
		t.Fatalf("status = %+v", status)
	}
}

func TestInFlightIsTrueOnlyBetweenCurrentAndStatus(t *testing.T) {
	dir := t.TempDir()
	if updaterjob.InFlight(dir) {
		t.Fatal("empty dir must not be in flight")
	}
	os.WriteFile(filepath.Join(dir, "current.json"), []byte(`{}`), 0o644)
	if !updaterjob.InFlight(dir) {
		t.Fatal("current.json without status.json must be in flight")
	}
	os.WriteFile(filepath.Join(dir, "status.json"), []byte(`{"result":"ok"}`), 0o644)
	if updaterjob.InFlight(dir) {
		t.Fatal("a finished job must not be in flight")
	}
}

func TestPendingIsTrueOnlyWhileAJobWaitsForTheUpdater(t *testing.T) {
	dir := t.TempDir()
	if updaterjob.Pending(dir) {
		t.Fatalf("empty dir must not be pending")
	}
	if err := os.WriteFile(filepath.Join(dir, "pending.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !updaterjob.Pending(dir) {
		t.Fatalf("pending.json present, Pending must be true")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || len(needle) == 0 ||
		func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		}())
}
