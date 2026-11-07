package host

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/Developer-Simon/energy-node-installer/internal/bundlesource"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// stagePackages is the seam for putting pinned files on the node. Tests
// replace it; production runs defaultStagePackages.
var stagePackages = defaultStagePackages

// defaultStagePackages makes remoteDir hold exactly the files pkgs names: a
// file whose SHA-256 on the node already matches the pin is left alone,
// a missing or differing one is uploaded, and every other file in remoteDir
// is removed. Only the top level of remoteDir is touched.
func defaultStagePackages(ctx context.Context, client *transport.Client, remoteDir string, pkgs []bundlesource.LocalPackage, progress func(done, total int64)) error {
	quoted := transport.ShellQuote(remoteDir)
	listing := fmt.Sprintf(
		"mkdir -p %[1]s && chmod 0755 %[1]s && cd %[1]s && find . -maxdepth 1 -type f -print0 | xargs -0 -r sha256sum",
		quoted)
	var stdout, stderr strings.Builder
	if err := client.Run(ctx, listing, &stdout, &stderr); err != nil {
		return fmt.Errorf("listing %s: %w (stderr: %s)", remoteDir, err, stderr.String())
	}
	present, err := parseRemoteSums(stdout.String())
	if err != nil {
		return fmt.Errorf("listing %s: %w", remoteDir, err)
	}

	want := make(map[string]string, len(pkgs))
	for _, p := range pkgs {
		want[p.File] = p.SHA256
	}

	var stale []string
	for name := range present {
		if _, ok := want[name]; !ok {
			stale = append(stale, transport.ShellQuote(name))
		}
	}
	if len(stale) > 0 {
		remove := fmt.Sprintf("cd %s && rm -f -- %s", quoted, strings.Join(stale, " "))
		if err := client.Run(ctx, remove, &strings.Builder{}, &strings.Builder{}); err != nil {
			return fmt.Errorf("removing stale files in %s: %w", remoteDir, err)
		}
	}

	var upload []bundlesource.LocalPackage
	var total int64
	for _, p := range pkgs {
		if present[p.File] == p.SHA256 {
			continue
		}
		info, err := os.Stat(p.Path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", p.Path, err)
		}
		upload = append(upload, p)
		total += info.Size()
	}

	var offset int64
	for _, p := range upload {
		if err := ctx.Err(); err != nil {
			return err
		}
		base := offset
		var onProgress func(done, total int64)
		if progress != nil {
			onProgress = func(done, _ int64) { progress(base+done, total) }
		}
		if err := client.UploadFileProgress(p.Path, path.Join(remoteDir, p.File), 0o644, onProgress); err != nil {
			return fmt.Errorf("uploading %s: %w", p.File, err)
		}
		if info, err := os.Stat(p.Path); err == nil {
			offset += info.Size()
		}
	}
	return nil
}

// parseRemoteSums reads sha256sum output ("<hash>  ./<name>" per line) into
// a map from file name to hash. Escaped lines, which start with a backslash,
// are skipped the same way bundle.HashRemoteDir skips them.
func parseRemoteSums(output string) (map[string]string, error) {
	sums := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" || strings.HasPrefix(line, "\\") {
			continue
		}
		if len(line) <= 66 || line[64:66] != "  " {
			return nil, fmt.Errorf("malformed sha256sum line: %q", line)
		}
		sums[strings.TrimPrefix(line[66:], "./")] = line[:64]
	}
	return sums, nil
}
