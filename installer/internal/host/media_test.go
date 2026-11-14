package host

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/media"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
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

func TestMediaCatalogListsTheDebianDevicesAndMarksI386Unsupported(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("media mode is Linux only")
	}
	h := newMediaHost(t, t.TempDir())
	catalog, err := h.MediaCatalog(context.Background())
	if err != nil {
		t.Fatalf("MediaCatalog: %v", err)
	}
	if len(catalog.Devices) != 8 {
		t.Fatalf("catalog has %d devices, want 8", len(catalog.Devices))
	}
	for _, d := range catalog.Devices {
		if d.ID == "deb-i386" && !d.Unsupported {
			t.Errorf("deb-i386.unsupported = false, want true")
		}
	}
}

// newDebianCatalogHost baut einen Wirt, dessen Katalog-Zwischenspeicher offline
// arbeitet: Debian-Release und OS-Liste kommen nur aus dir.
func newDebianCatalogHost(t *testing.T, dir string, keys openpgp.EntityList) *Host {
	t.Helper()
	h := newMediaHost(t, dir)
	h.mediaCache = &media.Cache{
		Dir:       dir,
		DebianURL: "http://127.0.0.1:1",
		OSListURL: "http://127.0.0.1:1/os_list.json",
		DebianKey: keys,
	}
	return h
}

func TestMediaCatalogOffersTheCachedDebianISO(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("media mode is Linux only")
	}
	dir := t.TempDir()
	keys := hostTestKeys(t)
	iso := []byte("debian netinst test image")
	isoSum := sha512.Sum512(iso)
	name := "debian-13.7.0-amd64-netinst.iso"
	signed := []byte(hex.EncodeToString(isoSum[:]) + "  " + name + "\n")
	sig := hostTestSignature(t, keys, signed)

	// Der gespeicherte Stand: signiertes SHA512SUMS und die geprueften ISO-Dateien.
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(dir, "debian-amd64.SHA512SUMS"), signed, 0o644))
	must(os.WriteFile(filepath.Join(dir, "debian-amd64.SHA512SUMS.sign"), sig, 0o644))
	must(os.MkdirAll(filepath.Join(dir, "images"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "images", name), iso, 0o644))
	must(os.WriteFile(filepath.Join(dir, "images", name+".sha512"), signed, 0o644))

	catalog, err := newDebianCatalogHost(t, dir, keys).MediaCatalog(context.Background())
	if err != nil {
		t.Fatalf("MediaCatalog: %v", err)
	}
	var amd64, arm64 *hostapi.MediaDevice
	for i := range catalog.Devices {
		switch catalog.Devices[i].ID {
		case "deb-amd64":
			amd64 = &catalog.Devices[i]
		case "deb-arm64":
			arm64 = &catalog.Devices[i]
		}
	}
	if amd64 == nil || amd64.Image == nil {
		t.Fatalf("deb-amd64 has no image: %+v", amd64)
	}
	img := amd64.Image
	if img.Name != name || img.Arch != "amd64" {
		t.Errorf("image = %q/%q, want %q/amd64", img.Name, img.Arch, name)
	}
	if img.DownloadSize != int64(len(iso)) || img.ExtractSize != int64(len(iso)) {
		t.Errorf("sizes = %d/%d, want %d", img.DownloadSize, img.ExtractSize, len(iso))
	}
	if !img.Cached {
		t.Error("Cached = false for the stored ISO")
	}
	if arm64 == nil || arm64.Image != nil {
		t.Errorf("deb-arm64.image = %+v, want nil without a stored release", arm64)
	}
}

func TestMediaCatalogLeavesDebianImageNilWithoutAStoredRelease(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("media mode is Linux only")
	}
	catalog, err := newDebianCatalogHost(t, t.TempDir(), hostTestKeys(t)).MediaCatalog(context.Background())
	if err != nil {
		t.Fatalf("MediaCatalog: %v", err)
	}
	for _, d := range catalog.Devices {
		if d.Image != nil {
			t.Errorf("device %s has an image offline without any stored data", d.ID)
		}
	}
}

// hostTestKeys erzeugt einen Wegwerfschluessel, mit dem die Tests das
// SHA512SUMS signieren.
func hostTestKeys(t *testing.T) openpgp.EntityList {
	t.Helper()
	entity, err := openpgp.NewEntity("Test Signer", "", "test@example.invalid", nil)
	if err != nil {
		t.Fatalf("NewEntity: %v", err)
	}
	return openpgp.EntityList{entity}
}

// hostTestSignature liefert eine ASCII-geschuetzte losgeloeste Signatur ueber data.
func hostTestSignature(t *testing.T, keys openpgp.EntityList, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, "PGP SIGNATURE", nil)
	if err != nil {
		t.Fatalf("armor.Encode: %v", err)
	}
	if err := openpgp.DetachSign(w, keys[0], bytes.NewReader(data), nil); err != nil {
		t.Fatalf("DetachSign: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close armor: %v", err)
	}
	return buf.Bytes()
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
