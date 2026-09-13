package bundle_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
)

func writeFileHash(t *testing.T, dir, rel, content string) string {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func newTestBundle(t *testing.T) (dir string, pub ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	dir = t.TempDir()
	hash := writeFileHash(t, dir, "bootstrap/10-apt.sh", "echo hallo\n")
	manifest := `{"version":"v0.2.0","files":{"bootstrap/10-apt.sh":"` + hash + `"}}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("writing manifest.json: %v", err)
	}
	sig := ed25519.Sign(priv, []byte(manifest))
	if err := os.WriteFile(filepath.Join(dir, "manifest.json.sig"), sig, 0o644); err != nil {
		t.Fatalf("writing manifest.json.sig: %v", err)
	}
	return dir, pub
}

func assertFaultCode(t *testing.T, err error, want bundle.FaultCode) {
	t.Helper()
	var bundleErr *bundle.Error
	if !errors.As(err, &bundleErr) {
		t.Fatalf("expected a *bundle.Error, got %v (%T)", err, err)
	}
	if bundleErr.Code != want {
		t.Fatalf("expected fault code %s, got %s", want, bundleErr.Code)
	}
}

func TestVerifyAcceptsAnIntactBundle(t *testing.T) {
	dir, pub := newTestBundle(t)
	manifest, err := bundle.Verify(dir, pub)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if manifest.Version != "v0.2.0" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
}

func TestVerifyRejectsATamperedFile(t *testing.T) {
	dir, pub := newTestBundle(t)
	if err := os.WriteFile(filepath.Join(dir, "bootstrap", "10-apt.sh"), []byte("echo boese\n"), 0o644); err != nil {
		t.Fatalf("tampering: %v", err)
	}
	_, err := bundle.Verify(dir, pub)
	assertFaultCode(t, err, bundle.FaultHashMismatch)
}

func TestVerifyRejectsAForgedManifestBeforeCheckingHashes(t *testing.T) {
	dir, pub := newTestBundle(t)

	// An attacker who can rewrite the bundle can tamper with the file AND
	// the manifest's recorded hash for it -- everything except the
	// signature, which needs the private key. If hashes were checked
	// before the signature, this bundle would sail through: the forged
	// manifest and the tampered file agree perfectly with each other.
	tampered := "echo boese\n"
	if err := os.WriteFile(filepath.Join(dir, "bootstrap", "10-apt.sh"), []byte(tampered), 0o644); err != nil {
		t.Fatalf("tampering with the file: %v", err)
	}
	sum := sha256.Sum256([]byte(tampered))
	forged := `{"version":"v0.2.0","files":{"bootstrap/10-apt.sh":"` + hex.EncodeToString(sum[:]) + `"}}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(forged), 0o644); err != nil {
		t.Fatalf("forging manifest.json: %v", err)
	}
	// manifest.json.sig is left untouched: it still signs the ORIGINAL
	// manifest.json, not this forged one.

	_, err := bundle.Verify(dir, pub)
	assertFaultCode(t, err, bundle.FaultSignatureInvalid)
}

func TestVerifyRejectsAMissingSignature(t *testing.T) {
	dir, pub := newTestBundle(t)
	if err := os.Remove(filepath.Join(dir, "manifest.json.sig")); err != nil {
		t.Fatalf("removing signature: %v", err)
	}
	_, err := bundle.Verify(dir, pub)
	assertFaultCode(t, err, bundle.FaultSignatureInvalid)
}

func TestVerifyRejectsAMissingManifest(t *testing.T) {
	_, err := bundle.Verify(t.TempDir(), ed25519.PublicKey{})
	assertFaultCode(t, err, bundle.FaultManifestMissing)
}

func TestVerifyInteroperatesWithOpenSSLSignatures(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl not installed")
	}
	dir := t.TempDir()
	hash := writeFileHash(t, dir, "bootstrap/10-apt.sh", "echo hallo\n")
	manifest := `{"version":"v0.2.0","files":{"bootstrap/10-apt.sh":"` + hash + `"}}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("writing manifest.json: %v", err)
	}

	keyPath := filepath.Join(dir, "key.pem")
	pubPath := filepath.Join(dir, "pub.pem")
	run(t, "openssl", "genpkey", "-algorithm", "ed25519", "-out", keyPath)
	run(t, "openssl", "pkey", "-in", keyPath, "-pubout", "-out", pubPath)
	run(t, "openssl", "pkeyutl", "-sign", "-inkey", keyPath, "-rawin",
		"-in", filepath.Join(dir, "manifest.json"), "-out", filepath.Join(dir, "manifest.json.sig"))

	pubPEM, err := os.ReadFile(pubPath)
	if err != nil {
		t.Fatalf("reading pub.pem: %v", err)
	}
	pub, err := bundle.ParsePublicKeyPEM(pubPEM)
	if err != nil {
		t.Fatalf("ParsePublicKeyPEM: %v", err)
	}

	// This is the cross-tool check that matters: Plan A-II signs with
	// `openssl pkeyutl -sign -rawin`, and this Go code must accept exactly
	// what that produces -- both implement plain Ed25519 over the raw
	// message, but only a real round trip proves the encodings line up.
	if _, err := bundle.Verify(dir, pub); err != nil {
		t.Fatalf("Go must accept a signature openssl produced: %v", err)
	}
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func TestEmbeddedPublicKeyParses(t *testing.T) {
	if _, err := bundle.EmbeddedPublicKey(); err != nil {
		t.Fatalf("EmbeddedPublicKey: %v", err)
	}
}

func TestVerifyDevAcceptsAnUnsignedIntactBundle(t *testing.T) {
	dir, _ := newTestBundle(t)
	if err := os.Remove(filepath.Join(dir, "manifest.json.sig")); err != nil {
		t.Fatalf("removing signature: %v", err)
	}
	manifest, err := bundle.VerifyDev(dir)
	if err != nil {
		t.Fatalf("VerifyDev: %v", err)
	}
	if manifest.Version != "v0.2.0" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
}

func TestVerifyDevStillRejectsATamperedFile(t *testing.T) {
	dir, _ := newTestBundle(t)
	if err := os.WriteFile(filepath.Join(dir, "bootstrap", "10-apt.sh"), []byte("echo boese\n"), 0o644); err != nil {
		t.Fatalf("tampering: %v", err)
	}
	_, err := bundle.VerifyDev(dir)
	assertFaultCode(t, err, bundle.FaultHashMismatch)
}

func TestVerifyDevRejectsAMissingManifest(t *testing.T) {
	_, err := bundle.VerifyDev(t.TempDir())
	assertFaultCode(t, err, bundle.FaultManifestMissing)
}
