package bundlefetch

import (
	"os"
	"path/filepath"
	"testing"
)

func writeDir(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		code  string
	}{
		{"ok", map[string]string{"manifest.json": `{"version":"v1","arch":"armv6"}`, "manifest.json.sig": "s"}, ""},
		{"no manifest", map[string]string{"x": "y"}, CodeBundleInvalid},
		{"bad json", map[string]string{"manifest.json": `{`, "manifest.json.sig": "s"}, CodeBundleInvalid},
		{"no version", map[string]string{"manifest.json": `{"arch":"armv6"}`, "manifest.json.sig": "s"}, CodeBundleInvalid},
		{"wrong arch", map[string]string{"manifest.json": `{"version":"v1","arch":"amd64"}`, "manifest.json.sig": "s"}, CodeArchMismatch},
		{"unsigned", map[string]string{"manifest.json": `{"version":"v1","arch":"armv6"}`}, CodeBundleUnsigned},
	}
	for _, c := range cases {
		dir := t.TempDir()
		writeDir(t, dir, c.files)
		version, err := validate(dir, "armv6")
		if c.code == "" {
			if err != nil || version != "v1" {
				t.Errorf("%s: version, err = %q, %v", c.name, version, err)
			}
			continue
		}
		if e := asError(err); e == nil || e.Code != c.code {
			t.Errorf("%s: err = %v, want %s", c.name, err, c.code)
		}
	}
}

func TestCandidateVersion(t *testing.T) {
	dir := t.TempDir()
	if got := candidateVersion(dir, "armv6"); got != "" {
		t.Errorf("empty dir: %q, want empty", got)
	}
	writeDir(t, dir, map[string]string{"manifest.json": `{"version":"v3","arch":"armv6"}`, "manifest.json.sig": "s"})
	if got := candidateVersion(dir, "armv6"); got != "v3" {
		t.Errorf("candidateVersion = %q, want v3", got)
	}
	if got := candidateVersion(dir, "amd64"); got != "" {
		t.Errorf("wrong arch: %q, want empty", got)
	}
}

func TestSwapInReplacesTheDestinationAndKeepsItOnFailure(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "candidate")
	writeDir(t, dest, map[string]string{"old.txt": "old"})
	staged := filepath.Join(root, "staged")
	writeDir(t, staged, map[string]string{"new.txt": "new"})

	if err := swapIn(staged, dest); err != nil {
		t.Fatalf("swapIn: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "new.txt")); err != nil {
		t.Errorf("new content missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "old.txt")); err == nil {
		t.Errorf("old content survived the swap")
	}
	if _, err := os.Stat(dest + ".old"); err == nil {
		t.Errorf("the .old directory was not cleaned up")
	}

	// A failing swap must leave the current candidate exactly as it was.
	if e := asError(swapIn(filepath.Join(root, "does-not-exist"), dest)); e == nil || e.Code != CodeInstallFailed {
		t.Fatalf("err = %v, want %s", e, CodeInstallFailed)
	}
	if _, err := os.Stat(filepath.Join(dest, "new.txt")); err != nil {
		t.Errorf("the candidate was damaged by a failed swap: %v", err)
	}

	// No candidate yet: swapIn just moves the staged directory into place.
	fresh := filepath.Join(root, "fresh")
	writeDir(t, staged, map[string]string{"again.txt": "x"})
	if err := swapIn(staged, fresh); err != nil {
		t.Fatalf("swapIn without an existing destination: %v", err)
	}
}
