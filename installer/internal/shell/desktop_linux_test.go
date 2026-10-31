//go:build linux

package shell

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallDesktopEntryWritesEntryAndIcon(t *testing.T) {
	dir := t.TempDir()
	if err := installDesktopEntry(dir, "/opt/my tools/energy-node-installer", "energy-node-installer"); err != nil {
		t.Fatalf("installDesktopEntry: %v", err)
	}
	entry, err := os.ReadFile(filepath.Join(dir, "applications", "energy-node-installer.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`Exec="/opt/my tools/energy-node-installer"`,
		"StartupWMClass=energy-node-installer",
		"Icon=" + filepath.Join(dir, "icons", "hicolor", "256x256", "apps", "energy-node-installer.png"),
	} {
		if !strings.Contains(string(entry), want) {
			t.Errorf("entry lacks %q:\n%s", want, entry)
		}
	}
	icon, err := os.ReadFile(filepath.Join(dir, "icons", "hicolor", "256x256", "apps", "energy-node-installer.png"))
	if err != nil || !bytes.Equal(icon, iconPNG) || !bytes.HasPrefix(icon, []byte("\x89PNG")) {
		t.Errorf("icon not written as the embedded PNG (err %v)", err)
	}
}

func TestInstallDesktopEntryIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := installDesktopEntry(dir, "/x/installer", "installer"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "applications", "installer.desktop")
	before, _ := os.Stat(path)
	if err := installDesktopEntry(dir, "/x/installer", "installer"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("an unchanged entry was rewritten")
	}
}

func TestQuoteExecEscapes(t *testing.T) {
	if got := quoteExec(`/a"b$c%d`); got != `"/a\"b\$c%%d"` {
		t.Errorf("quoteExec = %s", got)
	}
}
