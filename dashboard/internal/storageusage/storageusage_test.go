package storageusage

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// writeFile legt eine Datei mit 10000 Byte an und legt die Ordner dafür an.
func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, 10000), 0o644); err != nil {
		t.Fatal(err)
	}
}

// blocksOf liefert den belegten Platz einer Datei in Byte, ohne Symlinks zu folgen.
func blocksOf(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("%s: kein Stat_t", path)
	}
	return int64(st.Blocks) * 512
}

func TestMeasureSumsEachPart(t *testing.T) {
	base := t.TempDir()
	install := filepath.Join(base, "a")
	runtime := filepath.Join(base, "r")
	backups := filepath.Join(base, "b")
	writeFile(t, filepath.Join(install, "f1"))
	writeFile(t, filepath.Join(runtime, "f1"))
	writeFile(t, filepath.Join(runtime, "f2"))
	writeFile(t, filepath.Join(backups, "f1"))

	src := Sources{
		Ref:     base,
		Install: []string{install},
		Runtime: []string{runtime},
		Backups: []string{backups},
	}
	got := Measure(src)

	want := Parts{
		Install: blocksOf(t, filepath.Join(install, "f1")),
		Runtime: blocksOf(t, filepath.Join(runtime, "f1")) + blocksOf(t, filepath.Join(runtime, "f2")),
		Backups: blocksOf(t, filepath.Join(backups, "f1")),
	}
	if got != want {
		t.Fatalf("Measure() = %+v, want %+v", got, want)
	}
}

func TestMeasureExcludesNestedDirsFromInstall(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	x := filepath.Join(home, "data", "runtime", "x")
	y := filepath.Join(home, "data", "backups", "y")
	z := filepath.Join(home, "app", "z")
	writeFile(t, x)
	writeFile(t, y)
	writeFile(t, z)

	src := Sources{
		Ref:     base,
		Install: []string{home},
		Runtime: []string{filepath.Join(home, "data", "runtime")},
		Backups: []string{filepath.Join(home, "data", "backups")},
		Exclude: []string{
			filepath.Join(home, "data", "runtime"),
			filepath.Join(home, "data", "backups"),
		},
	}
	got := Measure(src)

	want := Parts{
		Install: blocksOf(t, z),
		Runtime: blocksOf(t, x),
		Backups: blocksOf(t, y),
	}
	if got != want {
		t.Fatalf("Measure() = %+v, want %+v", got, want)
	}
}

func TestMeasureDoesNotFollowSymlinks(t *testing.T) {
	base := t.TempDir()
	mount := filepath.Join(base, "mount")
	big := filepath.Join(mount, "big")
	writeFile(t, big)

	home := filepath.Join(base, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(mount, filepath.Join(home, "runtime")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(big, filepath.Join(home, "link")); err != nil {
		t.Fatal(err)
	}

	src := Sources{
		Ref:     base,
		Install: []string{home},
		Runtime: []string{filepath.Join(home, "runtime")},
	}
	got := Measure(src)

	if got.Runtime != 0 {
		t.Fatalf("Runtime = %d, want 0 (Symlink darf nicht gefolgt werden)", got.Runtime)
	}
	if got.Install != 0 {
		t.Fatalf("Install = %d, want 0 (Symlink auf Datei zählt nicht)", got.Install)
	}
}

func TestMeasureSkipsOtherDevices(t *testing.T) {
	base := t.TempDir()
	r := filepath.Join(base, "r")
	other := filepath.Join(base, "other")
	writeFile(t, filepath.Join(r, "f"))
	writeFile(t, filepath.Join(other, "f"))

	src := Sources{
		Ref:     base,
		Runtime: []string{r, other},
	}
	// Simuliert ein anderes Gerät: der Ordner "other" wird abgelehnt.
	sameDev := func(fi os.FileInfo) bool { return fi.Name() != "other" }
	got := measure(src, sameDev)

	want := Parts{Runtime: blocksOf(t, filepath.Join(r, "f"))}
	if got != want {
		t.Fatalf("measure() = %+v, want %+v", got, want)
	}
}

func TestMeasureMissingDirsCountZero(t *testing.T) {
	base := t.TempDir()
	missing := filepath.Join(base, "missing")
	src := Sources{
		Ref:     base,
		Install: []string{missing},
		Runtime: []string{missing},
		Backups: []string{missing},
	}
	if got := Measure(src); got != (Parts{}) {
		t.Fatalf("Measure() = %+v, want leer", got)
	}
}
