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

// Deploy uploads the archive at localArchivePath to the node and extracts
// it under remoteDir, which is created if needed. remoteDir ends up holding
// exactly the layout Vertrag 1 describes -- manifest.json, bootstrap/,
// dashboard/, and so on -- ready for VerifyRemote and the step engine.
func Deploy(ctx context.Context, client *transport.Client, localArchivePath, remoteDir string) error {
	remoteArchive := fmt.Sprintf("/tmp/energy-node-installer-bundle-%s.tar.gz", randomSuffix())
	if err := client.UploadFile(localArchivePath, remoteArchive, 0o600); err != nil {
		return fmt.Errorf("uploading bundle archive: %w", err)
	}
	defer client.RemoveRemote(remoteArchive)

	command := fmt.Sprintf(
		"mkdir -p %s && tar -xzf %s -C %s",
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
func VerifyRemote(ctx context.Context, client *transport.Client, remoteDir string, pubKeyPEM []byte) error {
	remotePubKey := fmt.Sprintf("/tmp/energy-node-installer-pubkey-%s.pem", randomSuffix())
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

	result := lastLine(stdout.String())
	if result == "OK" && runErr == nil {
		return nil
	}
	if code, ok := strings.CutPrefix(result, "FEHLER "); ok {
		return &Error{Code: FaultCode(code), Message: "verify_bundle.sh rejected the bundle on the node"}
	}
	return fmt.Errorf("verify_bundle.sh failed unexpectedly: %w (stdout: %q, stderr: %q)", runErr, stdout.String(), stderr.String())
}

func lastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	if idx := strings.LastIndexByte(s, '\n'); idx >= 0 {
		return s[idx+1:]
	}
	return s
}
