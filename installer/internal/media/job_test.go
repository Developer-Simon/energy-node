package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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
