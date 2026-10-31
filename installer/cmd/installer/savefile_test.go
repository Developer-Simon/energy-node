package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveFileInWritesUnchangedName(t *testing.T) {
	dir := t.TempDir()
	name := "energy-node-protokoll-20261031-120000.txt"
	content := []byte("inhalt")

	path, err := saveFileIn(dir, name, content)
	if err != nil {
		t.Fatalf("saveFileIn: %v", err)
	}
	if filepath.Base(path) != name {
		t.Fatalf("Dateiname = %q, want %q", filepath.Base(path), name)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("Inhalt = %q, want %q", got, content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("Rechte = %o, want 600", perm)
	}
}

func TestSaveFileInAddsCounterOnCollision(t *testing.T) {
	dir := t.TempDir()
	name := "energy-node-protokoll-20261031-120000.txt"

	first, err := saveFileIn(dir, name, []byte("erste"))
	if err != nil {
		t.Fatalf("erste Datei: %v", err)
	}
	second, err := saveFileIn(dir, name, []byte("zweite"))
	if err != nil {
		t.Fatalf("zweite Datei: %v", err)
	}
	if filepath.Base(second) != "energy-node-protokoll-20261031-120000-1.txt" {
		t.Fatalf("zweiter Dateiname = %q", filepath.Base(second))
	}
	got, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("ReadFile erste: %v", err)
	}
	if string(got) != "erste" {
		t.Fatalf("erste Datei verändert: %q", got)
	}
}

func TestSafeFileName(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd": "passwd",
		"a b/c:d.txt":      "c_d.txt",
		"":                 "energy-node.txt",
		".hidden":          "hidden",
	}
	for in, want := range cases {
		if got := safeFileName(in); got != want {
			t.Errorf("safeFileName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := safeFileName(strings.Repeat("a", 300)); len(got) != 100 {
		t.Errorf("Länge = %d, want 100", len(got))
	}
}

func TestSaveFileInCreatesMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "neu", "unter")
	path, err := saveFileIn(dir, "datei.txt", []byte("x"))
	if err != nil {
		t.Fatalf("saveFileIn: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Datei fehlt: %v", err)
	}
}
