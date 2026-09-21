package bundlesource

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// lookPath is a seam for tests.
var lookPath = exec.LookPath

const makeBundleScript = "scripts/build/make_bundle.sh"

// buildTools are what make_bundle.sh needs on the operator's machine: bash
// to run it, git for `rev-parse --show-toplevel`, go for the dashboard
// cross-compile.
var buildTools = []string{"bash", "go", "git"}

// DetectRepo returns the first directory that is, or contains as an
// ancestor of one of the starts, a checkout with make_bundle.sh; "" if none.
func DetectRepo(starts ...string) string {
	for _, start := range starts {
		dir := start
		for {
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(makeBundleScript))); err == nil {
				return dir
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return ""
}

// MissingTools lists the build tools that are not on PATH.
func MissingTools() []string {
	var missing []string
	for _, tool := range buildTools {
		if _, err := lookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	return missing
}

// ExpandHome resolves a leading "~" or "~/" to the user's home directory and
// cleans the result. The operator types the path into the UI, where no shell
// expands it. Anything else (including "~user") is returned unchanged.
func ExpandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

// CheckRepo reports why path cannot be built from, or nil if it can. A
// leading "~" is expanded first.
func CheckRepo(path string) *Error {
	path = ExpandHome(path)
	if _, err := os.Stat(filepath.Join(path, filepath.FromSlash(makeBundleScript))); err != nil {
		return &Error{Code: CodeRepoNotACheckout, Detail: path}
	}
	if missing := MissingTools(); len(missing) > 0 {
		return &Error{Code: CodeBuildToolsMissing, Detail: missing[0]}
	}
	return nil
}
