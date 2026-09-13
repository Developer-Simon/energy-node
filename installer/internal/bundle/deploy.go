package bundle

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path"
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
	remoteArchive := fmt.Sprintf("/tmp/energy-node-installer-bundle-%s-%s.tar.gz", stagingTag(remoteDir), randomSuffix())
	if err := client.UploadFile(localArchivePath, remoteArchive, 0o600); err != nil {
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
		return &Error{Code: FaultCode(code), Message: "verify_bundle.sh rejected the bundle on the node"}
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
