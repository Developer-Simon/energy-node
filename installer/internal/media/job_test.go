package media

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-installer/internal/media/isopatch"
)

// fakeRun replaces the imager process. With block set it produces no output and
// ends only when the context is cancelled.
type fakeRun struct {
	mu    sync.Mutex
	name  string
	args  []string
	out   string
	code  int
	block bool
}

func (f *fakeRun) run(ctx context.Context, name string, args ...string) (io.ReadCloser, func() (int, error), error) {
	f.mu.Lock()
	f.name, f.args = name, args
	f.mu.Unlock()
	if f.block {
		return io.NopCloser(blockingReader{ctx}), func() (int, error) { return -1, ctx.Err() }, nil
	}
	return io.NopCloser(strings.NewReader(f.out)), func() (int, error) { return f.code, nil }, nil
}

func (f *fakeRun) got() (string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.name, f.args
}

type blockingReader struct{ ctx context.Context }

func (b blockingReader) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func loadFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// cacheImage stores a small stand-in image with a matching .sha256 file, so that
// Cache.Ensure runs offline.
func cacheImage(t *testing.T, cache *Cache, img Image) {
	t.Helper()
	name, err := imageName(img)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(cache.Dir, imagesDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("stand-in image " + name)
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	line := hex.EncodeToString(sum[:]) + "  " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, name+".sha256"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newTestWriter returns a writer with the stored OS list, cached images for the
// test devices, fixed drives and the fake imager. The OS list URL is unreachable,
// so the stored copy is used.
func newTestWriter(t *testing.T, drives []Drive, fr *fakeRun) *Writer {
	t.Helper()
	dir := t.TempDir()
	data, err := os.ReadFile(filepath.Join("testdata", "os_list.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, osListFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
	cache := &Cache{Dir: dir, OSListURL: "http://127.0.0.1:1/os_list.json"}
	list, err := ParseOSList(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"pi4", "pi1"} {
		dev, _ := DeviceByID(id)
		img, ok := list.Find(dev.ImageName, dev.Tag)
		if !ok {
			t.Fatalf("no image for %s", id)
		}
		cacheImage(t, cache, img)
	}
	return &Writer{
		Cache: cache,
		Imager: func(context.Context) (Imager, bool) {
			return Imager{Kind: ImagerNative, Path: "/usr/bin/rpi-imager", Version: "2.0.1"}, true
		},
		Drives:     func(context.Context) ([]Drive, error) { return drives, nil },
		Run:        fr.run,
		KnownHosts: filepath.Join(dir, "known_hosts"),
		KeyPath:    filepath.Join(dir, "installer_key"),
		WorkDir:    filepath.Join(dir, "jobs"),
	}
}

// waitJob polls until the job has ended. It fails the test after five seconds.
func waitJob(t *testing.T, w *Writer) JobView {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		v := w.View()
		if v.State != JobRunning {
			return v
		}
		if time.Now().After(deadline) {
			t.Fatal("job did not end within 5s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func jobTestSettings() Settings {
	return Settings{Hostname: "energy-node", User: "pi", AuthorizedKey: "ssh-ed25519 AAAA test"}
}

func hasArg(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}

func requireEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("%s is not empty: %d entries", dir, len(entries))
	}
}

func requireAllStepsDone(t *testing.T, v JobView) {
	t.Helper()
	for _, s := range v.Steps {
		if s.State != StepDone {
			t.Fatalf("step %s = %s, want done", s.ID, s.State)
		}
	}
}

var (
	sdb     = Drive{Path: "/dev/sdb", Model: "Stick", Size: 32e9, Removable: true}
	okWrite = WriteRequest{Device: "pi4", Drive: "/dev/sdb", Confirm: "yes", Settings: jobTestSettings()}
)

func TestStartRefusesADriveThatIsNoLongerListed(t *testing.T) {
	fr := &fakeRun{}
	w := newTestWriter(t, []Drive{sdb}, fr)
	req := okWrite
	req.Drive = "/dev/sdc"
	err := w.Start(context.Background(), req)
	if err != ErrDriveGone {
		t.Fatalf("Start error = %v, want ErrDriveGone", err)
	}
	if v := w.View(); v.State != JobIdle {
		t.Fatalf("state after refused start = %q, want %q", v.State, JobIdle)
	}
	if name, _ := fr.got(); name != "" {
		t.Fatalf("runner was called with %q", name)
	}
}

func TestAnInternalDriveNeedsItsModelName(t *testing.T) {
	internal := Drive{Path: "/dev/nvme0n1", Model: "Samsung SSD 870", Size: 500e9, Removable: false}
	fr := &fakeRun{out: loadFixture(t, "imager-ok.txt")}
	w := newTestWriter(t, []Drive{internal}, fr)
	req := WriteRequest{Device: "pi4", Drive: internal.Path, Confirm: "yes", Settings: jobTestSettings()}

	if err := w.Start(context.Background(), req); err != ErrConfirm {
		t.Fatalf("Start with confirm yes = %v, want ErrConfirm", err)
	}

	req.Confirm = internal.Model
	if err := w.Start(context.Background(), req); err != nil {
		t.Fatalf("Start with model name = %v, want nil", err)
	}
	v := waitJob(t, w)
	if v.State != JobDone {
		t.Fatalf("state = %s, error = %+v", v.State, v.Error)
	}
	if _, args := fr.got(); !hasArg(args, "--enable-writing-system-drives") {
		t.Fatalf("args %v lack --enable-writing-system-drives", args)
	}
}

func TestASecondStartIsBusy(t *testing.T) {
	w := newTestWriter(t, []Drive{sdb}, &fakeRun{block: true})
	if err := w.Start(context.Background(), okWrite); err != nil {
		t.Fatalf("first Start = %v, want nil", err)
	}
	t.Cleanup(func() {
		w.Cancel()
		waitJob(t, w)
	})
	if err := w.Start(context.Background(), okWrite); err != ErrBusy {
		t.Fatalf("second Start = %v, want ErrBusy", err)
	}
}

func TestAWriteRunsAllStepsAndPinsTheKey(t *testing.T) {
	fr := &fakeRun{out: loadFixture(t, "imager-ok.txt")}
	w := newTestWriter(t, []Drive{sdb}, fr)
	if err := w.Start(context.Background(), okWrite); err != nil {
		t.Fatal(err)
	}
	v := waitJob(t, w)
	if v.State != JobDone {
		t.Fatalf("state = %s, error = %+v", v.State, v.Error)
	}
	requireAllStepsDone(t, v)

	known, err := os.ReadFile(w.KnownHosts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(known), "energy-node.local") {
		t.Fatalf("known_hosts lacks energy-node.local: %q", known)
	}
	if v.Target == nil || v.Target.Host != "energy-node.local" || v.Target.User != "pi" || v.Target.KeyPath != w.KeyPath {
		t.Fatalf("target = %+v", v.Target)
	}
	_, args := fr.got()
	if !hasArg(args, "--cloudinit-userdata") {
		t.Fatalf("args %v lack --cloudinit-userdata", args)
	}
	requireEmptyDir(t, w.WorkDir)
}

func TestALegacyImageGetsAFirstRunScript(t *testing.T) {
	fr := &fakeRun{out: loadFixture(t, "imager-ok.txt")}
	w := newTestWriter(t, []Drive{sdb}, fr)
	req := okWrite
	req.Device = "pi1"
	if err := w.Start(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if v := waitJob(t, w); v.State != JobDone {
		t.Fatalf("state = %s, error = %+v", v.State, v.Error)
	}
	_, args := fr.got()
	if !hasArg(args, "--first-run-script") || hasArg(args, "--cloudinit-userdata") {
		t.Fatalf("args %v, want --first-run-script and no --cloudinit-userdata", args)
	}
}

func TestAFailedWriteCleansUpAndPinsNothing(t *testing.T) {
	fr := &fakeRun{out: loadFixture(t, "imager-error.txt"), code: 1}
	w := newTestWriter(t, []Drive{sdb}, fr)
	if err := w.Start(context.Background(), okWrite); err != nil {
		t.Fatal(err)
	}
	v := waitJob(t, w)
	if v.State != JobFailed {
		t.Fatalf("state = %s, want failed", v.State)
	}
	if v.Error == nil || v.Error.Code != "MEDIA_WRITE_FAILED" {
		t.Fatalf("error = %+v, want MEDIA_WRITE_FAILED", v.Error)
	}
	if !strings.Contains(v.Error.Detail, "Error writing to storage device") {
		t.Fatalf("detail = %q", v.Error.Detail)
	}
	if _, err := os.Stat(w.KnownHosts); !os.IsNotExist(err) {
		t.Fatalf("known_hosts exists after failed write: %v", err)
	}
	requireEmptyDir(t, w.WorkDir)
}

func TestPkexecDismissedMeansRightsDenied(t *testing.T) {
	fr := &fakeRun{code: 126}
	w := newTestWriter(t, []Drive{sdb}, fr)
	if err := w.Start(context.Background(), okWrite); err != nil {
		t.Fatal(err)
	}
	v := waitJob(t, w)
	if v.State != JobFailed || v.Error == nil || v.Error.Code != "MEDIA_RIGHTS_DENIED" {
		t.Fatalf("state = %s, error = %+v, want MEDIA_RIGHTS_DENIED", v.State, v.Error)
	}
	if name, _ := fr.got(); name != "pkexec" {
		t.Fatalf("runner name = %q, want pkexec", name)
	}
}

func TestTheEmergencyPasswordIsNeverInTheView(t *testing.T) {
	fr := &fakeRun{out: loadFixture(t, "imager-ok.txt")}
	w := newTestWriter(t, []Drive{sdb}, fr)
	req := okWrite
	req.Password = "geheim-123"
	if err := w.Start(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if v := waitJob(t, w); v.State != JobDone {
		t.Fatalf("state = %s, error = %+v", v.State, v.Error)
	}
	data, err := json.Marshal(w.View())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "geheim-123") {
		t.Fatal("view contains the emergency password")
	}
}

func TestSSHPasswordWithoutAPasswordIsRefused(t *testing.T) {
	fr := &fakeRun{}
	w := newTestWriter(t, []Drive{sdb}, fr)
	req := okWrite
	req.Settings.SSHPassword = true
	req.Password = ""
	if err := w.Start(context.Background(), req); err != ErrSettings {
		t.Fatalf("Start error = %v, want ErrSettings", err)
	}
}

// fakePatch replaces isopatch.Patch. It records the call, writes a stand-in
// stick image to out and, when err is set, fails without writing anything.
type fakePatch struct {
	mu          sync.Mutex
	calls       int
	in, out     string
	opts        isopatch.Options
	err         error
	view        func() JobView // snapshot taken during the call, nil = none
	stepsAtCall []StepView
}

func (f *fakePatch) patch(in, out string, o isopatch.Options) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.in, f.out, f.opts = in, out, o
	if f.view != nil {
		f.stepsAtCall = f.view().Steps
	}
	if f.err != nil {
		return f.err
	}
	return os.WriteFile(out, []byte("patched "+in), 0o644)
}

func (f *fakePatch) got() (int, string, string, isopatch.Options) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.in, f.out, f.opts
}

// cacheDebianISO stores a stand-in netinst ISO that matches the release checksum,
// so that Cache.EnsureISO runs offline.
func cacheDebianISO(t *testing.T, cache *Cache, name string) DebianRelease {
	t.Helper()
	body := []byte("stand-in netinst " + name)
	dir := filepath.Join(cache.Dir, imagesDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha512.Sum512(body)
	return DebianRelease{
		Arch:   "amd64",
		Name:   name,
		URL:    "http://127.0.0.1:1/" + name,
		SHA512: hex.EncodeToString(sum[:]),
		Size:   int64(len(body)),
	}
}

// newDebianTestWriter returns a test writer whose Debian release is a cached
// stand-in ISO and whose patch step is pt.
func newDebianTestWriter(t *testing.T, drives []Drive, fr *fakeRun, pt *fakePatch) (*Writer, DebianRelease) {
	t.Helper()
	w := newTestWriter(t, drives, fr)
	rel := cacheDebianISO(t, w.Cache, "debian-13.7.0-amd64-netinst.iso")
	w.Debian = func(context.Context, string) (DebianRelease, error) { return rel, nil }
	w.PatchISO = pt.patch
	pt.view = w.View
	return w, rel
}

func debianRequest() WriteRequest {
	s := jobTestSettings()
	s.Keyboard = "de"
	s.Timezone = "Europe/Berlin"
	return WriteRequest{Device: "deb-amd64", Drive: "/dev/sdb", Confirm: "yes", Settings: s, InstallLang: "de", SystemLang: "de"}
}

func stepIDs(steps []StepView) []StepID {
	ids := make([]StepID, len(steps))
	for i, s := range steps {
		ids[i] = s.ID
	}
	return ids
}

func fileData(files []isopatch.File, name string) ([]byte, bool) {
	for _, f := range files {
		if f.Name == name {
			return f.Data, true
		}
	}
	return nil, false
}

func TestDebianJobRunsPatchBetweenChecksumAndRights(t *testing.T) {
	fr := &fakeRun{out: loadFixture(t, "imager-ok.txt")}
	pt := &fakePatch{}
	w, _ := newDebianTestWriter(t, []Drive{sdb}, fr, pt)
	if err := w.Start(context.Background(), debianRequest()); err != nil {
		t.Fatal(err)
	}
	v := waitJob(t, w)
	if v.State != JobDone {
		t.Fatalf("state = %s, error = %+v", v.State, v.Error)
	}
	want := []StepID{StepDownload, StepChecksum, StepPatch, StepRights, StepWrite, StepVerify, StepEject}
	if got := stepIDs(v.Steps); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("steps = %v, want %v", got, want)
	}
	requireAllStepsDone(t, v)

	states := map[StepID]StepState{}
	for _, s := range pt.stepsAtCall {
		states[s.ID] = s.State
	}
	if states[StepChecksum] != StepDone || states[StepPatch] != StepActive || states[StepRights] != StepPending {
		t.Fatalf("steps during patch = %v, want checksum done, patch active, rights pending", pt.stepsAtCall)
	}
}

func TestDebianJobHasNoCustomizeStep(t *testing.T) {
	fr := &fakeRun{out: loadFixture(t, "imager-ok.txt")}
	pt := &fakePatch{}
	w, _ := newDebianTestWriter(t, []Drive{sdb}, fr, pt)
	if err := w.Start(context.Background(), debianRequest()); err != nil {
		t.Fatal(err)
	}
	v := waitJob(t, w)
	if v.State != JobDone {
		t.Fatalf("state = %s, error = %+v", v.State, v.Error)
	}
	for _, s := range v.Steps {
		if s.ID == StepCustomize {
			t.Fatal("debian job has a customize step")
		}
	}
}

func TestDebianJobPassesNoCloudInitFlags(t *testing.T) {
	fr := &fakeRun{out: loadFixture(t, "imager-ok.txt")}
	pt := &fakePatch{}
	w, _ := newDebianTestWriter(t, []Drive{sdb}, fr, pt)
	if err := w.Start(context.Background(), debianRequest()); err != nil {
		t.Fatal(err)
	}
	if v := waitJob(t, w); v.State != JobDone {
		t.Fatalf("state = %s, error = %+v", v.State, v.Error)
	}
	_, args := fr.got()
	for _, flag := range []string{"--cloudinit-userdata", "--cloudinit-networkconfig", "--first-run-script"} {
		if hasArg(args, flag) {
			t.Fatalf("args %v contain %s", args, flag)
		}
	}
}

func TestDebianJobWritesThePatchedISONotTheOriginal(t *testing.T) {
	fr := &fakeRun{out: loadFixture(t, "imager-ok.txt")}
	pt := &fakePatch{}
	w, rel := newDebianTestWriter(t, []Drive{sdb}, fr, pt)
	if err := w.Start(context.Background(), debianRequest()); err != nil {
		t.Fatal(err)
	}
	if v := waitJob(t, w); v.State != JobDone {
		t.Fatalf("state = %s, error = %+v", v.State, v.Error)
	}
	calls, in, out, opts := pt.got()
	if calls != 1 {
		t.Fatalf("patch calls = %d, want 1", calls)
	}
	if want := filepath.Join(w.Cache.Dir, imagesDir, rel.Name); in != want {
		t.Fatalf("patch input = %q, want %q", in, want)
	}
	if !strings.HasPrefix(filepath.Dir(out), filepath.Join(w.WorkDir, "job-")) {
		t.Fatalf("patch output %q is not in a job folder", out)
	}
	if opts.Label != "ENERGY-NODE-LOESCHT" {
		t.Fatalf("label = %q", opts.Label)
	}
	_, args := fr.got()
	if len(args) < 2 || args[len(args)-2] != out {
		t.Fatalf("imager args %v do not write the patched ISO %q", args, out)
	}
}

func TestDebianRequestDefaultsToAskingBeforeErasing(t *testing.T) {
	fr := &fakeRun{out: loadFixture(t, "imager-ok.txt")}
	pt := &fakePatch{}
	w, _ := newDebianTestWriter(t, []Drive{sdb}, fr, pt)
	if err := w.Start(context.Background(), debianRequest()); err != nil {
		t.Fatal(err)
	}
	if v := waitJob(t, w); v.State != JobDone {
		t.Fatalf("state = %s, error = %+v", v.State, v.Error)
	}
	_, _, _, opts := pt.got()
	env, ok := fileData(opts.Add, "energy-node.env")
	if !ok {
		t.Fatal("energy-node.env is not added to the ISO")
	}
	if !strings.Contains(string(env), "EN_CONFIRM=yes\n") {
		t.Fatalf("env = %q, want EN_CONFIRM=yes", env)
	}
}

func TestDebianRequestRefusesWifi(t *testing.T) {
	fr := &fakeRun{}
	pt := &fakePatch{}
	w, _ := newDebianTestWriter(t, []Drive{sdb}, fr, pt)
	req := debianRequest()
	req.Settings.Wifi = &Wifi{SSID: "Haus", Password: "geheim", Country: "DE"}
	if err := w.Start(context.Background(), req); !errors.Is(err, ErrSettings) {
		t.Fatalf("Start error = %v, want ErrSettings", err)
	}
	if name, _ := fr.got(); name != "" {
		t.Fatalf("runner was called with %q", name)
	}
}

func TestDebianRequestRefusesI386(t *testing.T) {
	fr := &fakeRun{}
	pt := &fakePatch{}
	w, _ := newDebianTestWriter(t, []Drive{sdb}, fr, pt)
	req := debianRequest()
	req.Device = "deb-i386"
	if err := w.Start(context.Background(), req); !errors.Is(err, ErrSettings) {
		t.Fatalf("Start error = %v, want ErrSettings", err)
	}
}

func TestBadSignatureFailsWithSignatureInvalidAndWritesNothing(t *testing.T) {
	fr := &fakeRun{}
	pt := &fakePatch{}
	w, _ := newDebianTestWriter(t, []Drive{sdb}, fr, pt)
	w.Debian = func(context.Context, string) (DebianRelease, error) {
		return DebianRelease{}, fmt.Errorf("%w: bad signature", ErrSignature)
	}
	if err := w.Start(context.Background(), debianRequest()); !errors.Is(err, ErrSignature) {
		t.Fatalf("Start error = %v, want ErrSignature", err)
	}
	if name, _ := fr.got(); name != "" {
		t.Fatalf("runner was called with %q", name)
	}
	if calls, _, _, _ := pt.got(); calls != 0 {
		t.Fatalf("patch was called %d times", calls)
	}
	requireEmptyDir(t, w.WorkDir)
}

func TestPatchFailureFailsWithPatchFailedAndNoImagerCall(t *testing.T) {
	fr := &fakeRun{out: loadFixture(t, "imager-ok.txt")}
	pt := &fakePatch{err: fmt.Errorf("%w: unexpected directory record", isopatch.ErrLayout)}
	w, _ := newDebianTestWriter(t, []Drive{sdb}, fr, pt)
	if err := w.Start(context.Background(), debianRequest()); err != nil {
		t.Fatal(err)
	}
	v := waitJob(t, w)
	if v.State != JobFailed || v.Error == nil || v.Error.Code != "MEDIA_PATCH_FAILED" {
		t.Fatalf("state = %s, error = %+v, want MEDIA_PATCH_FAILED", v.State, v.Error)
	}
	if name, _ := fr.got(); name != "" {
		t.Fatalf("runner was called with %q", name)
	}
	requireEmptyDir(t, w.WorkDir)
}

func TestADebianJobCleansUpTheStickISOAndPinsTheHostKey(t *testing.T) {
	fr := &fakeRun{out: loadFixture(t, "imager-ok.txt")}
	pt := &fakePatch{}
	w, _ := newDebianTestWriter(t, []Drive{sdb}, fr, pt)
	if err := w.Start(context.Background(), debianRequest()); err != nil {
		t.Fatal(err)
	}
	v := waitJob(t, w)
	if v.State != JobDone {
		t.Fatalf("state = %s, error = %+v", v.State, v.Error)
	}
	_, _, out, _ := pt.got()
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("stick ISO still exists: %v", err)
	}
	requireEmptyDir(t, w.WorkDir)
	known, err := os.ReadFile(w.KnownHosts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(known), "energy-node.local") {
		t.Fatalf("known_hosts lacks energy-node.local: %q", known)
	}
	if v.Target == nil || v.Target.Host != "energy-node.local" {
		t.Fatalf("target = %+v", v.Target)
	}
}

func TestDebianTooSmallDrive(t *testing.T) {
	fr := &fakeRun{}
	pt := &fakePatch{}
	w, rel := newDebianTestWriter(t, []Drive{sdb}, fr, pt)
	rel.Size = 64e9
	w.Debian = func(context.Context, string) (DebianRelease, error) { return rel, nil }
	if err := w.Start(context.Background(), debianRequest()); !errors.Is(err, ErrTooSmall) {
		t.Fatalf("Start error = %v, want ErrTooSmall", err)
	}
}
