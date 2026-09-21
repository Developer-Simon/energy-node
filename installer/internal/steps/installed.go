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
func RecordInstalled(ctx context.Context, client *transport.Client, remoteBundleDir, remoteStateDir string) error {
	src := transport.ShellQuote(path.Join(remoteBundleDir, "manifest.json"))
	dst := path.Join(remoteStateDir, "installed-manifest.json")
	tmp := transport.ShellQuote(dst + ".tmp")
	command := fmt.Sprintf("cp %s %s && mv -f %s %s || { rm -f %s; exit 1; }",
		src, tmp, tmp, transport.ShellQuote(dst), tmp)

	var stderr bytes.Buffer
	if err := client.Run(ctx, command, io.Discard, &stderr); err != nil {
		return fmt.Errorf("recording installed-manifest.json: %w (stderr: %s)", err, stderr.String())
	}
	return nil
}
