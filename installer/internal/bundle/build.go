package bundle

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
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
	// DevVersion appends -dev.<commit> (and .dirty) to the bundle version
	// (make_bundle.sh --dev-version), so a build from a working tree never
	// shares its step stamps with a release of the same VERSION.
	DevVersion bool
	// Log, when set, receives every line make_bundle.sh prints (stdout and
	// stderr interleaved) while it runs. The full output is still included
	// in the error on failure.
	Log func(line string)
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

	if args.DevVersion {
		cmdArgs = append(cmdArgs, "--dev-version")
	}

	cmd := exec.CommandContext(ctx, "bash", append([]string{script}, cmdArgs...)...)
	cmd.Dir = args.RepoRoot
	var output []byte
	var err error
	if args.Log == nil {
		output, err = cmd.CombinedOutput()
	} else {
		output, err = runStreaming(cmd, args.Log)
	}
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

// runStreaming runs cmd with stdout and stderr merged, calls log for each
// line as it arrives, and returns everything it captured.
func runStreaming(cmd *exec.Cmd, log func(string)) ([]byte, error) {
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	var captured bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(io.TeeReader(pr, &captured))
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for scanner.Scan() {
			log(scanner.Text())
		}
		// A line beyond the scanner's limit stops it; keep draining so the
		// child never blocks on a full pipe.
		_, _ = io.Copy(io.Discard, pr)
	}()
	err := cmd.Run()
	_ = pw.Close()
	<-done
	return captured.Bytes(), err
}
