package host

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/media"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// newMediaHost baut einen Wirt mit Medienverzeichnis und Schluesselordner in tmp.
func newMediaHost(t *testing.T, mediaDir string) *Host {
	t.Helper()
	h, err := New(Config{
		BundleDir:      t.TempDir(),
		IdentityDir:    t.TempDir(),
		KnownHostsPath: filepath.Join(t.TempDir(), "known_hosts"),
		MediaDir:       mediaDir,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

func TestDescribeOffersMediaFirst(t *testing.T) {
	with := newMediaHost(t, t.TempDir()).Describe().EntryPoints
	if len(with) == 0 || with[0] != "media" {
		t.Errorf("EntryPoints with MediaDir = %v, want media first", with)
	}
	without := newMediaHost(t, "").Describe().EntryPoints
	if slices.Contains(without, "media") {
		t.Errorf("EntryPoints without MediaDir = %v, want no media", without)
	}
}

func TestMediaWriteFillsTheInstallerKey(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("media mode is Linux only")
	}
	h := newMediaHost(t, t.TempDir())
	var got media.WriteRequest
	h.mediaStart = func(_ context.Context, req media.WriteRequest) error {
		got = req
		return nil
	}
	err := h.MediaWrite(context.Background(), hostapi.MediaWriteRequest{
		Device:   "pi4",
		Drive:    "/dev/sdb",
		Confirm:  "yes",
		Settings: hostapi.MediaSettings{Hostname: "energy-node", User: "node", Timezone: "Europe/Berlin", Keyboard: "de"},
	})
	if err != nil {
		t.Fatalf("MediaWrite: %v", err)
	}
	if !strings.HasPrefix(got.Settings.AuthorizedKey, "ssh-ed25519 ") {
		t.Errorf("AuthorizedKey = %q, want an ssh-ed25519 line", got.Settings.AuthorizedKey)
	}
}

func TestMediaUnsupportedWithoutLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("checks the non-Linux answer")
	}
	h := newMediaHost(t, t.TempDir())
	_, err := h.MediaCatalog(context.Background())
	assertAPIError(t, err, hostapi.MediaErrUnsupported, http.StatusNotImplemented)
}

func TestMediaReachableRejectsBadHosts(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("media mode is Linux only")
	}
	h := newMediaHost(t, t.TempDir())
	for _, host := range []string{"", "a/b", "energy node.local"} {
		_, err := h.MediaReachable(context.Background(), host)
		assertAPIError(t, err, "BAD_REQUEST", http.StatusBadRequest)
	}
}

func TestPinnedKeyReadsTheKnownHostsEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	hk, err := media.NewHostKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.PinHostKey(path, "energy-node.local", hk.Public); err != nil {
		t.Fatal(err)
	}
	got := pinnedKey(path, "energy-node.local")
	if got == nil || !bytes.Equal(got.Marshal(), hk.Public.Marshal()) {
		t.Errorf("pinnedKey = %v, want the pinned installer key", got)
	}
	if got := pinnedKey(path, "other.local"); got != nil {
		t.Errorf("pinnedKey for an unknown host = %v, want nil", got)
	}
}

func TestMediaErrorsMapToCodes(t *testing.T) {
	cases := []struct {
		err    error
		code   string
		status int
	}{
		{media.ErrBusy, hostapi.MediaErrBusy, http.StatusConflict},
		{media.ErrDriveGone, hostapi.MediaErrDriveGone, http.StatusConflict},
		{media.ErrConfirm, hostapi.MediaErrConfirmMismatch, http.StatusBadRequest},
		{media.ErrSettings, hostapi.MediaErrInvalidSettings, http.StatusBadRequest},
		{media.ErrImagerMissing, hostapi.MediaErrImagerMissing, http.StatusConflict},
		{media.ErrTooSmall, hostapi.MediaErrTooSmall, http.StatusBadRequest},
	}
	for _, c := range cases {
		assertAPIError(t, mediaError(c.err), c.code, c.status)
	}
	if err := mediaError(nil); err != nil {
		t.Errorf("mediaError(nil) = %v", err)
	}
}

// assertAPIError prueft, dass err ein *hostapi.Error mit Code und Status ist.
func assertAPIError(t *testing.T, err error, code string, status int) {
	t.Helper()
	var apiErr *hostapi.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want a hostapi.Error %s", err, code)
	}
	if apiErr.Code != code || apiErr.Status != status {
		t.Errorf("error = %s/%d, want %s/%d", apiErr.Code, apiErr.Status, code, status)
	}
}

func TestExecRunnerMergesOutputAndReportsTheExitCode(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses /bin/sh")
	}
	out, wait, err := execRunner(context.Background(), "/bin/sh", "-c", "echo out; echo err >&2; exit 3")
	if err != nil {
		t.Fatalf("execRunner: %v", err)
	}
	data, err := io.ReadAll(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	out.Close()
	code, werr := wait()
	if werr != nil || code != 3 {
		t.Errorf("wait() = %d, %v, want 3, nil", code, werr)
	}
	if got := string(data); !strings.Contains(got, "out") || !strings.Contains(got, "err") {
		t.Errorf("output = %q, want stdout and stderr merged", got)
	}
}
