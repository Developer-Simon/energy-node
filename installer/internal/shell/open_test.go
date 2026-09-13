package shell_test

import (
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/shell"
)

func TestWindowsPrefersEdgeBecauseItIsAlwaysThere(t *testing.T) {
	candidates := shell.Candidates("windows")
	if len(candidates) == 0 {
		t.Fatalf("no candidates for windows")
	}
	if !strings.Contains(strings.ToLower(candidates[0].Name), "edge") {
		t.Errorf("first candidate = %q, want Edge - it ships with Windows", candidates[0].Name)
	}
}

func TestEveryCandidateAsksForAnAppWindow(t *testing.T) {
	for _, goos := range []string{"windows", "darwin", "linux"} {
		for _, candidate := range shell.Candidates(goos) {
			args := candidate.Args("http://127.0.0.1:1234/?token=x")
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, "--app=http://127.0.0.1:1234/?token=x") {
				t.Errorf("%s/%s: args = %v, want a --app= window", goos, candidate.Name, args)
			}
		}
	}
}

func TestMacOSCandidatesGoThroughOpen(t *testing.T) {
	for _, candidate := range shell.Candidates("darwin") {
		args := candidate.Args("http://127.0.0.1:1/")
		if candidate.Name != "open" {
			t.Errorf("darwin candidate = %q, want every one to be launched via open", candidate.Name)
		}
		if len(args) < 2 || args[0] != "-a" {
			t.Errorf("args = %v, want open -a <app> --args --app=<url>", args)
		}
	}
}

func TestAnUnknownGOOSStillHasCandidates(t *testing.T) {
	if len(shell.Candidates("freebsd")) == 0 {
		t.Errorf("an unknown GOOS must fall back to the Linux candidates")
	}
}
