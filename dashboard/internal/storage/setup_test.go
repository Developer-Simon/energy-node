package storage

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

const (
	testNodeID = "node-1"
	testUUID   = "11111111-2222-3333-4444-555555555555"
)

var errInjected = errors.New("injected failure")

// failAt returns a step hook that fails exactly the named step.
func failAt(step string) func(string) error {
	return func(name string) error {
		if name == step {
			return errInjected
		}
		return nil
	}
}

type setupFixture struct {
	t        *testing.T
	dataDir  string
	mount    string
	runtime  string
	fallback string
	svc      *Service
	// configured is the runtime_dir the config holds; "" means legacy layout.
	configured string

	calls     []string
	verbErr   map[string]error
	stopErr   error
	applied   []string
	stopped   int
	started   int
	restarted int
	hookFn    func(name string) error
}

func newSetupFixture(t *testing.T) *setupFixture {
	t.Helper()
	root := t.TempDir()
	f := &setupFixture{
		t:          t,
		configured: filepath.Join(root, "data", "runtime"),
		dataDir:    filepath.Join(root, "data"),
		mount:      filepath.Join(root, "mnt", "energy-node-data"),
		runtime:    filepath.Join(root, "data", "runtime"),
		fallback:   filepath.Join(root, "data", "runtime-fallback"),
		verbErr:    map[string]error{},
	}
	writeTestFile(t, filepath.Join(f.runtime, "a.json"), "alpha")
	writeTestFile(t, filepath.Join(f.runtime, "sub", "b.bin"), "beta-bytes")
	writeTestFile(t, filepath.Join(f.dataDir, "backups", "20261108-120000-manual.tar.gz"), "archive")

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
				return f.stopErr
			},
			StartServices: func() error {
				f.started++
				return nil
			},
			RestartDashboard: func(ctx context.Context) error {
				f.restarted++
				return nil
			},
			ReadRuntimeDir: func() string { return f.configured },
			ApplyRuntimeDir: func(ctx context.Context, dir string) error {
				f.applied = append(f.applied, dir)
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

type fakeLister struct {
	drives []Drive
}

func (l fakeLister) Drives(ctx context.Context) ([]Drive, error) {
	return l.drives, nil
}

// storage stands in for the root helper: format prints a fixed UUID, mount
// creates the mount point and the marker like the real helper does.
func (f *setupFixture) storage(ctx context.Context, verb, arg string) (string, error) {
	f.calls = append(f.calls, verb)
	if err := f.verbErr[verb]; err != nil {
		return "", err
	}
	switch verb {
	case "format":
		return testUUID, nil
	case "mount":
		if err := os.MkdirAll(DriveRuntimeDir(Paths{Mount: f.mount}), 0o755); err != nil {
			return "", err
		}
		marker := filepath.Join(f.mount, markerName)
		if _, err := os.Stat(marker); os.IsNotExist(err) {
			writeTestFile(f.t, marker, testNodeID+"\n")
		}
	}
	return "", nil
}

func (f *setupFixture) request() SetupRequest {
	return SetupRequest{
		Kname:      "sdb",
		Format:     true,
		UseData:    true,
		UseBackups: true,
		Fallback:   FallbackSystem,
	}
}

// assertSystemUntouched checks that an aborted setup left the system disk as it was.
func (f *setupFixture) assertSystemUntouched() {
	f.t.Helper()
	fi, err := os.Lstat(f.runtime)
	if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		f.t.Fatalf("runtime dir is no longer the original directory: %v", err)
	}
	if got := readTestFile(f.t, filepath.Join(f.runtime, "a.json")); got != "alpha" {
		f.t.Fatalf("a.json = %q, want alpha", got)
	}
	if got := readTestFile(f.t, filepath.Join(f.runtime, "sub", "b.bin")); got != "beta-bytes" {
		f.t.Fatalf("sub/b.bin = %q, want beta-bytes", got)
	}
	if _, err := os.Stat(filepath.Join(f.dataDir, "backups", "20261108-120000-manual.tar.gz")); err != nil {
		f.t.Fatalf("system archive: %v", err)
	}
	for _, p := range []string{filepath.Join(f.dataDir, configFile), filepath.Join(f.dataDir, oldRuntimeName), filepath.Join(f.dataDir, pendingFile)} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			f.t.Fatalf("%s must not exist after an abort: %v", p, err)
		}
	}
	if n := len(f.applied); n > 0 && f.applied[n-1] != f.configured {
		f.t.Fatalf("runtime_dir left applied as %q, want %q", f.applied[n-1], f.configured)
	}
	if f.started != f.stopped {
		f.t.Fatalf("services stopped %d times but started %d times", f.stopped, f.started)
	}
}

func assertJob(t *testing.T, svc *Service, wantErr string) {
	t.Helper()
	job := svc.Job()
	if !job.Done {
		t.Fatalf("job not done: %+v", job)
	}
	if job.Err != wantErr {
		t.Fatalf("job error = %q, want %q", job.Err, wantErr)
	}
	if job.Phase == "" {
		t.Fatalf("job without phase: %+v", job)
	}
}

func TestSetupAbortLeavesSystemDriveUntouched(t *testing.T) {
	cases := []struct {
		name     string
		hook     string
		stopErr  bool
		format   bool
		wantCode string
	}{
		{name: "format", hook: "format", format: true, wantCode: CodeFormatFailed},
		{name: "mount", hook: "mount", wantCode: CodeMountFailed},
		{name: "copy", hook: "copy", wantCode: CodeCopyFailed},
		{name: "verify", hook: "verify", wantCode: CodeVerifyFailed},
		{name: "stop", hook: "stop", wantCode: CodeStopFailed},
		{name: "stop call fails", stopErr: true, wantCode: CodeStopFailed},
		{name: "copy final", hook: "copy_final", wantCode: CodeCopyFailed},
		{name: "verify final", hook: "verify_final", wantCode: CodeVerifyFailed},
		{name: "config", hook: "config", wantCode: CodeConfigFailed},
		{name: "switch", hook: "switch", wantCode: CodeSwitchFailed},
		{name: "save", hook: "save", wantCode: CodeSaveFailed},
		{name: "restart", hook: "restart", wantCode: CodeRestartFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSetupFixture(t)
			if tc.hook != "" {
				f.hookFn = failAt(tc.hook)
			}
			if tc.stopErr {
				f.stopErr = errInjected
			}
			req := f.request()
			req.Format = tc.format
			if !tc.format {
				req.Kname = "sdc"
			}

			err := f.svc.Setup(context.Background(), req)
			if got := CodeOf(err); got != tc.wantCode {
				t.Fatalf("Setup error code = %q (%v), want %q", got, err, tc.wantCode)
			}
			assertJob(t, f.svc, tc.wantCode)
			f.assertSystemUntouched()
		})
	}
}

func TestSetupFormatFailureKeepsHelperOutput(t *testing.T) {
	f := newSetupFixture(t)
	inner := f.svc.Deps.Storage
	f.svc.Deps.Storage = func(ctx context.Context, verb, arg string) (string, error) {
		if verb == "format" {
			f.calls = append(f.calls, verb)
			return "DEVICE_UNKNOWN", errors.New("exit status 65")
		}
		return inner(ctx, verb, arg)
	}

	err := f.svc.Setup(context.Background(), f.request())
	if got := CodeOf(err); got != CodeFormatFailed {
		t.Fatalf("Setup error code = %q (%v), want %q", got, err, CodeFormatFailed)
	}
	if !strings.Contains(err.Error(), "DEVICE_UNKNOWN") {
		t.Fatalf("Setup error %q does not contain the helper output", err)
	}
	assertJob(t, f.svc, CodeFormatFailed)
	f.assertSystemUntouched()
}

// The helper prints the code and the reason of a failed step on two lines. The
// log entry stays on one line.
func TestSetupFormatFailureKeepsReasonOnOneLine(t *testing.T) {
	f := newSetupFixture(t)
	inner := f.svc.Deps.Storage
	f.svc.Deps.Storage = func(ctx context.Context, verb, arg string) (string, error) {
		if verb == "format" {
			f.calls = append(f.calls, verb)
			return "FORMAT_FAILED\nmkfs: mke2fs: /dev/sda1 contains a iso9660 file system\n", errors.New("exit status 1")
		}
		return inner(ctx, verb, arg)
	}

	err := f.svc.Setup(context.Background(), f.request())
	if got := CodeOf(err); got != CodeFormatFailed {
		t.Fatalf("Setup error code = %q (%v), want %q", got, err, CodeFormatFailed)
	}
	if !strings.Contains(err.Error(), "mkfs: mke2fs: /dev/sda1 contains a iso9660 file system") {
		t.Fatalf("Setup error %q does not contain the reason", err)
	}
	if strings.Contains(err.Error(), "\n") {
		t.Fatalf("Setup error %q spans several lines", err)
	}
}

func TestSetupVerifiesBeforeSwitching(t *testing.T) {
	f := newSetupFixture(t)
	f.hookFn = func(name string) error {
		if name == "verify" {
			// Same size, different content: only the SHA-256 comparison can catch it.
			writeTestFile(t, filepath.Join(DriveRuntimeDir(f.svc.Paths), "a.json"), "ALPHA")
		}
		return nil
	}

	err := f.svc.Setup(context.Background(), f.request())
	if got := CodeOf(err); got != CodeVerifyFailed {
		t.Fatalf("Setup error code = %q (%v), want %q", got, err, CodeVerifyFailed)
	}
	assertJob(t, f.svc, CodeVerifyFailed)
	if f.stopped != 0 {
		t.Fatalf("services stopped before the copy was verified")
	}
	if len(f.applied) != 0 {
		t.Fatalf("runtime_dir applied before the copy was verified: %v", f.applied)
	}
	f.assertSystemUntouched()
}

func TestSetupRejectsForeignDrive(t *testing.T) {
	f := newSetupFixture(t)
	writeTestFile(t, filepath.Join(f.mount, markerName), "someone-else\n")

	err := f.svc.Setup(context.Background(), f.request())
	if got := CodeOf(err); got != CodeDriveForeign {
		t.Fatalf("Setup error code = %q (%v), want %q", got, err, CodeDriveForeign)
	}
	f.assertSystemUntouched()
}

func TestSetupRejectsUnknownDrive(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*SetupRequest)
		wantCode string
	}{
		{name: "unknown kname", mutate: func(r *SetupRequest) { r.Kname = "sdz" }, wantCode: CodeUnknownDrive},
		{name: "system disk name", mutate: func(r *SetupRequest) { r.Kname = "mmcblk0" }, wantCode: CodeUnknownDrive},
		{name: "uuid mismatch", mutate: func(r *SetupRequest) {
			r.Kname = "sdc"
			r.Format = false
			r.UUID = "99999999-2222-3333-4444-555555555555"
		}, wantCode: CodeUnknownDrive},
		{name: "format on a ready drive", mutate: func(r *SetupRequest) { r.Kname = "sdc" }, wantCode: CodeFormatNotNeeded},
		{name: "needs format but not confirmed", mutate: func(r *SetupRequest) { r.Format = false }, wantCode: CodeNeedsFormat},
		{name: "unknown fallback", mutate: func(r *SetupRequest) { r.Fallback = "cloud" }, wantCode: CodeFallbackInvalid},
		{name: "nothing selected", mutate: func(r *SetupRequest) {
			r.UseData = false
			r.UseBackups = false
		}, wantCode: CodeNothingSelected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSetupFixture(t)
			req := f.request()
			tc.mutate(&req)

			err := f.svc.Setup(context.Background(), req)
			if got := CodeOf(err); got != tc.wantCode {
				t.Fatalf("Setup error code = %q (%v), want %q", got, err, tc.wantCode)
			}
			if len(f.calls) != 0 || f.stopped != 0 {
				t.Fatalf("rejected request still ran helper calls %v or stopped services", f.calls)
			}
			f.assertSystemUntouched()
		})
	}
}

func TestSetupRejectsWhenAlreadySetUp(t *testing.T) {
	f := newSetupFixture(t)
	if err := SaveConfig(f.dataDir, Config{UUID: testUUID, Fallback: FallbackSystem, UseData: true}); err != nil {
		t.Fatal(err)
	}
	err := f.svc.Setup(context.Background(), f.request())
	if got := CodeOf(err); got != CodeAlreadySetUp {
		t.Fatalf("Setup error code = %q (%v), want %q", got, err, CodeAlreadySetUp)
	}
}

func TestRemoveMovesDataBack(t *testing.T) {
	f := newSetupFixture(t)
	ctx := context.Background()

	if err := f.svc.Setup(ctx, f.request()); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if fi, err := os.Lstat(f.runtime); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("runtime path is not a symlink after setup: %v", err)
	}
	if got := readTestFile(t, filepath.Join(f.mount, driveRuntimeName, "sub", "b.bin")); got != "beta-bytes" {
		t.Fatalf("drive copy of sub/b.bin = %q", got)
	}
	if target, err := os.Readlink(f.runtime); err != nil || target != filepath.Join(f.mount, "runtime") {
		t.Fatalf("runtime symlink = %q (%v), want the drive runtime dir", target, err)
	}
	if kind, ok, err := readPending(f.dataDir); err != nil || !ok || kind != pendingSetup {
		t.Fatalf("pending marker after setup = %q, %v, %v", kind, ok, err)
	}
	old := filepath.Join(f.dataDir, oldRuntimeName)
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("old runtime data must wait for the follow-up step: %v", err)
	}

	if err := f.svc.Finish(ctx); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("Finish kept the old runtime data: %v", err)
	}
	if f.started != 1 {
		t.Fatalf("Finish started services %d times, want 1", f.started)
	}
	if _, ok, _ := readPending(f.dataDir); ok {
		t.Fatalf("Finish left the pending marker")
	}

	if err := f.svc.Remove(ctx); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	target, err := os.Readlink(f.runtime)
	if err != nil || target != f.fallback {
		t.Fatalf("runtime symlink = %q (%v), want %q", target, err, f.fallback)
	}
	if kind, ok, _ := readPending(f.dataDir); !ok || kind != pendingRemove {
		t.Fatalf("pending marker after remove = %q, %v", kind, ok)
	}
	if got := readTestFile(t, filepath.Join(f.fallback, "a.json")); got != "alpha" {
		t.Fatalf("moved back a.json = %q", got)
	}
	if got := readTestFile(t, filepath.Join(f.fallback, "sub", "b.bin")); got != "beta-bytes" {
		t.Fatalf("moved back sub/b.bin = %q", got)
	}
	if _, err := os.Stat(filepath.Join(f.fallback, markerName)); !os.IsNotExist(err) {
		t.Fatalf("volume marker copied to the system disk: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.dataDir, "backups", "20261108-120000-manual.tar.gz")); err != nil {
		t.Fatalf("archive missing after remove: %v", err)
	}
	cfg, err := LoadConfig(f.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UUID != "" || cfg.Fallback != FallbackSystem {
		t.Fatalf("config after remove = %+v", cfg)
	}
	if _, err := os.Stat(filepath.Join(f.mount, markerName)); err != nil {
		t.Fatalf("remove wiped the drive: %v", err)
	}
	if !contains(f.calls, "unmount") {
		t.Fatalf("remove did not unmount, calls %v", f.calls)
	}
	if f.restarted != 2 {
		t.Fatalf("restarts = %d, want 2 (setup and remove)", f.restarted)
	}
	assertJob(t, f.svc, "")
}

func TestResumeFinishesAfterGreenRestart(t *testing.T) {
	f := newSetupFixture(t)
	ctx := context.Background()
	if err := f.svc.Setup(ctx, f.request()); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	// The drive is unplugged before the next boot: nothing happens, the marker stays.
	if err := os.Remove(filepath.Join(f.mount, markerName)); err != nil {
		t.Fatal(err)
	}
	if ran, err := f.svc.Resume(ctx); err != nil || ran {
		t.Fatalf("Resume without green status = %v, %v", ran, err)
	}
	if f.started != 0 {
		t.Fatalf("services started without green status")
	}
	if _, ok, _ := readPending(f.dataDir); !ok {
		t.Fatalf("marker removed without green status")
	}

	// The drive is back: the follow-up runs once.
	writeTestFile(t, filepath.Join(f.mount, markerName), testNodeID+"\n")
	if ran, err := f.svc.Resume(ctx); err != nil || !ran {
		t.Fatalf("Resume with green status = %v, %v", ran, err)
	}
	if _, err := os.Stat(filepath.Join(f.dataDir, oldRuntimeName)); !os.IsNotExist(err) {
		t.Fatalf("old runtime data kept after Resume: %v", err)
	}
	if f.started != 1 {
		t.Fatalf("services started %d times, want 1", f.started)
	}
	if ran, err := f.svc.Resume(ctx); err != nil || ran {
		t.Fatalf("second Resume = %v, %v, want nothing to do", ran, err)
	}
}

func TestResumeWithoutMarkerDoesNothing(t *testing.T) {
	f := newSetupFixture(t)
	if ran, err := f.svc.Resume(context.Background()); err != nil || ran {
		t.Fatalf("Resume without marker = %v, %v", ran, err)
	}
	if f.started != 0 {
		t.Fatalf("services started without a pending job")
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// newLegacySetupFixture puts the runtime data where a device without
// runtime_dir keeps it: runtime cache and energy months in data_dir, the
// automation history in devices_dir, one battery state in the shared legacy file.
func newLegacySetupFixture(t *testing.T) *setupFixture {
	t.Helper()
	f := newSetupFixture(t)
	if err := os.RemoveAll(f.runtime); err != nil {
		t.Fatal(err)
	}
	f.configured = ""
	devices := filepath.Join(t.TempDir(), "devices")
	f.svc.Legacy = Legacy{DevicesDir: devices, BatteryState: filepath.Join(t.TempDir(), "battery_soc", "state.json")}
	writeTestFile(t, filepath.Join(f.dataDir, "runtime.json"), "cache")
	writeTestFile(t, filepath.Join(f.dataDir, "energystats", "2026-11.json"), "months")
	writeTestFile(t, filepath.Join(devices, automationHistoryFile), `{"entries":[1]}`)
	writeTestFile(t, filepath.Join(devices, "battery_soc_devices.json"), `[{"id":"bat1","name":"Akku"}]`)
	writeTestFile(t, f.svc.Legacy.BatteryState, "soc-state")
	return f
}

// legacyFiles lists the legacy locations with the content they had before setup.
func (f *setupFixture) legacyFiles() map[string]string {
	devices := f.svc.Legacy.DevicesDir
	return map[string]string{
		filepath.Join(f.dataDir, "runtime.json"):                "cache",
		filepath.Join(f.dataDir, "energystats", "2026-11.json"): "months",
		filepath.Join(devices, automationHistoryFile):           `{"entries":[1]}`,
		f.svc.Legacy.BatteryState:                               "soc-state",
	}
}

func (f *setupFixture) assertLegacyUntouched() {
	f.t.Helper()
	for path, want := range f.legacyFiles() {
		if got := readTestFile(f.t, path); got != want {
			f.t.Fatalf("legacy file %s = %q after an abort, want %q", path, got, want)
		}
		if _, err := os.Stat(path + legacyOldSuffix); !os.IsNotExist(err) {
			f.t.Fatalf("%s.old exists after an abort: %v", path, err)
		}
	}
	if _, err := os.Lstat(f.runtime); !os.IsNotExist(err) {
		f.t.Fatalf("runtime path created after an abort: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.dataDir, configFile)); !os.IsNotExist(err) {
		f.t.Fatalf("storage.json written after an abort: %v", err)
	}
	if n := len(f.applied); n > 0 && f.applied[n-1] != "" {
		f.t.Fatalf("runtime_dir left applied as %q", f.applied[n-1])
	}
	if f.started != f.stopped {
		f.t.Fatalf("services stopped %d times but started %d times", f.stopped, f.started)
	}
}

func TestSetupLegacyAbortLeavesLegacyUntouched(t *testing.T) {
	cases := []struct {
		hook     string
		wantCode string
	}{
		{"copy", CodeCopyFailed},
		{"verify", CodeVerifyFailed},
		{"stop", CodeStopFailed},
		{"copy_final", CodeCopyFailed},
		{"verify_final", CodeVerifyFailed},
		{"config", CodeConfigFailed},
		{"switch", CodeSwitchFailed},
		{"save", CodeSaveFailed},
		{"restart", CodeRestartFailed},
	}
	for _, tc := range cases {
		t.Run(tc.hook, func(t *testing.T) {
			f := newLegacySetupFixture(t)
			f.hookFn = failAt(tc.hook)
			req := f.request()
			err := f.svc.Setup(context.Background(), req)
			if got := CodeOf(err); got != tc.wantCode {
				t.Fatalf("Setup error code = %q (%v), want %q", got, err, tc.wantCode)
			}
			assertJob(t, f.svc, tc.wantCode)
			f.assertLegacyUntouched()
		})
	}
}

func TestSetupLegacyVerifiesBeforeMovingFiles(t *testing.T) {
	f := newLegacySetupFixture(t)
	f.hookFn = func(name string) error {
		if name == "verify" {
			// Same size, different content on the drive copy of the runtime cache.
			writeTestFile(t, filepath.Join(DriveRuntimeDir(f.svc.Paths), "runtime.json"), "CACHE")
		}
		return nil
	}
	err := f.svc.Setup(context.Background(), f.request())
	if got := CodeOf(err); got != CodeVerifyFailed {
		t.Fatalf("Setup error code = %q (%v), want %q", got, err, CodeVerifyFailed)
	}
	if f.stopped != 0 {
		t.Fatal("services stopped before the legacy copy was verified")
	}
	f.assertLegacyUntouched()
}

func TestSetupLegacyRefusesLeftoverOldFiles(t *testing.T) {
	f := newLegacySetupFixture(t)
	writeTestFile(t, filepath.Join(f.dataDir, "runtime.json"+legacyOldSuffix), "from an earlier setup")
	err := f.svc.Setup(context.Background(), f.request())
	if got := CodeOf(err); got != CodeSwitchFailed {
		t.Fatalf("Setup error code = %q (%v), want %q", got, err, CodeSwitchFailed)
	}
	if len(f.calls) != 0 {
		t.Fatalf("helper called despite the refusal: %v", f.calls)
	}
}

func TestSetupLegacyMovesDataToDrive(t *testing.T) {
	f := newLegacySetupFixture(t)
	ctx := context.Background()
	if err := f.svc.Setup(ctx, f.request()); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	drive := DriveRuntimeDir(f.svc.Paths)
	for rel, want := range map[string]string{
		"runtime.json": "cache",
		filepath.Join("energystats", "2026-11.json"): "months",
		automationHistoryFile:                        `{"entries":[1]}`,
		filepath.Join("battery_soc", "bat1.json"):    "soc-state",
	} {
		if got := readTestFile(t, filepath.Join(drive, rel)); got != want {
			t.Fatalf("drive copy of %s = %q, want %q", rel, got, want)
		}
	}
	// The legacy places: the runtime cache, the energystats directory, the
	// automation history and the shared battery state file.
	places := []string{
		filepath.Join(f.dataDir, "runtime.json"),
		filepath.Join(f.dataDir, "energystats"),
		filepath.Join(f.svc.Legacy.DevicesDir, automationHistoryFile),
		f.svc.Legacy.BatteryState,
	}
	for _, path := range places {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("legacy place %s still in use: %v", path, err)
		}
		if _, err := os.Stat(path + legacyOldSuffix); err != nil {
			t.Fatalf("old legacy place %s not kept until Finish: %v", path, err)
		}
	}
	if target, err := os.Readlink(f.runtime); err != nil || target != drive {
		t.Fatalf("runtime symlink = %q (%v), want %q", target, err, drive)
	}
	cfg, err := LoadConfig(f.dataDir)
	if err != nil || !cfg.Legacy || len(cfg.LegacyOld) != 4 {
		t.Fatalf("config = %+v, %v; want legacy with four old files", cfg, err)
	}
	if f.applied[len(f.applied)-1] != f.runtime {
		t.Fatalf("runtime_dir applied = %q, want %q", f.applied[len(f.applied)-1], f.runtime)
	}

	if err := f.svc.Finish(ctx); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	for _, path := range places {
		if _, err := os.Stat(path + legacyOldSuffix); !os.IsNotExist(err) {
			t.Fatalf("Finish kept %s.old: %v", path, err)
		}
	}
}

func TestRemoveLegacyMovesDataBack(t *testing.T) {
	f := newLegacySetupFixture(t)
	ctx := context.Background()
	if err := f.svc.Setup(ctx, f.request()); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if err := f.svc.Finish(ctx); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if err := f.svc.Remove(ctx); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	for path, want := range f.legacyFiles() {
		if got := readTestFile(t, path); got != want {
			t.Fatalf("legacy file %s after remove = %q, want %q", path, got, want)
		}
	}
	if _, err := os.Lstat(f.runtime); !os.IsNotExist(err) {
		t.Fatalf("runtime symlink kept after remove: %v", err)
	}
	if f.applied[len(f.applied)-1] != "" {
		t.Fatalf("runtime_dir after remove = %q, want unset", f.applied[len(f.applied)-1])
	}
	cfg, err := LoadConfig(f.dataDir)
	if err != nil || cfg.UUID != "" || cfg.Legacy {
		t.Fatalf("config after remove = %+v, %v", cfg, err)
	}
	if _, err := os.Stat(filepath.Join(f.mount, markerName)); err != nil {
		t.Fatalf("remove wiped the drive: %v", err)
	}
	assertJob(t, f.svc, "")
}

// TestSetupMovesBackupsAsideUntilResume checks that the archives leave the
// system disk once they are on the drive, that Resume deletes the old copy
// after the green restart, and that Remove still copies them back.
func TestSetupMovesBackupsAsideUntilResume(t *testing.T) {
	f := newSetupFixture(t)
	ctx := context.Background()
	const archive = "20261108-120000-manual.tar.gz"
	backups := filepath.Join(f.dataDir, "backups")
	old := backups + legacyOldSuffix

	if err := f.svc.Setup(ctx, f.request()); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if _, err := os.Lstat(backups); !os.IsNotExist(err) {
		t.Fatalf("backups still on the system disk after setup: %v", err)
	}
	if got := readTestFile(t, filepath.Join(old, archive)); got != "archive" {
		t.Fatalf("backups.old archive = %q, want the kept copy until Resume", got)
	}
	if got := readTestFile(t, filepath.Join(f.mount, "backups", archive)); got != "archive" {
		t.Fatalf("drive copy of the archive = %q", got)
	}
	cfg, err := LoadConfig(f.dataDir)
	if err != nil || cfg.BackupsOld != old {
		t.Fatalf("config = %+v, %v; want backups_old %q", cfg, err, old)
	}

	if ran, err := f.svc.Resume(ctx); err != nil || !ran {
		t.Fatalf("Resume after green restart = %v, %v", ran, err)
	}
	if _, err := os.Lstat(old); !os.IsNotExist(err) {
		t.Fatalf("Resume kept backups.old: %v", err)
	}

	if err := f.svc.Remove(ctx); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := readTestFile(t, filepath.Join(backups, archive)); got != "archive" {
		t.Fatalf("Remove did not copy the archive back: %q", got)
	}
	assertJob(t, f.svc, "")
}

// TestSetupBackupsRollBackWithTheRest checks that a failure after the backups
// moved aside puts them back where they were.
func TestSetupBackupsRollBackWithTheRest(t *testing.T) {
	f := newSetupFixture(t)
	f.hookFn = failAt("save")
	err := f.svc.Setup(context.Background(), f.request())
	if got := CodeOf(err); got != CodeSaveFailed {
		t.Fatalf("Setup error code = %q (%v), want %q", got, err, CodeSaveFailed)
	}
	f.assertSystemUntouched()
	if _, err := os.Lstat(filepath.Join(f.dataDir, "backups"+legacyOldSuffix)); !os.IsNotExist(err) {
		t.Fatalf("backups.old left after rollback: %v", err)
	}
}

// TestResumeMergesLeftoverLegacyEnergyStats checks that the energy months the
// old dashboard writes to the legacy folder on its SIGTERM flush (the last
// partial hour) reach the drive copy, and that the folder is then removed.
func TestResumeMergesLeftoverLegacyEnergyStats(t *testing.T) {
	f := newLegacySetupFixture(t)
	ctx := context.Background()
	if err := f.svc.Setup(ctx, f.request()); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	// The old process flushes the partial hour into the legacy folder after the setup.
	legacy := filepath.Join(f.dataDir, "energystats")
	store := energystats.NewStore(legacy)
	hour := time.Date(2026, 12, 3, 10, 0, 0, 0, time.UTC)
	if err := store.AddOwn(energystats.HourRecord{Hour: hour, KWh: map[string]float64{"grid": 1.5}, Covered: 1800}); err != nil {
		t.Fatalf("flush partial hour: %v", err)
	}

	if ran, err := f.svc.Resume(ctx); err != nil || !ran {
		t.Fatalf("Resume after green restart = %v, %v", ran, err)
	}
	if _, err := os.Lstat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy energystats folder kept after Resume: %v", err)
	}
	if got := gridKWh(t, filepath.Join(DriveRuntimeDir(f.svc.Paths), "energystats"), hour); got != 1.5 {
		t.Fatalf("grid kWh on the drive = %v, want 1.5", got)
	}
}

// gridKWh reads the grid energy of the hour from an energystats directory.
func gridKWh(t *testing.T, dir string, hour time.Time) float64 {
	t.Helper()
	got := math.NaN()
	err := energystats.NewStore(dir).Each(hour, hour.Add(time.Hour), func(h time.Time, m *energystats.Month, i int) {
		if s := m.Roles["grid"]; s != nil {
			got = s.KWh[i]
		}
	})
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	return got
}

// TestBackupsOnlySetupDeletesOldBackups checks that a setup without runtime
// data has no restart and no Resume, so the old archives go right after the
// verified copy, without a *.old safety net.
func TestBackupsOnlySetupDeletesOldBackups(t *testing.T) {
	f := newSetupFixture(t)
	req := f.request()
	req.UseData = false

	if err := f.svc.Setup(context.Background(), req); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(f.dataDir, "backups")); !os.IsNotExist(err) {
		t.Fatalf("old backups kept on the system disk: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(f.dataDir, "backups"+legacyOldSuffix)); !os.IsNotExist(err) {
		t.Fatalf("backups.old kept without a Resume: %v", err)
	}
	if got := readTestFile(t, filepath.Join(f.mount, "backups", "20261108-120000-manual.tar.gz")); got != "archive" {
		t.Fatalf("drive copy of the archive = %q", got)
	}
	if _, ok, _ := readPending(f.dataDir); ok {
		t.Fatalf("backups-only setup left a pending marker")
	}
	if f.restarted != 0 {
		t.Fatalf("backups-only setup restarted the dashboard %d times", f.restarted)
	}
	if target, err := os.Readlink(f.runtime); err == nil {
		t.Fatalf("runtime path switched to %q by a backups-only setup", target)
	}
	assertJob(t, f.svc, "")
}
