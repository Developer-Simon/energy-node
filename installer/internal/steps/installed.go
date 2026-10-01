package steps

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"

	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// RecordInstalled marks the deployed bundle as fully applied by copying its
// manifest.json to <remoteStateDir>/installed-manifest.json. plan.sh reads
// that copy as the "von" side of every component, so callers must only call
// it after a run in which every step of the manifest succeeded -- never after
// a failed or single-step run, which would make the next preview lie.
//
// The copy goes through a .tmp file and mv so an interrupted connection
// never leaves a half-written file behind. The state directory belongs to
// the connecting user (provisionRemoteStateDir), so no sudo is needed.
// When the bundle carries a changelog.json it is copied next to it the same way (best effort) so the dashboard can show what the installed version contains.
func RecordInstalled(ctx context.Context, client *transport.Client, remoteBundleDir, remoteStateDir string) error {
	src := transport.ShellQuote(path.Join(remoteBundleDir, "manifest.json"))
	dst := path.Join(remoteStateDir, "installed-manifest.json")
	tmp := transport.ShellQuote(dst + ".tmp")

	logSrc := transport.ShellQuote(path.Join(remoteBundleDir, "changelog.json"))
	logDst := path.Join(remoteStateDir, "changelog.json")
	logTmp := transport.ShellQuote(logDst + ".tmp")

	// The manifest copy is the record and must succeed (exit 1 otherwise). The
	// changelog copy that follows is best effort: an older bundle has no
	// changelog.json, and a missing changelog must never turn a fully applied
	// run into a failure.
	command := fmt.Sprintf(
		"cp %s %s && mv -f %s %s || { rm -f %s; exit 1; }; "+
			"if [ -f %s ]; then cp %s %s && mv -f %s %s || rm -f %s; fi",
		src, tmp, tmp, transport.ShellQuote(dst), tmp,
		logSrc, logSrc, logTmp, logTmp, transport.ShellQuote(logDst), logTmp)

	var stderr bytes.Buffer
	if err := client.Run(ctx, command, io.Discard, &stderr); err != nil {
		return fmt.Errorf("recording installed-manifest.json: %w (stderr: %s)", err, stderr.String())
	}
	return nil
}

// ReadInstalledManifest returns the installed-manifest.json copy from the node's
// state directory -- the "from" side of every component. It returns nil, nil
// when there is no copy yet (a node the installer has never finished a run on),
// which callers treat as "nothing installed", not as an error.
func ReadInstalledManifest(ctx context.Context, client *transport.Client, remoteStateDir string) ([]byte, error) {
	target := transport.ShellQuote(path.Join(remoteStateDir, "installed-manifest.json"))
	command := fmt.Sprintf("if [ -f %s ]; then cat %s; fi", target, target)

	var stdout, stderr bytes.Buffer
	if err := client.Run(ctx, command, &stdout, &stderr); err != nil {
		return nil, fmt.Errorf("reading installed-manifest.json: %w (stderr: %s)", err, stderr.String())
	}
	if stdout.Len() == 0 {
		return nil, nil
	}
	return stdout.Bytes(), nil
}
