package media

import (
	"bytes"
	"errors"
	"fmt"
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
