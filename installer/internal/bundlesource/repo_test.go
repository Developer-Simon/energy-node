package bundlesource

import (
	"os"
	"path/filepath"
	"testing"
)

func fakeCheckout(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	script := filepath.Join(root, "scripts", "build", "make_bundle.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDetectRepoWalksUpFromAnyStart(t *testing.T) {
	root := fakeCheckout(t)
	deep := filepath.Join(root, "installer", "cmd")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DetectRepo(t.TempDir(), deep); got != root {
		t.Errorf("DetectRepo = %q, want %q", got, root)
	}
	if got := DetectRepo(t.TempDir()); got != "" {
		t.Errorf("DetectRepo without a checkout = %q, want empty", got)
	}
}

func TestCheckRepoRejectsANonCheckout(t *testing.T) {
	err := CheckRepo(t.TempDir())
	if err == nil || err.Code != CodeRepoNotACheckout {
		t.Fatalf("CheckRepo = %v, want %s", err, CodeRepoNotACheckout)
	}
}

func TestCheckRepoReportsMissingTools(t *testing.T) {
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	lookPath = func(name string) (string, error) {
		if name == "go" {
			return "", os.ErrNotExist
		}
		return "/usr/bin/" + name, nil
	}
	err := CheckRepo(fakeCheckout(t))
	if err == nil || err.Code != CodeBuildToolsMissing || err.Detail != "go" {
		t.Fatalf("CheckRepo = %+v, want %s with detail go", err, CodeBuildToolsMissing)
	}
	if got := MissingTools(); len(got) != 1 || got[0] != "go" {
		t.Errorf("MissingTools = %v, want [go]", got)
	}
}
