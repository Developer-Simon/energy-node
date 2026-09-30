package bundle

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// randomSuffix returns a short random hex string for building unique
// staging paths. Deploy and VerifyRemote may run concurrently -- against
// different nodes from the same operator machine, or (proven while
// validating this plan) as different Go packages' test binaries that
// happen to share one real filesystem via localhost sshd -- and a fixed
// staging name would let one run's cleanup race another's still-in-flight
// upload.
func randomSuffix() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("reading random bytes: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// stagingTag turns remoteDir into a filesystem-safe fragment for embedding
// in a /tmp staging filename alongside randomSuffix. /tmp is a single shared
// namespace -- on a real node across concurrent operator runs, and (proven
// while validating this plan) across this module's own test binaries during
// `go test ./...`, which all stage through localhost sshd onto the same real
// filesystem -- so a cleanup check that globs by a fixed prefix alone can
// match another, unrelated run's still-in-flight upload. Tagging the
// filename with the target remoteDir (always distinct per real deploy target
// and per test) lets a caller scope its own glob to its own staging files.
func stagingTag(remoteDir string) string {
	replacer := strings.NewReplacer("/", "_", " ", "_")
	return replacer.Replace(strings.Trim(remoteDir, "/"))
}

const VerifiedManifestName = ".verified-manifest.json"

// Deploy uploads the archive at localArchivePath to the node and extracts
// it under remoteDir, replacing whatever was there. remoteDir ends up
// holding exactly the layout Vertrag 1 describes -- manifest.json,
// bootstrap/, dashboard/, and so on -- ready for VerifyRemote and the step
// engine.
//
// remoteDir is cleared before extraction rather than merged into: tar
// overwrites files a new archive still has but never deletes ones it
// dropped, so a bootstrap script renamed between bundle versions (sanctioned
// by resolveScriptPath's own "<id>-*.sh" convention) would otherwise leave
// its old file behind, and resolveScriptPath fails outright once two files
// match the same step id. remoteDir is a cache of the last-deployed bundle,
// not state the node depends on between deploys, so clearing it first is
// safe.
//
// Deploy does not itself verify localArchivePath -- callers must run Verify
// (or, once Plan B-II's ExtractArchive exists, Unpack+Verify) against it
// first. VerifyRemote re-checks the deployed bytes, but it does so by
// running the verify_bundle.sh that was just extracted from the very same
// archive, so it cannot by itself catch a bundle whose entire contents,
// script included, were forged together.
func Deploy(ctx context.Context, client *transport.Client, localArchivePath, remoteDir string) error {
	return DeployProgress(ctx, client, localArchivePath, remoteDir, nil)
}

// DeployProgress is Deploy that reports the archive upload's progress
// (bytes sent, archive size); onProgress may be nil.
func DeployProgress(ctx context.Context, client *transport.Client, localArchivePath, remoteDir string, onProgress func(done, total int64)) error {
	remoteArchive := fmt.Sprintf("/tmp/energy-node-installer-bundle-%s-%s.tar.gz", stagingTag(remoteDir), randomSuffix())
	if err := client.UploadFileProgress(localArchivePath, remoteArchive, 0o600, onProgress); err != nil {
		return fmt.Errorf("uploading bundle archive: %w", err)
	}
	defer client.RemoveRemote(remoteArchive)

	command := fmt.Sprintf(
		"rm -rf %s && mkdir -p %s && tar -xzf %s --no-same-owner -C %s",
		transport.ShellQuote(remoteDir),
		transport.ShellQuote(remoteDir),
		transport.ShellQuote(remoteArchive),
		transport.ShellQuote(remoteDir),
	)
	var stdout, stderr bytes.Buffer
	if err := client.Run(ctx, command, &stdout, &stderr); err != nil {
		return fmt.Errorf("extracting bundle on the node: %w (stdout: %q, stderr: %q)", err, stdout.String(), stderr.String())
	}
	return nil
}

// verifyBundleErrorMessage constructs the error message for a verify_bundle.sh
// FEHLER result, including stderr output when available.
func verifyBundleErrorMessage(stderr string) string {
	msg := "verify_bundle.sh rejected the bundle on the node"
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	var nonEmptyLines []string
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			nonEmptyLines = append(nonEmptyLines, trimmed)
		}
	}
	if len(nonEmptyLines) > 0 {
		if len(nonEmptyLines) > 10 {
			nonEmptyLines = nonEmptyLines[:10]
		}
		msg += ": " + strings.Join(nonEmptyLines, ", ")
	}
	return msg
}

// VerifyRemote runs verify_bundle.sh --target against an already-deployed
// bundle: it re-checks signature and hashes on the exact bytes that landed
// on the node, and -- the reason this cannot be a purely local Go check --
// compares the bundle's declared architecture and Python ABI against the
// node's own uname and interpreter. This is the gate AK5 requires: called
// before any bootstrap step runs, a mismatched bundle is rejected before
// anything on the node changes.
//
// This defends against corruption in transit and an architecture/ABI
// mismatch -- it does not by itself defend against a forged bundle, because
// the verify_bundle.sh it runs came from the same unverified archive Deploy
// just extracted. A forged bundle that ships its own always-OK
// verify_bundle.sh would pass this check. The local Verify (or, once Plan
// B-II's ExtractArchive exists, Unpack+Verify) against the embedded signing
// key is what actually defends against tampering; callers must run it
// before Deploy.
func VerifyRemote(ctx context.Context, client *transport.Client, remoteDir string, pubKeyPEM []byte) error {
	remotePubKey := fmt.Sprintf("/tmp/energy-node-installer-pubkey-%s-%s.pem", stagingTag(remoteDir), randomSuffix())
	if err := client.UploadBytes(pubKeyPEM, remotePubKey, 0o600); err != nil {
		return fmt.Errorf("uploading public key: %w", err)
	}
	defer client.RemoveRemote(remotePubKey)

	command := fmt.Sprintf(
		"bash %s --bundle %s --pubkey %s --target",
		transport.ShellQuote(path.Join(remoteDir, "bootstrap", "verify_bundle.sh")),
		transport.ShellQuote(remoteDir),
		transport.ShellQuote(remotePubKey),
	)
	var stdout, stderr bytes.Buffer
	runErr := client.Run(ctx, command, &stdout, &stderr)

	result := LastLine(stdout.String())
	if result == "OK" && runErr == nil {
		return nil
	}
	if code, ok := strings.CutPrefix(result, "FEHLER "); ok {
		return &Error{Code: FaultCode(code), Message: verifyBundleErrorMessage(stderr.String())}
	}
	return fmt.Errorf("verify_bundle.sh failed unexpectedly: %w (stdout: %q, stderr: %q)", runErr, stdout.String(), stderr.String())
}

// VerifyRemoteDev is VerifyRemote's --dev-unsigned counterpart: it runs
// verify_bundle.sh --target-only, which checks architecture and Python ABI
// against the node but skips the signature and hash checks entirely (see
// verify_bundle.sh's own --target-only doc comment) -- there is no key to
// check a signature against, since a developer build made with --dev-
// unsigned never has one. It never uploads a public key, unlike
// VerifyRemote, because none is needed.
func VerifyRemoteDev(ctx context.Context, client *transport.Client, remoteDir string) error {
	command := fmt.Sprintf(
		"bash %s --bundle %s --target-only",
		transport.ShellQuote(path.Join(remoteDir, "bootstrap", "verify_bundle.sh")),
		transport.ShellQuote(remoteDir),
	)
	var stdout, stderr bytes.Buffer
	runErr := client.Run(ctx, command, &stdout, &stderr)

	result := LastLine(stdout.String())
	if result == "OK" && runErr == nil {
		return nil
	}
	if code, ok := strings.CutPrefix(result, "FEHLER "); ok {
		return &Error{Code: FaultCode(code), Message: verifyBundleErrorMessage(stderr.String())}
	}
	return fmt.Errorf("verify_bundle.sh failed unexpectedly: %w (stdout: %q, stderr: %q)", runErr, stdout.String(), stderr.String())
}

// LastLine returns the last non-empty line of s, trimming a trailing
// newline first. verify_bundle.sh and plan.sh both write their machine-
// readable result ("OK" or "FEHLER <CODE>") as the last line of stdout,
// possibly after other diagnostic output -- every caller that parses that
// contract (VerifyRemote here, steps.Preview) uses this same helper so the
// parsing rule cannot drift between them.
func LastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	if idx := strings.LastIndexByte(s, '\n'); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

// ReadVerifiedManifest downloads <remoteBundleDir>/.verified-manifest.json
// and parses it. The file is a copy of manifest.json that MarkVerified writes
// only after verify_bundle.sh accepted the directory, and every transfer
// removes it before changing the directory, so it describes exactly the last
// verified content. Any problem -- the directory has never been verified, the
// file is unreadable, or its JSON is corrupt -- is reported as nil, not an
// error: DeployDelta's caller treats "no trustworthy record" as "do a full
// transfer", exactly as internal/host/host.go's currentSelection already
// does for the same node's selection.json.
func ReadVerifiedManifest(ctx context.Context, client *transport.Client, remoteBundleDir string) *Manifest {
	tmp, err := os.CreateTemp("", "energy-node-installer-verified-manifest-*.json")
	if err != nil {
		return nil
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	if err := client.DownloadFile(path.Join(remoteBundleDir, VerifiedManifestName), tmp.Name()); err != nil {
		return nil
	}
	raw, err := os.ReadFile(tmp.Name())
	if err != nil {
		return nil
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return &m
}

// MarkVerified copies <dir>/manifest.json to <dir>/.verified-manifest.json
// to mark the directory as having passed verification. The operation is
// atomic: the temp file is moved into place after the copy completes.
func MarkVerified(ctx context.Context, client *transport.Client, remoteBundleDir string) error {
	tmpName := VerifiedManifestName + ".tmp"
	command := fmt.Sprintf(
		"cp %s %s && mv -f %s %s",
		transport.ShellQuote(path.Join(remoteBundleDir, "manifest.json")),
		transport.ShellQuote(path.Join(remoteBundleDir, tmpName)),
		transport.ShellQuote(path.Join(remoteBundleDir, tmpName)),
		transport.ShellQuote(path.Join(remoteBundleDir, VerifiedManifestName)),
	)
	var stdout, stderr bytes.Buffer
	if err := client.Run(ctx, command, &stdout, &stderr); err != nil {
		// Best-effort cleanup of temp file
		cleanupCmd := fmt.Sprintf("rm -f %s", transport.ShellQuote(path.Join(remoteBundleDir, tmpName)))
		_ = client.Run(ctx, cleanupCmd, &bytes.Buffer{}, &bytes.Buffer{})
		return fmt.Errorf("MarkVerified: %w (stderr: %q)", err, stderr.String())
	}
	return nil
}

// HashRemoteDir returns a Manifest whose Files maps every regular file under
// the directory (with relpaths using forward slashes, no leading "./") to its
// sha256, EXCLUDING manifest.json, manifest.json.sig, and .verified-manifest.json(.tmp)
// at the top level. A missing directory is not an error: it returns an empty
// Files map. Malformed output or a command error is reported as an error.
func HashRemoteDir(ctx context.Context, client *transport.Client, remoteBundleDir string) (*Manifest, error) {
	command := fmt.Sprintf(
		"if [ -d %s ]; then cd %s && find . -type f ! -path ./manifest.json ! -path ./manifest.json.sig ! -path './.verified-manifest.json*' -print0 | xargs -0 -r sha256sum; fi",
		transport.ShellQuote(remoteBundleDir),
		transport.ShellQuote(remoteBundleDir),
	)
	var stdout, stderr bytes.Buffer
	if err := client.Run(ctx, command, &stdout, &stderr); err != nil {
		return nil, fmt.Errorf("HashRemoteDir: %w (stderr: %q)", err, stderr.String())
	}

	files := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		if line == "" {
			continue
		}
		// Skip lines that start with a backslash (escaped filenames)
		if strings.HasPrefix(line, "\\") {
			continue
		}
		// Parse "<64 hex>  ./rel" strictly
		// sha256sum format: exactly 64 hex chars, two spaces, then filename
		if len(line) <= 66 {
			return nil, fmt.Errorf("HashRemoteDir: malformed sha256sum line: %q", line)
		}
		hash := line[:64]
		// Validate hash is lowercase hex
		if _, err := hex.DecodeString(hash); err != nil {
			return nil, fmt.Errorf("HashRemoteDir: malformed sha256sum line: %q", line)
		}
		// Validate two-space separator
		if line[64:66] != "  " {
			return nil, fmt.Errorf("HashRemoteDir: malformed sha256sum line: %q", line)
		}
		relPath := strings.TrimPrefix(line[66:], "./")
		files[relPath] = hash
	}

	return &Manifest{Files: files}, nil
}

// DeltaBase returns the best available diff base for computing what must
// change in the next incremental transfer. It tries ReadVerifiedManifest first;
// if that returns nil, it falls back to HashRemoteDir; if that errors, it
// returns an empty manifest. source is "verified", "hashed", or "none".
// DeltaBase never returns nil.
func DeltaBase(ctx context.Context, client *transport.Client, remoteBundleDir string) (base *Manifest, source string) {
	if base := ReadVerifiedManifest(ctx, client, remoteBundleDir); base != nil {
		return base, "verified"
	}
	if base, err := HashRemoteDir(ctx, client, remoteBundleDir); err == nil {
		return base, "hashed"
	}
	return &Manifest{Files: map[string]string{}}, "none"
}

// isSafeRelPath rejects a manifest-listed relpath that would escape
// remoteDir. Every relpath DeployDelta acts on comes from a manifest.json
// already verified (signed, or hash-checked by VerifyDev) before it
// reaches here, so this should never actually trigger -- it exists as
// defence in depth, the same reasoning transport.ShellQuote's own callers
// already apply to every remote path they build.
func isSafeRelPath(rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "/") {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

// DeployDelta uploads only the files in changed and deletes only the files
// in removed under remoteDir, leaving every other file there untouched --
// the incremental counterpart to Deploy, which always replaces remoteDir's
// whole contents. bundleDir is the unpacked, already-verified new bundle
// changed's relpaths are read from (bundlesource.Resolved.Dir, or the
// developer CLI's own extracted build directory); removed's relpaths name
// files the *previous* bundle had that the new one no longer does. The diff
// base comes from DeltaBase: it tries the .verified-manifest.json marker
// (if present), falls back to hashing the current remoteDir, and returns an
// empty manifest if neither is available.
//
// manifest.json and manifest.json.sig are always handled by DeployDelta
// even though they are not listed in any manifest's own "files" map:
// scripts/build/lib/manifest.sh never includes them, because the manifest
// itself cannot verify itself. DeployDelta always packs and uploads
// manifest.json (alongside any changed files); if manifest.json.sig exists
// in bundleDir, it packs and uploads that too; if not, it removes any
// manifest.json.sig that might linger on the node from a previous release.
// This ensures a delta transfer leaves the node with a current manifest.
//
// Unlike Deploy, DeployDelta never clears remoteDir first: that is exactly
// what lets it skip re-sending a file whose content did not change between
// bundle versions. It relies on tar creating a file entry's missing parent
// directories on extraction by itself (see PackFiles's own doc comment), so
// a brand-new subdirectory in the new bundle needs no special handling. It
// removes .verified-manifest.json at the start so a new verification must
// run before the next incremental transfer.
func DeployDelta(ctx context.Context, client *transport.Client, bundleDir string, changed, removed []string, remoteDir string, onProgress func(done, total int64)) error {
	// Remove .verified-manifest.json marker as the first remote action,
	// before any other changes, so a transfer is always atomic from the
	// verification perspective.
	if err := client.Run(ctx, "rm -f "+transport.ShellQuote(path.Join(remoteDir, VerifiedManifestName)), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		return fmt.Errorf("removing %s before transfer: %w", VerifiedManifestName, err)
	}

	for _, rel := range changed {
		if !isSafeRelPath(rel) {
			return fmt.Errorf("bundle manifest lists an unsafe path: %q", rel)
		}
	}
	for _, rel := range removed {
		if !isSafeRelPath(rel) {
			return fmt.Errorf("bundle manifest lists an unsafe path: %q", rel)
		}
	}

	toPackPaths := make([]string, len(changed))
	copy(toPackPaths, changed)

	hasManifest := false
	for _, rel := range toPackPaths {
		if rel == "manifest.json" {
			hasManifest = true
			break
		}
	}
	if !hasManifest {
		toPackPaths = append(toPackPaths, "manifest.json")
	}

	sigPath := filepath.Join(bundleDir, "manifest.json.sig")
	toRemove := make([]string, len(removed))
	copy(toRemove, removed)

	if _, err := os.Stat(sigPath); err == nil {
		toPackPaths = append(toPackPaths, "manifest.json.sig")
	} else {
		toRemove = append(toRemove, "manifest.json.sig")
	}

	local, err := os.CreateTemp("", "energy-node-installer-delta-*.tar.gz")
	if err != nil {
		return fmt.Errorf("creating a delta archive: %w", err)
	}
	local.Close()
	defer os.Remove(local.Name())
	if err := PackFiles(bundleDir, toPackPaths, local.Name()); err != nil {
		return fmt.Errorf("packing changed files: %w", err)
	}

	remoteArchive := fmt.Sprintf("/tmp/energy-node-installer-delta-%s-%s.tar.gz", stagingTag(remoteDir), randomSuffix())
	if err := client.UploadFileProgress(local.Name(), remoteArchive, 0o600, onProgress); err != nil {
		return fmt.Errorf("uploading delta archive: %w", err)
	}
	defer client.RemoveRemote(remoteArchive)

	command := fmt.Sprintf(
		"mkdir -p %s && tar -xzf %s --no-same-owner -C %s",
		transport.ShellQuote(remoteDir),
		transport.ShellQuote(remoteArchive),
		transport.ShellQuote(remoteDir),
	)
	var stdout, stderr bytes.Buffer
	if err := client.Run(ctx, command, &stdout, &stderr); err != nil {
		return fmt.Errorf("extracting delta archive on the node: %w (stdout: %q, stderr: %q)", err, stdout.String(), stderr.String())
	}

	if len(toRemove) > 0 {
		var cmd strings.Builder
		cmd.WriteString("rm -f")
		for _, rel := range toRemove {
			cmd.WriteString(" ")
			cmd.WriteString(transport.ShellQuote(path.Join(remoteDir, rel)))
		}
		var stdout, stderr bytes.Buffer
		if err := client.Run(ctx, cmd.String(), &stdout, &stderr); err != nil {
			return fmt.Errorf("removing files dropped from the bundle: %w (stdout: %q, stderr: %q)", err, stdout.String(), stderr.String())
		}
	}
	return nil
}
