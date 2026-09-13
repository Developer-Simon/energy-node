// Package bundle reads and verifies an unpacked installer bundle: the
// manifest, its ed25519 signature, the per-file SHA-256 hashes and the
// target architecture/ABI check -- the same three checks
// scripts/bootstrap/verify_bundle.sh performs, done natively in Go so the
// operator's machine never needs openssl or bash (Windows has neither).
// See .docs/superpowers/specs/2026-09-05-installationsanwendung-design.md,
// E13.
package bundle

// FaultCode identifies a bundle verification failure. Every value here is
// stable and shared with the bootstrap side: verify_bundle.sh prints the
// identical string after "FEHLER ". internal/faults (Plan B-II) maps each
// one to operator-facing text; until then, the code itself is the only
// output.
type FaultCode string

const (
	FaultManifestMissing   FaultCode = "BUNDLE_MANIFEST_MISSING"
	FaultSignatureInvalid  FaultCode = "BUNDLE_SIGNATURE_INVALID"
	FaultHashMismatch      FaultCode = "BUNDLE_HASH_MISMATCH"
	FaultArchMismatch      FaultCode = "ARCH_MISMATCH"
	FaultPythonABIMismatch FaultCode = "PYTHON_ABI_MISMATCH"
)

// Error is returned by every verification function in this package.
type Error struct {
	Code    FaultCode
	Message string
}

func (e *Error) Error() string {
	return string(e.Code) + ": " + e.Message
}
