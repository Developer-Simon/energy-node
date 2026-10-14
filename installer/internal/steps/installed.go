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
// The per-version files under changelog/ are added or replaced in the state directory the same way (best effort), and existing files are never deleted, so the node keeps every version it received.
// Before the copy, a differing installed version is kept as previous-manifest.json (best effort), the "von" side of the dashboard's what's new page.
func RecordInstalled(ctx context.Context, client *transport.Client, remoteBundleDir, remoteStateDir string) error {
	src := transport.ShellQuote(path.Join(remoteBundleDir, "manifest.json"))
	dst := path.Join(remoteStateDir, "installed-manifest.json")
	tmp := transport.ShellQuote(dst + ".tmp")

	logSrc := transport.ShellQuote(path.Join(remoteBundleDir, "changelog.json"))
	logDst := path.Join(remoteStateDir, "changelog.json")
	logTmp := transport.ShellQuote(logDst + ".tmp")

	prev := transport.ShellQuote(path.Join(remoteStateDir, "previous-manifest.json"))
	prevTmp := transport.ShellQuote(path.Join(remoteStateDir, "previous-manifest.json.tmp"))
	quotedDst := transport.ShellQuote(dst)
	// python3 is on every node (step 10 installs it). A failing read leaves the
	// version empty and skips the copy, which only costs the "von" side.
	versionOf := func(file string) string {
		return `python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("version",""))' ` + file + ` 2>/dev/null`
	}
	keepPrevious := "if [ -f " + quotedDst + " ]; then " +
		"old=$(" + versionOf(quotedDst) + "); new=$(" + versionOf(src) + "); " +
		"if [ -n \"$old\" ] && [ \"$old\" != \"$new\" ]; then " +
		"cp " + quotedDst + " " + prevTmp + " && mv -f " + prevTmp + " " + prev + " || rm -f " + prevTmp + "; fi; fi; "

	// The manifest copy is the record and must succeed (exit 1 otherwise). The
	// changelog copy that follows is best effort: an older bundle has no
	// changelog.json, and a missing changelog must never turn a fully applied
	// run into a failure. The same holds for the per-version files: the
	// fragment ends with "|| true" so it cannot change the exit status.
	command := keepPrevious + fmt.Sprintf(
		"cp %s %s && mv -f %s %s || { rm -f %s; exit 1; }; "+
			"if [ -f %s ]; then cp %s %s && mv -f %s %s || rm -f %s; fi",
		src, tmp, tmp, transport.ShellQuote(dst), tmp,
		logSrc, logSrc, logTmp, logTmp, transport.ShellQuote(logDst), logTmp)
	command += "; " + versionedChangelogFragment(
		transport.ShellQuote(path.Join(remoteBundleDir, "changelog")),
		transport.ShellQuote(path.Join(remoteStateDir, "changelog")))

	var stderr bytes.Buffer
	if err := client.Run(ctx, command, io.Discard, &stderr); err != nil {
		return fmt.Errorf("recording installed-manifest.json: %w (stderr: %s)", err, stderr.String())
	}
	return nil
}

// versionedChangelogFragment copies every bundle changelog/<version>.json that
// is new or differs from the node's copy into the state directory's changelog/
// directory. Files only the node has are kept. Both arguments are already
// shell-quoted directory paths.
func versionedChangelogFragment(bundleDir, stateDir string) string {
	return "if [ -d " + bundleDir + " ]; then " +
		"mkdir -p " + stateDir + " && " +
		"for f in " + bundleDir + "/*.json; do " +
		"[ -f \"$f\" ] || continue; " +
		"d=" + stateDir + "/\"$(basename \"$f\")\"; " +
		"cmp -s \"$f\" \"$d\" && continue; " +
		"cp \"$f\" \"$d.tmp\" && mv -f \"$d.tmp\" \"$d\" || rm -f \"$d.tmp\"; " +
		"done; fi || true"
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
