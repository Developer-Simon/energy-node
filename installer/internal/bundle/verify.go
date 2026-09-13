package bundle

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

//go:embed signing_key.pub.pem
var embeddedPublicKeyPEM []byte

// EmbeddedPublicKey parses the ed25519 public key compiled into this
// binary. The matching private key lives only in the release job's CI
// secret (E13) and never touches this repository.
func EmbeddedPublicKey() (ed25519.PublicKey, error) {
	return ParsePublicKeyPEM(embeddedPublicKeyPEM)
}

// ParsePublicKeyPEM decodes a PEM-encoded SubjectPublicKeyInfo block -- the
// format `openssl pkey -pubout` writes -- into an ed25519 public key.
func ParsePublicKeyPEM(pemBytes []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in public key")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing public key: %w", err)
	}
	key, ok := pub.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is %T, not ed25519", pub)
	}
	return key, nil
}

// VerifySignature checks manifest.json.sig against manifest.json's exact
// bytes. ed25519 signs the raw message rather than a digest of it -- the
// same reason verify_bundle.sh insists on openssl's -rawin flag.
func VerifySignature(bundleDir string, pubKey ed25519.PublicKey) error {
	manifestPath := filepath.Join(bundleDir, "manifest.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return &Error{Code: FaultManifestMissing, Message: manifestPath + " does not exist"}
	}
	sigPath := manifestPath + ".sig"
	sig, err := os.ReadFile(sigPath)
	if err != nil {
		return &Error{Code: FaultSignatureInvalid, Message: sigPath + " does not exist"}
	}
	if !ed25519.Verify(pubKey, manifestBytes, sig) {
		return &Error{Code: FaultSignatureInvalid, Message: "signature does not match manifest.json"}
	}
	return nil
}

// VerifyFileHashes checks every file manifest.Files lists against its
// SHA-256 sum on disk. A file present in the bundle but absent from the
// manifest is not an error -- the manifest is authoritative about what must
// match, not about what may exist.
func VerifyFileHashes(bundleDir string, manifest *Manifest) error {
	names := make([]string, 0, len(manifest.Files))
	for name := range manifest.Files {
		names = append(names, name)
	}
	sort.Strings(names) // deterministic order for a reproducible first-failure message

	for _, rel := range names {
		want := manifest.Files[rel]
		path := filepath.Join(bundleDir, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if err != nil {
			return &Error{Code: FaultHashMismatch, Message: fmt.Sprintf("%s: %v", rel, err)}
		}
		got := sha256.Sum256(data)
		if hex.EncodeToString(got[:]) != want {
			return &Error{Code: FaultHashMismatch, Message: rel + " does not match its recorded hash"}
		}
	}
	return nil
}

// Verify runs the full local integrity check on an already-unpacked bundle,
// in the order E13 requires: load the manifest, verify its signature, and
// only then trust the file list enough to check hashes against it.
// Checking hashes first would mean checking them against a list an
// attacker wrote.
func Verify(bundleDir string, pubKey ed25519.PublicKey) (*Manifest, error) {
	manifest, err := LoadManifest(bundleDir)
	if err != nil {
		return nil, err
	}
	if err := VerifySignature(bundleDir, pubKey); err != nil {
		return nil, err
	}
	if err := VerifyFileHashes(bundleDir, manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}
