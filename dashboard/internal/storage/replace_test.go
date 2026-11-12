package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const replaceNewUUID = "66666666-2222-3333-4444-555555555555"

// replaceFixture models the one mount point of the helper: a drive mounted
// there is moved to a stash when it is unmounted, and a second mount is refused
// while the point is busy.
type replaceFixture struct {
	t        *testing.T
	root     string
	dataDir  string
	mount    string
	runtime  string
	fallback string
	svc      *Service

	stash   map[string]string
	mounted string
	calls   []string

	stopped   int
	started   int
	restarted int
	hookFn    func(name string) error
}

func newReplaceFixture(t *testing.T) *replaceFixture {
	t.Helper()
	root := t.TempDir()
	f := &replaceFixture{
		t:        t,
		root:     root,
		dataDir:  filepath.Join(root, "data"),
		mount:    filepath.Join(root, "mnt", "energy-node-data"),
		runtime:  filepath.Join(root, "data", "runtime"),
		fallback: filepath.Join(root, "data", "runtime-fallback"),
		stash:    map[string]string{},
	}
	f.svc = &Service{
		Paths: Paths{
			Runtime:        f.runtime,
			Mount:          f.mount,
			SystemFallback: f.fallback,
			RAMFallback:    filepath.Join(root, "ram"),
		},
		DataDir: f.dataDir,
		NodeID:  testNodeID,
		Deps: Deps{
			Lister: fakeLister{drives: []Drive{
				{Kname: "sdb", Name: "USB", Size: 64 << 30, NeedsFormat: true, Contents: ContentsNone},
				{Kname: "sdc", Name: "Old", UUID: testUUID, FSType: "ext4", Ready: true, Size: 32 << 30},
			}},
			Storage: f.storage,
			StopServices: func() error {
				f.stopped++
				return nil
			},
			StartServices: func() error {
				f.started++
				return nil
			},
			RestartDashboard: func(ctx context.Context) error {
				f.restarted++
				return nil
			},
			Step: func(name string) error {
				if f.hookFn != nil {
					return f.hookFn(name)
				}
				return nil
			},
		},
	}
	return f
}

// newReplaceOkFixture puts the old drive in the mount point with its data and
// points the runtime path at the drive, as a finished setup does.
func newReplaceOkFixture(t *testing.T) *replaceFixture {
	t.Helper()
	f := newReplaceFixture(t)
	writeTestFile(t, filepath.Join(f.mount, markerName), testNodeID+"\n")
	writeTestFile(t, filepath.Join(f.mount, driveRuntimeName, "a.json"), "old-alpha")
	writeTestFile(t, filepath.Join(f.mount, driveRuntimeName, "sub", "b.bin"), "old-beta")
	writeTestFile(t, filepath.Join(f.mount, "backups", "20261108-120000-manual.tar.gz"), "old-archive")
	f.mounted = testUUID
	f.saveOldConfig()
	if err := os.Symlink(DriveRuntimeDir(f.svc.Paths), f.runtime); err != nil {
		t.Fatal(err)
	}
	return f
}

// newReplaceMissingFixture has no old drive: the runtime data runs on the
// system disk, and the backups are the system copy.
func newReplaceMissingFixture(t *testing.T, systemBackups bool) *replaceFixture {
	t.Helper()
	f := newReplaceFixture(t)
	f.svc.Deps.Lister = fakeLister{drives: []Drive{
		{Kname: "sdb", Name: "USB", Size: 64 << 30, NeedsFormat: true, Contents: ContentsNone},
	}}
	writeTestFile(t, filepath.Join(f.fallback, "a.json"), "fallback-alpha")
	if systemBackups {
		writeTestFile(t, filepath.Join(f.dataDir, "backups", "20261108-120000-manual.tar.gz"), "sys-archive")
	}
	f.saveOldConfig()
	if err := os.Symlink(f.fallback, f.runtime); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *replaceFixture) saveOldConfig() {
	f.t.Helper()
	if err := SaveConfig(f.dataDir, Config{UUID: testUUID, Name: "Old", Fallback: FallbackSystem, UseData: true, UseBackups: true}); err != nil {
		f.t.Fatal(err)
	}
}

// storage stands in for the root helper with the one-mount-point behaviour.
func (f *replaceFixture) storage(ctx context.Context, verb, arg string) (string, error) {
	f.calls = append(f.calls, verb)
	switch verb {
	case "format":
		if f.mounted != "" {
			return "", errors.New("device is mounted")
		}
		return replaceNewUUID, nil
	case "mount":
		if f.mounted != "" {
			return "", errors.New("mount point busy")
		}
		if dir, ok := f.stash[arg]; ok {
			if err := os.Rename(dir, f.mount); err != nil {
				return "", err
			}
			delete(f.stash, arg)
		} else {
			writeTestFile(f.t, filepath.Join(f.mount, markerName), testNodeID+"\n")
			if err := os.MkdirAll(filepath.Join(f.mount, driveRuntimeName), 0o755); err != nil {
				return "", err
			}
			if err := os.MkdirAll(filepath.Join(f.mount, "backups"), 0o755); err != nil {
				return "", err
			}
		}
		f.mounted = arg
	case "unmount":
		if f.mounted == "" {
			return "", nil
		}
		dir := filepath.Join(f.root, "stash", f.mounted)
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return "", err
		}
		if err := os.Rename(f.mount, dir); err != nil {
			return "", err
		}
		f.stash[f.mounted] = dir
		f.mounted = ""
	}
	return "", nil
}

// assertOldSetupIntact checks that an aborted replace left the old setup as it was.
func (f *replaceFixture) assertOldSetupIntact(before []byte) {
	f.t.Helper()
	after, err := os.ReadFile(filepath.Join(f.dataDir, configFile))
	if err != nil || !bytes.Equal(before, after) {
		f.t.Fatalf("storage.json changed by an aborted replace: %v\n%s", err, after)
	}
	if target, err := os.Readlink(f.runtime); err != nil || target != DriveRuntimeDir(f.svc.Paths) {
		f.t.Fatalf("runtime symlink = %q (%v), want the old drive runtime", target, err)
	}
	if f.mounted != testUUID {
		f.t.Fatalf("old drive mounted = %q after the abort, want %q", f.mounted, testUUID)
	}
	if got := readTestFile(f.t, filepath.Join(f.mount, driveRuntimeName, "a.json")); got != "old-alpha" {
		f.t.Fatalf("old drive a.json = %q after the abort", got)
	}
	for _, p := range []string{filepath.Join(f.dataDir, replaceStagingName), filepath.Join(f.dataDir, pendingFile)} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			f.t.Fatalf("%s left after an abort: %v", p, err)
		}
	}
	if f.started != f.stopped {
		f.t.Fatalf("services stopped %d times but started %d times", f.stopped, f.started)
	}
}

func TestReplaceFromOkDrive(t *testing.T) {
	f := newReplaceOkFixture(t)
	ctx := context.Background()
	req := ReplaceRequest{Kname: "sdb", Format: true, UseData: true, UseBackups: true, Fallback: FallbackSystem}

	if err := f.svc.Replace(ctx, req); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	assertJob(t, f.svc, "")
	if f.mounted != replaceNewUUID {
		t.Fatalf("mounted = %q, want the new drive", f.mounted)
	}
	if target, err := os.Readlink(f.runtime); err != nil || target != DriveRuntimeDir(f.svc.Paths) {
		t.Fatalf("runtime symlink = %q (%v)", target, err)
	}
	if got := readTestFile(t, filepath.Join(f.mount, driveRuntimeName, "a.json")); got != "old-alpha" {
		t.Fatalf("new drive a.json = %q, want the old data", got)
	}
	if got := readTestFile(t, filepath.Join(f.mount, driveRuntimeName, "sub", "b.bin")); got != "old-beta" {
		t.Fatalf("new drive sub/b.bin = %q", got)
	}
	if got := readTestFile(t, filepath.Join(f.mount, "backups", "20261108-120000-manual.tar.gz")); got != "old-archive" {
		t.Fatalf("new drive archive = %q", got)
	}
	cfg, err := LoadConfig(f.dataDir)
	if err != nil || cfg.UUID != replaceNewUUID || cfg.BackupsOld != "" || cfg.ReplaceStaging != filepath.Join(f.dataDir, replaceStagingName) {
		t.Fatalf("config = %+v, %v", cfg, err)
	}
	if kind, ok, _ := readPending(f.dataDir); !ok || kind != pendingSetup {
		t.Fatalf("pending marker = %q, %v", kind, ok)
	}
	if f.restarted != 1 || f.stopped != 1 || f.started != 0 {
		t.Fatalf("restarted %d, stopped %d, started %d; want 1, 1, 0", f.restarted, f.stopped, f.started)
	}
	// The staging stays until the green restart, the old stick is not wiped.
	if got := readTestFile(t, filepath.Join(f.dataDir, replaceStagingName, "runtime", "a.json")); got != "old-alpha" {
		t.Fatalf("staging a.json = %q", got)
	}

	if ran, err := f.svc.Resume(ctx); err != nil || !ran {
		t.Fatalf("Resume = %v, %v", ran, err)
	}
	if _, err := os.Lstat(filepath.Join(f.dataDir, replaceStagingName)); !os.IsNotExist(err) {
		t.Fatalf("Resume kept the staging: %v", err)
	}
	if got := readTestFile(t, filepath.Join(f.stash[testUUID], driveRuntimeName, "a.json")); got != "old-alpha" {
		t.Fatalf("old stick wiped: a.json = %q", got)
	}
	if f.started != 1 {
		t.Fatalf("services started %d times, want 1", f.started)
	}
}

func TestReplaceFromMissingDriveUsesFallback(t *testing.T) {
	cases := []struct {
		name        string
		sysBackups  bool
		wantWarning string
	}{
		{name: "system backups kept", sysBackups: true},
		{name: "backups lost with the drive", sysBackups: false, wantWarning: CodeBackupsLost},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newReplaceMissingFixture(t, tc.sysBackups)
			req := ReplaceRequest{Kname: "sdb", Format: true, UseData: true, UseBackups: true, Fallback: FallbackSystem}

			if err := f.svc.Replace(context.Background(), req); err != nil {
				t.Fatalf("Replace: %v", err)
			}
			assertJob(t, f.svc, "")
			if got := f.svc.Job().Warn; got != tc.wantWarning {
				t.Fatalf("job warning = %q, want %q", got, tc.wantWarning)
			}
			if contains(f.calls, "unmount") {
				t.Fatalf("unmount called without an old drive: %v", f.calls)
			}
			if got := readTestFile(t, filepath.Join(f.mount, driveRuntimeName, "a.json")); got != "fallback-alpha" {
				t.Fatalf("new drive a.json = %q, want the fallback data", got)
			}
			if target, err := os.Readlink(f.runtime); err != nil || target != DriveRuntimeDir(f.svc.Paths) {
				t.Fatalf("runtime symlink = %q (%v)", target, err)
			}
			if tc.sysBackups {
				if got := readTestFile(t, filepath.Join(f.mount, "backups", "20261108-120000-manual.tar.gz")); got != "sys-archive" {
					t.Fatalf("new drive archive = %q", got)
				}
			}
			cfg, err := LoadConfig(f.dataDir)
			if err != nil || cfg.UUID != replaceNewUUID || cfg.ReplaceStaging != "" {
				t.Fatalf("config = %+v, %v", cfg, err)
			}
		})
	}
}

func TestReplaceAbortKeepsOldSetup(t *testing.T) {
	cases := []struct {
		hook     string
		wantCode string
	}{
		{"stop", CodeStopFailed},
		{"stage", CodeCopyFailed},
		{"verify_stage", CodeVerifyFailed},
		{"unmount", CodeUnmountFailed},
		{"format", CodeFormatFailed},
		{"mount", CodeMountFailed},
		{"copy", CodeCopyFailed},
		{"verify", CodeVerifyFailed},
		{"switch", CodeSwitchFailed},
		{"save", CodeSaveFailed},
		{"restart", CodeRestartFailed},
	}
	for _, tc := range cases {
		t.Run(tc.hook, func(t *testing.T) {
			f := newReplaceOkFixture(t)
			before, err := os.ReadFile(filepath.Join(f.dataDir, configFile))
			if err != nil {
				t.Fatal(err)
			}
			f.hookFn = failAt(tc.hook)
			req := ReplaceRequest{Kname: "sdb", Format: true, UseData: true, UseBackups: true, Fallback: FallbackSystem}

			err = f.svc.Replace(context.Background(), req)
			if got := CodeOf(err); got != tc.wantCode {
				t.Fatalf("Replace error code = %q (%v), want %q", got, err, tc.wantCode)
			}
			assertJob(t, f.svc, tc.wantCode)
			f.assertOldSetupIntact(before)
		})
	}
}

func TestReplaceStagesAndChecksSpace(t *testing.T) {
	t.Run("refuses without space before anything changes", func(t *testing.T) {
		f := newReplaceOkFixture(t)
		before, err := os.ReadFile(filepath.Join(f.dataDir, configFile))
		if err != nil {
			t.Fatal(err)
		}
		f.svc.Deps.FreeSpace = func(string) (uint64, error) { return 1, nil }
		req := ReplaceRequest{Kname: "sdb", Format: true, UseData: true, UseBackups: true, Fallback: FallbackSystem}

		if err := f.svc.Replace(context.Background(), req); CodeOf(err) != CodeNoSpace {
			t.Fatalf("Replace error = %v, want %s", err, CodeNoSpace)
		}
		if len(f.calls) != 0 || f.stopped != 0 {
			t.Fatalf("refused replace still called %v or stopped services", f.calls)
		}
		f.assertOldSetupIntact(before)
	})

	t.Run("copies the old data to the staging before the unmount", func(t *testing.T) {
		f := newReplaceOkFixture(t)
		seen := false
		f.hookFn = func(name string) error {
			if name == "unmount" {
				seen = true
				if f.mounted != testUUID {
					t.Errorf("old drive not mounted when the staging is checked")
				}
				if got := readTestFile(t, filepath.Join(f.dataDir, replaceStagingName, "runtime", "sub", "b.bin")); got != "old-beta" {
					t.Errorf("staging sub/b.bin = %q before the unmount", got)
				}
				if got := readTestFile(t, filepath.Join(f.dataDir, replaceStagingName, "backups", "20261108-120000-manual.tar.gz")); got != "old-archive" {
					t.Errorf("staging archive = %q before the unmount", got)
				}
			}
			return nil
		}
		req := ReplaceRequest{Kname: "sdb", Format: true, UseData: true, UseBackups: true, Fallback: FallbackSystem}
		if err := f.svc.Replace(context.Background(), req); err != nil {
			t.Fatalf("Replace: %v", err)
		}
		if !seen {
			t.Fatalf("unmount step not reached")
		}
	})
}

func TestReplaceRejectsSameDriveAndUnknownKname(t *testing.T) {
	cases := []struct {
		name     string
		req      ReplaceRequest
		wantCode string
	}{
		{name: "same drive", req: ReplaceRequest{Kname: "sdc", UUID: testUUID, UseData: true, Fallback: FallbackSystem}, wantCode: CodeSameDrive},
		{name: "unknown kname", req: ReplaceRequest{Kname: "sdz", Format: true, UseData: true, Fallback: FallbackSystem}, wantCode: CodeUnknownDrive},
		{name: "system disk name", req: ReplaceRequest{Kname: "mmcblk0", Format: true, UseData: true, Fallback: FallbackSystem}, wantCode: CodeUnknownDrive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newReplaceOkFixture(t)
			before, err := os.ReadFile(filepath.Join(f.dataDir, configFile))
			if err != nil {
				t.Fatal(err)
			}
			err = f.svc.Replace(context.Background(), tc.req)
			if got := CodeOf(err); got != tc.wantCode {
				t.Fatalf("Replace error code = %q (%v), want %q", got, err, tc.wantCode)
			}
			if len(f.calls) != 0 || f.stopped != 0 {
				t.Fatalf("rejected replace called %v or stopped services", f.calls)
			}
			f.assertOldSetupIntact(before)
		})
	}
}

// TestStartReplaceRunsInBackgroundAndExposesWarn checks the job the route
// starts: it returns at once, the job ends with done set, and the warning of
// lost backups shows in the job JSON the status API returns.
func TestStartReplaceRunsInBackgroundAndExposesWarn(t *testing.T) {
	f := newReplaceMissingFixture(t, false)
	req := ReplaceRequest{Kname: "sdb", Format: true, UseData: true, UseBackups: true, Fallback: FallbackSystem}

	if err := f.svc.StartReplace(req); err != nil {
		t.Fatalf("StartReplace: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !f.svc.Job().Done {
		if time.Now().After(deadline) {
			t.Fatalf("job did not end: %+v", f.svc.Job())
		}
		time.Sleep(5 * time.Millisecond)
	}
	job := f.svc.Job()
	if job.Err != "" || job.Warn != CodeBackupsLost {
		t.Fatalf("job = %+v, want no error and warning %q", job, CodeBackupsLost)
	}
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"warn":"storage_backups_lost"`) {
		t.Fatalf("job JSON lacks the warning: %s", raw)
	}
}

// TestStartReplaceRejectsAtOnce checks that a rejected request returns its
// code without touching the helper or the services.
func TestStartReplaceRejectsAtOnce(t *testing.T) {
	f := newReplaceOkFixture(t)
	err := f.svc.StartReplace(ReplaceRequest{Kname: "sdz", Format: true, UseData: true, Fallback: FallbackSystem})
	if got := CodeOf(err); got != CodeUnknownDrive {
		t.Fatalf("StartReplace error = %q (%v), want %q", got, err, CodeUnknownDrive)
	}
	if len(f.calls) != 0 || f.stopped != 0 || f.svc.Busy() {
		t.Fatalf("rejected start called %v, stopped %d, busy %v", f.calls, f.stopped, f.svc.Busy())
	}
}
