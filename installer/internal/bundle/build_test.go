package bundle_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
)

const fakeMakeBundleBody = `#!/bin/sh
set -eu
for a in "$@"; do
  if [ -n "${ARGS_LOG:-}" ]; then printf '%s\n' "$a" >> "$ARGS_LOG"; fi
done
out=""
arch=""
while [ $# -gt 0 ]; do
  case "$1" in
    --out) out="$2"; shift 2 ;;
    --arch) arch="$2"; shift 2 ;;
    *) shift ;;
  esac
done
mkdir -p "$out"
printf 'fake\n' > "$out/energy-node-vTEST-$arch.tar.gz"
`

func writeFakeMakeBundle(t *testing.T, repoRoot, body string) {
	t.Helper()
	script := filepath.Join(repoRoot, "scripts", "build", "make_bundle.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestBuildViaRepoReturnsTheBuiltArchivePath(t *testing.T) {
	repoRoot := t.TempDir()
	writeFakeMakeBundle(t, repoRoot, fakeMakeBundleBody)
	outDir := filepath.Join(t.TempDir(), "dist")

	archive, err := bundle.BuildViaRepo(context.Background(), bundle.BuildArgs{
		RepoRoot: repoRoot,
		Arch:     "amd64",
		OutDir:   outDir,
	})
	if err != nil {
		t.Fatalf("BuildViaRepo: %v", err)
	}
	want := filepath.Join(outDir, "energy-node-vTEST-amd64.tar.gz")
	if archive != want {
		t.Fatalf("got %q, want %q", archive, want)
	}
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("returned archive does not exist: %v", err)
	}
}

func TestBuildViaRepoReportsScriptFailure(t *testing.T) {
	repoRoot := t.TempDir()
	writeFakeMakeBundle(t, repoRoot, "#!/bin/sh\necho 'boom' >&2\nexit 1\n")

	_, err := bundle.BuildViaRepo(context.Background(), bundle.BuildArgs{
		RepoRoot: repoRoot,
		Arch:     "amd64",
		OutDir:   filepath.Join(t.TempDir(), "dist"),
	})
	if err == nil {
		t.Fatalf("expected an error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error should include the script's own output: %v", err)
	}
}

func TestBuildViaRepoRejectsAMissingScript(t *testing.T) {
	_, err := bundle.BuildViaRepo(context.Background(), bundle.BuildArgs{
		RepoRoot: t.TempDir(),
		Arch:     "amd64",
		OutDir:   t.TempDir(),
	})
	if err == nil {
		t.Fatalf("expected an error for a missing make_bundle.sh")
	}
}

func TestBuildViaRepoForwardsFlagsToTheScript(t *testing.T) {
	repoRoot := t.TempDir()
	writeFakeMakeBundle(t, repoRoot, fakeMakeBundleBody)

	argsLog := filepath.Join(t.TempDir(), "args.log")
	t.Setenv("ARGS_LOG", argsLog)

	_, err := bundle.BuildViaRepo(context.Background(), bundle.BuildArgs{
		RepoRoot:    repoRoot,
		Arch:        "arm64",
		PythonMinor: "3.11",
		ABI:         "cp311",
		User:        "energynode",
		Base:        "/home/energynode",
		OutDir:      filepath.Join(t.TempDir(), "dist"),
		SignKeyPath: "/tmp/sign-key.pem",
		DevVersion:  true,
	})
	if err != nil {
		t.Fatalf("BuildViaRepo: %v", err)
	}

	logged, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("reading args log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(logged), "\n"), "\n")
	contains := func(s string) bool {
		for _, l := range lines {
			if l == s {
				return true
			}
		}
		return false
	}
	for _, want := range []string{
		"--arch", "arm64", "--python-minor", "3.11", "--abi", "cp311",
		"--user", "energynode", "--base", "/home/energynode",
		"--sign-key", "/tmp/sign-key.pem", "--dev-version",
	} {
		if !contains(want) {
			t.Fatalf("expected argument %q not found in log:\n%s", want, logged)
		}
	}
}

func TestBuildViaRepoOmitsUnsetOptionalFlags(t *testing.T) {
	repoRoot := t.TempDir()
	writeFakeMakeBundle(t, repoRoot, fakeMakeBundleBody)

	argsLog := filepath.Join(t.TempDir(), "args.log")
	t.Setenv("ARGS_LOG", argsLog)

	_, err := bundle.BuildViaRepo(context.Background(), bundle.BuildArgs{
		RepoRoot: repoRoot,
		Arch:     "amd64",
		OutDir:   filepath.Join(t.TempDir(), "dist"),
	})
	if err != nil {
		t.Fatalf("BuildViaRepo: %v", err)
	}

	logged, _ := os.ReadFile(argsLog)
	if strings.Contains(string(logged), "--sign-key") {
		t.Fatalf("an unset SignKeyPath must not produce a --sign-key flag:\n%s", logged)
	}
	if strings.Contains(string(logged), "--dev-version") {
		t.Fatalf("DevVersion false must not produce --dev-version:\n%s", logged)
	}
}

func TestBuildViaRepoStreamsTheScriptOutputLineByLine(t *testing.T) {
	repoRoot := t.TempDir()
	writeFakeMakeBundle(t, repoRoot, `#!/bin/sh
set -eu
out=""
while [ $# -gt 0 ]; do
  case "$1" in
    --out) out="$2"; shift 2 ;;
    *) shift ;;
  esac
done
mkdir -p "$out"
echo step-one
echo step-two >&2
printf 'fake\n' > "$out/energy-node-vTEST-armv6.tar.gz"
`)
	var lines []string
	_, err := bundle.BuildViaRepo(context.Background(), bundle.BuildArgs{
		RepoRoot: repoRoot,
		Arch:     "armv6",
		OutDir:   filepath.Join(t.TempDir(), "dist"),
		Log:      func(line string) { lines = append(lines, line) },
	})
	if err != nil {
		t.Fatalf("BuildViaRepo: %v", err)
	}
	if len(lines) != 2 || lines[0] != "step-one" || lines[1] != "step-two" {
		t.Fatalf("streamed lines = %q, want [step-one step-two]", lines)
	}
}
