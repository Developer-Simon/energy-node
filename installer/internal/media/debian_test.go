package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
)

// newTestSigner creates a throwaway OpenPGP key for signing test fixtures.
func newTestSigner(t *testing.T, name string) openpgp.EntityList {
	t.Helper()
	entity, err := openpgp.NewEntity(name, "", name+"@example.invalid", nil)
	if err != nil {
		t.Fatalf("NewEntity: %v", err)
	}
	return openpgp.EntityList{entity}
}

// armoredSignature returns an ASCII-armored detached signature over data.
func armoredSignature(t *testing.T, signer openpgp.EntityList, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, "PGP SIGNATURE", nil)
	if err != nil {
		t.Fatalf("armor.Encode: %v", err)
	}
	if err := openpgp.DetachSign(w, signer[0], bytes.NewReader(data), nil); err != nil {
		t.Fatalf("DetachSign: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close armor: %v", err)
	}
	return buf.Bytes()
}

func readDebianFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "debian", "SHA512SUMS"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func TestEmbeddedKeyHasTheDebianCDFingerprint(t *testing.T) {
	keys, err := DebianKey()
	if err != nil {
		t.Fatalf("DebianKey: %v", err)
	}
	for _, entity := range keys {
		if fmt.Sprintf("%X", entity.PrimaryKey.Fingerprint) == DebianKeyFingerprint {
			return
		}
	}
	t.Fatalf("no embedded key with fingerprint %s", DebianKeyFingerprint)
}

func TestParseSHA512SUMSAcceptsAGoodSignature(t *testing.T) {
	signer := newTestSigner(t, "Good Signer")
	signed := readDebianFixture(t)
	sig := armoredSignature(t, signer, signed)

	release, err := ParseSHA512SUMS(signed, sig, signer, "amd64")
	if err != nil {
		t.Fatalf("ParseSHA512SUMS: %v", err)
	}
	if release.Arch != "amd64" {
		t.Errorf("Arch = %q, want amd64", release.Arch)
	}
	if release.Name != "debian-13.7.0-amd64-netinst.iso" {
		t.Errorf("Name = %q", release.Name)
	}
	wantURL := DebianCDURL + "/amd64/iso-cd/debian-13.7.0-amd64-netinst.iso"
	if release.URL != wantURL {
		t.Errorf("URL = %q, want %q", release.URL, wantURL)
	}
	if release.SHA512 != strings.Repeat("a", 128) {
		t.Errorf("SHA512 = %q", release.SHA512)
	}
}

func TestParseSHA512SUMSRejectsATamperedFile(t *testing.T) {
	signer := newTestSigner(t, "Good Signer")
	signed := readDebianFixture(t)
	sig := armoredSignature(t, signer, signed)

	tampered := bytes.Replace(signed, []byte("aaaa"), []byte("aaab"), 1)
	if bytes.Equal(tampered, signed) {
		t.Fatal("fixture change did not apply")
	}
	_, err := ParseSHA512SUMS(tampered, sig, signer, "amd64")
	if !errors.Is(err, ErrSignature) {
		t.Fatalf("err = %v, want ErrSignature", err)
	}
}

func TestParseSHA512SUMSRejectsAWrongKey(t *testing.T) {
	signer := newTestSigner(t, "Signer One")
	other := newTestSigner(t, "Signer Two")
	signed := readDebianFixture(t)
	sig := armoredSignature(t, signer, signed)

	_, err := ParseSHA512SUMS(signed, sig, other, "amd64")
	if !errors.Is(err, ErrSignature) {
		t.Fatalf("err = %v, want ErrSignature", err)
	}
}

func TestParseSHA512SUMSPicksTheRequestedArch(t *testing.T) {
	signer := newTestSigner(t, "Good Signer")
	signed := readDebianFixture(t)
	sig := armoredSignature(t, signer, signed)

	release, err := ParseSHA512SUMS(signed, sig, signer, "arm64")
	if err != nil {
		t.Fatalf("ParseSHA512SUMS: %v", err)
	}
	if release.Name != "debian-13.7.0-arm64-netinst.iso" {
		t.Errorf("Name = %q", release.Name)
	}
	if release.SHA512 != strings.Repeat("c", 128) {
		t.Errorf("SHA512 = %q, want the arm64 hash", release.SHA512)
	}
	if !strings.Contains(release.URL, "/arm64/iso-cd/") {
		t.Errorf("URL = %q, want the arm64 directory", release.URL)
	}
}

func TestParseSHA512SUMSWithoutANetinstLineIsNoRelease(t *testing.T) {
	signer := newTestSigner(t, "Good Signer")
	signed := []byte(strings.Repeat("e", 128) + "  debian-13.7.0-s390x-netinst.iso\n" +
		strings.Repeat("f", 128) + "  debian-13.7.0-amd64-DVD-1.iso\n")
	sig := armoredSignature(t, signer, signed)

	_, err := ParseSHA512SUMS(signed, sig, signer, "amd64")
	if !errors.Is(err, ErrNoRelease) {
		t.Fatalf("err = %v, want ErrNoRelease", err)
	}
}

// debianServer serves a Debian CD directory for amd64 with the given signed
// SHA512SUMS and signature, plus a netinst ISO of isoSize bytes for HEAD.
func debianServer(t *testing.T, signed, sig []byte, isoSize int64) *httptest.Server {
	t.Helper()
	const iso = "/amd64/iso-cd/debian-13.7.0-amd64-netinst.iso"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/amd64/iso-cd/SHA512SUMS":
			w.Write(signed)
		case "/amd64/iso-cd/SHA512SUMS.sign":
			w.Write(sig)
		case iso:
			w.Header().Set("Content-Length", fmt.Sprint(isoSize))
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDebianReleaseStoresTheSignedFilesAndFallsBackOffline(t *testing.T) {
	signer := newTestSigner(t, "Good Signer")
	signed := readDebianFixture(t)
	sig := armoredSignature(t, signer, signed)
	srv := debianServer(t, signed, sig, 4321)
	dir := t.TempDir()
	c := &Cache{Dir: dir, DebianURL: srv.URL, DebianKey: signer}

	release, offline, err := c.DebianRelease(context.Background(), "amd64")
	if err != nil {
		t.Fatalf("DebianRelease: %v", err)
	}
	if offline {
		t.Error("offline = true with a live server")
	}
	if release.Name != "debian-13.7.0-amd64-netinst.iso" || release.SHA512 != strings.Repeat("a", 128) {
		t.Errorf("release = %+v", release)
	}
	if want := srv.URL + "/amd64/iso-cd/debian-13.7.0-amd64-netinst.iso"; release.URL != want {
		t.Errorf("URL = %q, want %q", release.URL, want)
	}
	if release.Size != 4321 {
		t.Errorf("Size = %d, want 4321", release.Size)
	}
	for _, name := range []string{"debian-amd64.SHA512SUMS", "debian-amd64.SHA512SUMS.sign"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s not stored: %v", name, err)
		}
	}

	srv.Close()
	again, offline, err := c.DebianRelease(context.Background(), "amd64")
	if err != nil {
		t.Fatalf("offline DebianRelease: %v", err)
	}
	if !offline {
		t.Error("offline = false without a server")
	}
	if again.Name != release.Name || again.SHA512 != release.SHA512 {
		t.Errorf("offline release = %+v, want %+v", again, release)
	}
	if again.Size != 0 {
		t.Errorf("offline Size = %d, want 0 without a cached ISO", again.Size)
	}
}

func TestDebianReleaseOfflineUsesTheCachedISOSize(t *testing.T) {
	signer := newTestSigner(t, "Good Signer")
	signed := readDebianFixture(t)
	sig := armoredSignature(t, signer, signed)
	srv := debianServer(t, signed, sig, 4321)
	dir := t.TempDir()
	c := &Cache{Dir: dir, DebianURL: srv.URL, DebianKey: signer}
	if _, _, err := c.DebianRelease(context.Background(), "amd64"); err != nil {
		t.Fatalf("DebianRelease: %v", err)
	}

	iso := filepath.Join(dir, imagesDir, "debian-13.7.0-amd64-netinst.iso")
	if err := os.MkdirAll(filepath.Dir(iso), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(iso, make([]byte, 77), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	release, offline, err := c.DebianRelease(context.Background(), "amd64")
	if err != nil || !offline {
		t.Fatalf("offline = %v, err = %v", offline, err)
	}
	if release.Size != 77 {
		t.Errorf("Size = %d, want the cached ISO size 77", release.Size)
	}
}

func TestDebianReleaseOfflineRejectsAModifiedStoredCopy(t *testing.T) {
	signer := newTestSigner(t, "Good Signer")
	signed := readDebianFixture(t)
	sig := armoredSignature(t, signer, signed)
	srv := debianServer(t, signed, sig, 4321)
	dir := t.TempDir()
	c := &Cache{Dir: dir, DebianURL: srv.URL, DebianKey: signer}
	if _, _, err := c.DebianRelease(context.Background(), "amd64"); err != nil {
		t.Fatalf("DebianRelease: %v", err)
	}

	stored := filepath.Join(dir, "debian-amd64.SHA512SUMS")
	data, err := os.ReadFile(stored)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte("aaaa"), []byte("aaab"), 1)
	if err := os.WriteFile(stored, data, 0o644); err != nil {
		t.Fatal(err)
	}
	srv.Close()

	_, offline, err := c.DebianRelease(context.Background(), "amd64")
	if !errors.Is(err, ErrSignature) {
		t.Fatalf("err = %v, want ErrSignature", err)
	}
	if offline {
		t.Error("offline = true on a rejected copy")
	}
}

func TestDebianReleaseBadSignatureStoresNothing(t *testing.T) {
	signer := newTestSigner(t, "Good Signer")
	other := newTestSigner(t, "Other Signer")
	signed := readDebianFixture(t)
	sig := armoredSignature(t, other, signed)
	srv := debianServer(t, signed, sig, 4321)
	dir := filepath.Join(t.TempDir(), "media")
	c := &Cache{Dir: dir, DebianURL: srv.URL, DebianKey: signer}

	_, _, err := c.DebianRelease(context.Background(), "amd64")
	if !errors.Is(err, ErrSignature) {
		t.Fatalf("err = %v, want ErrSignature", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("cache dir exists after a bad signature: %v", err)
	}
}
