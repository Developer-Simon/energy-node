package host

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/bundlesource"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

var _ hostapi.PackageBackend = (*Host)(nil)

type recordingSink struct{ markers, logs, notes []string }

func (s *recordingSink) Marker(id, state, detail string) { s.markers = append(s.markers, id+":"+state) }
func (s *recordingSink) Log(id, line string)             { s.logs = append(s.logs, line) }
func (s *recordingSink) Message(id, key string, args map[string]string) {
	s.notes = append(s.notes, id+":"+key)
}

// stubSeams replaces the SSH-facing seams; the returned recorder tells a
// test what was staged.
type staged struct {
	archive, remoteDir string
	signed             bool
	verifyCalls        int
}

func stubSeams(t *testing.T, machine string) *staged {
	t.Helper()
	origDetect, origStage, origVerify, origProvision := detectMachine, stageBundle, verifyStaged, provisionRemoteStateDir
	t.Cleanup(func() {
		detectMachine, stageBundle, verifyStaged, provisionRemoteStateDir = origDetect, origStage, origVerify, origProvision
	})
	rec := &staged{}
	detectMachine = func(context.Context, *transport.Client) (string, error) { return machine, nil }
	provisionRemoteStateDir = func(context.Context, *transport.Client, string) error { return nil }
	stageBundle = func(_ context.Context, _ *transport.Client, archive, remoteDir string, onProgress func(done, total int64)) error {
		rec.archive, rec.remoteDir = archive, remoteDir
		onProgress(50, 100)
		onProgress(100, 100)
		if _, err := os.Stat(archive); err != nil {
			return err
		}
		return nil
	}
	verifyStaged = func(_ context.Context, _ *transport.Client, _ string, signed bool) error {
		rec.verifyCalls++
		rec.signed = signed
		return nil
	}
	return rec
}

// hostWithBundled builds a connected Host whose bundled dir holds a valid
// unsigned armv6 bundle.
func hostWithBundled(t *testing.T) *Host {
	t.Helper()
	dir := t.TempDir()
	writeUnsignedBundle(t, dir, "armv6")
	work := t.TempDir()
	h, err := New(Config{
		BundleDir: dir, RemoteBundleDir: "/var/lib/energy-node-installer/bundle",
		Resolver: &bundlesource.Resolver{BundledDir: dir, WorkDir: work, GitHub: &bundlesource.GitHub{CacheDir: t.TempDir()}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	h.client = &transport.Client{}
	return h
}

func writeUnsignedBundle(t *testing.T, dir, arch string) {
	t.Helper()
	// files is empty so VerifyDev has nothing to hash.
	manifest := `{"version":"v1.4.2","arch":"` + arch + `","uname_machine":["armv6l"],"python_abi":"cp311","steps":[],"files":{}}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNewWithoutABundleIsNotAnError(t *testing.T) {
	h, err := New(Config{BundleDir: t.TempDir(), Resolver: &bundlesource.Resolver{}})
	if err != nil {
		t.Fatalf("New must accept a missing bundle: %v", err)
	}
	if got := h.Describe(); got.BundleVersion != "" || got.Package == nil || got.Package.Bundled != nil {
		t.Errorf("Describe() = %+v, want no bundle and a Package block", got)
	}
	if _, err := h.Manifest(context.Background()); err == nil {
		t.Errorf("Manifest before prepare must fail with NO_PACKAGE")
	} else {
		var apiErr *hostapi.Error
		if !errors.As(err, &apiErr) || apiErr.Code != "NO_PACKAGE" {
			t.Errorf("Manifest error = %v, want NO_PACKAGE", err)
		}
	}
}

func TestPrepareStagesTheBundledDirectoryAsAnArchive(t *testing.T) {
	rec := stubSeams(t, "armv6l")
	h := hostWithBundled(t)
	sink := &recordingSink{}

	if err := h.SelectPackage(context.Background(), hostapi.PackageSelection{Kind: "bundled"}); err != nil {
		t.Fatalf("SelectPackage: %v", err)
	}
	if err := h.Run(context.Background(), hostapi.RunRequest{Mode: hostapi.ModePrepare}, sink); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if rec.remoteDir != "/var/lib/energy-node-installer/bundle" || !strings.HasSuffix(rec.archive, ".tar.gz") {
		t.Errorf("staged %q to %q", rec.archive, rec.remoteDir)
	}
	if rec.verifyCalls != 1 || rec.signed {
		t.Errorf("verifyStaged calls = %d signed = %v, want one unsigned verification", rec.verifyCalls, rec.signed)
	}
	if got := h.Describe(); got.Package.Resolved == nil || got.Package.Resolved.Signed || got.Package.Resolved.Kind != "bundled" {
		t.Errorf("Resolved = %+v", got.Package.Resolved)
	}
	if len(sink.markers) != 2 || sink.markers[0] != "package:begin" || sink.markers[1] != "package:ok" {
		t.Errorf("markers = %v", sink.markers)
	}
}

func TestPrepareRejectsAnUnsupportedNodeArchitecture(t *testing.T) {
	stubSeams(t, "riscv64")
	h := hostWithBundled(t)
	err := h.Run(context.Background(), hostapi.RunRequest{Mode: hostapi.ModePrepare}, &recordingSink{})
	var apiErr *hostapi.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "ARCH_UNSUPPORTED" || apiErr.Detail != "riscv64" {
		t.Fatalf("err = %v, want ARCH_UNSUPPORTED riscv64", err)
	}
}

func TestPrepareMapsResolverAndBundleFaultsToTheirCodes(t *testing.T) {
	stubSeams(t, "amd64")
	h := hostWithBundled(t) // the bundled bundle is armv6, the node is amd64 -> ARCH_MISMATCH
	err := h.Run(context.Background(), hostapi.RunRequest{Mode: hostapi.ModePrepare}, &recordingSink{})
	var apiErr *hostapi.Error
	if !errors.As(err, &apiErr) || apiErr.Code != string(bundle.FaultArchMismatch) {
		t.Fatalf("err = %v, want %s", err, bundle.FaultArchMismatch)
	}
}

func TestSelectPackageRefusesANonCheckoutAndAnUnknownKind(t *testing.T) {
	h := hostWithBundled(t)
	err := h.SelectPackage(context.Background(), hostapi.PackageSelection{Kind: "repo", Path: t.TempDir()})
	var apiErr *hostapi.Error
	if !errors.As(err, &apiErr) || apiErr.Code != bundlesource.CodeRepoNotACheckout {
		t.Fatalf("err = %v, want %s", err, bundlesource.CodeRepoNotACheckout)
	}
	if err := h.SelectPackage(context.Background(), hostapi.PackageSelection{Kind: "nonsense"}); err == nil {
		t.Errorf("an unknown kind must be rejected")
	}
}

func TestUploadPackageStoresTheFileAndSelectsIt(t *testing.T) {
	h := hostWithBundled(t)
	if err := h.UploadPackage(context.Background(), "energy-node-x.tar.gz", strings.NewReader("bytes")); err != nil {
		t.Fatalf("UploadPackage: %v", err)
	}
	h.mu.Lock()
	choice, path := h.choice, h.uploaded
	h.mu.Unlock()
	if choice.Kind != bundlesource.KindFile || choice.Path != path {
		t.Fatalf("choice = %+v, uploaded = %q", choice, path)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "bytes" {
		t.Fatalf("stored upload = %q, %v", raw, err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("Close must remove the uploaded file")
	}
}

func TestPrepareBundledEmitsTranslatedNoteKeys(t *testing.T) {
	stubSeams(t, "armv6l")
	h := hostWithBundled(t)
	sink := &recordingSink{}

	if err := h.SelectPackage(context.Background(), hostapi.PackageSelection{Kind: "bundled"}); err != nil {
		t.Fatalf("SelectPackage: %v", err)
	}
	if err := h.Run(context.Background(), hostapi.RunRequest{Mode: hostapi.ModePrepare}, sink); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	// Verify the two host-emitted message keys appear in order
	if len(sink.notes) < 2 {
		t.Errorf("notes = %v, want at least 2 keys (detect_arch, arch_detected)", sink.notes)
	} else {
		if sink.notes[0] != "package:package.log.detect_arch" {
			t.Errorf("notes[0] = %q, want package:package.log.detect_arch", sink.notes[0])
		}
		if sink.notes[1] != "package:package.log.arch_detected" {
			t.Errorf("notes[1] = %q, want package:package.log.arch_detected", sink.notes[1])
		}
	}

	// Verify no German text from prepare steps appears in the logs
	for _, line := range sink.logs {
		// These are German strings that should NOT appear when using notes
		if strings.Contains(line, "Architektur") || strings.Contains(line, "Geraet meldet") ||
			strings.Contains(line, "Suche das neueste") || strings.Contains(line, "Lade ") ||
			strings.Contains(line, "liegt bereits") {
			t.Errorf("sink.logs contains German prepare text: %q", line)
		}
	}
}

func TestUploadProgressNotesEveryFivePercentOnceAndEndsAtOneHundred(t *testing.T) {
	var got []map[string]string
	report := uploadProgress(func(key string, args map[string]string) {
		if key != "package.log.upload_progress" {
			t.Errorf("key = %s", key)
		}
		got = append(got, args)
	})
	const total = 10 << 20
	for done := int64(0); done <= total; done += 64 << 10 { // 64 KiB reads
		report(done, total)
	}
	report(total, total) // a repeated final call must not repeat the note
	if len(got) != 20 {
		t.Fatalf("got %d notes, want 20 (5%% steps): %v", len(got), got)
	}
	last := got[len(got)-1]
	if last["percent"] != "100" || last["done"] != "10.0" || last["total"] != "10.0" {
		t.Errorf("last note = %v, want 100 %% of 10.0 MB", last)
	}
	if got[0]["percent"] != "5" {
		t.Errorf("first note = %v, want 5 %%", got[0])
	}
}

func TestPrepareReportsTheUploadProgress(t *testing.T) {
	stubSeams(t, "armv6l")
	sink := &recordingSink{}
	if err := hostWithBundled(t).Run(context.Background(), hostapi.RunRequest{Mode: hostapi.ModePrepare}, sink); err != nil {
		t.Fatalf("Run: %v", err)
	}
	found := false
	for _, n := range sink.notes {
		found = found || n == "package:package.log.upload_progress"
	}
	if !found {
		t.Errorf("notes = %v, want an upload_progress note", sink.notes)
	}
}
