package bundle

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BuildArgs configures a repo-mode build. Field names match make_bundle.sh's
// own flags (Plan A-II, Task 19) exactly; this package does not validate
// them a second time -- make_bundle.sh already does, and reports its own
// errors on stderr.
type BuildArgs struct {
	RepoRoot    string
	Arch        string // "armv6", "arm64" or "amd64"
	PythonMinor string // e.g. "3.11"; omit to let make_bundle.sh choose
	ABI         string // e.g. "cp311"; omit to let make_bundle.sh choose
	User        string // omit to let make_bundle.sh choose
	Base        string // omit to let make_bundle.sh choose
	OutDir      string
	SignKeyPath string // omit for an unsigned bundle
}

// BuildViaRepo runs scripts/build/make_bundle.sh on an existing checkout and
// returns the path to the resulting archive. This is the whole of "repo
// mode" (E3): once the archive exists, Verify and Deploy treat it exactly
// like a downloaded one.
func BuildViaRepo(ctx context.Context, args BuildArgs) (string, error) {
	script := filepath.Join(args.RepoRoot, "scripts", "build", "make_bundle.sh")
	if _, err := os.Stat(script); err != nil {
		return "", fmt.Errorf("make_bundle.sh not found at %s: %w", script, err)
	}

	cmdArgs := []string{"--arch", args.Arch, "--out", args.OutDir}
	for flag, value := range map[string]string{
		"--python-minor": args.PythonMinor,
		"--abi":          args.ABI,
		"--user":         args.User,
		"--base":         args.Base,
		"--sign-key":     args.SignKeyPath,
	} {
		if value != "" {
			cmdArgs = append(cmdArgs, flag, value)
		}
	}

	cmd := exec.CommandContext(ctx, "bash", append([]string{script}, cmdArgs...)...)
	cmd.Dir = args.RepoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("make_bundle.sh failed: %w\n%s", err, output)
	}

	archive, err := findBuiltArchive(args.OutDir)
	if err != nil {
		return "", fmt.Errorf("locating the built archive: %w\noutput:\n%s", err, output)
	}
	return archive, nil
}

// findBuiltArchive locates make_bundle.sh's output in outDir: exactly one
// energy-node-*.tar.gz, never the separate caddy-*.tar.gz side-package
// (E13) it may also produce alongside it.
func findBuiltArchive(outDir string) (string, error) {
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return "", err
	}
	var matches []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "energy-node-") && strings.HasSuffix(name, ".tar.gz") {
			matches = append(matches, filepath.Join(outDir, name))
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no energy-node-*.tar.gz found in %s", outDir)
	default:
		return "", fmt.Errorf("more than one energy-node-*.tar.gz found in %s: %v", outDir, matches)
	}
}
