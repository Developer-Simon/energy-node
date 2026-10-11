package updateprogress

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const job = `{"bundle_version":"v0.9.1","mode":"redeploy","steps":["10","20","50","60"],"run_id":"r1"}`

func logOf(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

func TestReadEmptyDirIsIdle(t *testing.T) {
	p, err := Read(t.TempDir())
	if err != nil || p.State != "idle" {
		t.Fatalf("p = %+v, err = %v", p, err)
	}
}

func TestReadMissingDirIsIdle(t *testing.T) {
	p, err := Read(filepath.Join(t.TempDir(), "absent"))
	if err != nil || p.State != "idle" {
		t.Fatalf("p = %+v, err = %v", p, err)
	}
}

func TestReadPendingJob(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "pending.json", job)
	p, err := Read(dir)
	if err != nil || p.State != "pending" || p.Version != "v0.9.1" || p.Total != 4 || p.Done != 0 || p.RunID != "r1" {
		t.Fatalf("p = %+v, err = %v", p, err)
	}
}

func TestReadRunningInsideAStep(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "current.json", job)
	write(t, dir, "log", logOf(
		"1000 ##STEP 10 begin", "1001 ##STEP 10 skip already done",
		"1002 ##STEP 20 begin", "1003 ##STEP 20 ok",
		"1004 ##STEP 50 begin", "1005 pip says hello"))
	p, err := Read(dir)
	if err != nil || p.State != "running" || p.Done != 2 || p.Step == nil || p.Step.ID != "50" {
		t.Fatalf("p = %+v (step %+v), err = %v", p, p.Step, err)
	}
}

func TestReadRunningBetweenStepsNamesTheNextOne(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "current.json", job)
	write(t, dir, "log", logOf("1000 ##STEP 10 begin", "1001 ##STEP 10 ok"))
	p, _ := Read(dir)
	if p.State != "running" || p.Done != 1 || p.Step == nil || p.Step.ID != "20" {
		t.Fatalf("p = %+v (step %+v)", p, p.Step)
	}
}

func TestReadRunningBeforeTheFirstMarker(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "current.json", job)
	p, _ := Read(dir)
	if p.State != "running" || p.Done != 0 || p.Step == nil || p.Step.ID != "10" {
		t.Fatalf("p = %+v (step %+v)", p, p.Step)
	}
}

func TestReadFinishedOk(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "current.json", job)
	write(t, dir, "log", logOf("1000 ##STEP 10 ok"))
	write(t, dir, "status.json", `{"result":"ok"}`)
	p, _ := Read(dir)
	if p.State != "ok" || p.Done != 4 || p.FinishedAt == nil || p.Step != nil {
		t.Fatalf("p = %+v", p)
	}
}

func TestReadFailedStepAndCode(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "current.json", job)
	write(t, dir, "log", logOf("1000 ##STEP 10 ok", "1001 ##STEP 20 begin", "1002 ##STEP 20 fail MOSQUITTO_RESTART_FAILED"))
	write(t, dir, "status.json", `{"result":"fail","step":"20","code":"MOSQUITTO_RESTART_FAILED"}`)
	p, _ := Read(dir)
	if p.State != "fail" || p.Step == nil || p.Step.ID != "20" || p.Code != "MOSQUITTO_RESTART_FAILED" || p.Done != 1 {
		t.Fatalf("p = %+v (step %+v)", p, p.Step)
	}
}

func TestReadRejectedBundle(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "current.json", job)
	write(t, dir, "status.json", `{"result":"rejected","code":"BUNDLE_SIGNATURE_INVALID"}`)
	p, _ := Read(dir)
	if p.State != "rejected" || p.Code != "BUNDLE_SIGNATURE_INVALID" || p.Step != nil {
		t.Fatalf("p = %+v", p)
	}
}

func TestReadInterruptedRunIsFail(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "current.json", job)
	write(t, dir, "log", logOf("1000 ##STEP 10 ok", "1001 ##STEP 20 begin", "1002 FAIL UPDATER_INTERRUPTED"))
	write(t, dir, "status.json", `{"result":"fail","step":"","code":"UPDATER_INTERRUPTED"}`)
	p, _ := Read(dir)
	if p.State != "fail" || p.Code != "UPDATER_INTERRUPTED" || p.Step == nil || p.Step.ID != "20" {
		t.Fatalf("p = %+v (step %+v), want fail at the step that was running", p, p.Step)
	}
}

func TestReadUnknownResultCountsAsFail(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "current.json", job)
	write(t, dir, "status.json", `{"result":"weird"}`)
	p, _ := Read(dir)
	if p.State != "fail" {
		t.Fatalf("p = %+v", p)
	}
}

func TestReadBrokenStatusIsAnError(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "current.json", job)
	write(t, dir, "status.json", `{`)
	if _, err := Read(dir); err == nil {
		t.Fatal("want an error for an unreadable status.json")
	}
}
