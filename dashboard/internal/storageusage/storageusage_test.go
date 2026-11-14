package storageusage

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
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

// newTestMeter baut einen Meter mit fester Uhr. spawn sammelt die Messungen,
// statt sie zu starten, und measure zählt die Aufrufe.
func newTestMeter(t *testing.T, ttl time.Duration, value *Parts) (*Meter, *int, *[]func(), *time.Time) {
	t.Helper()
	calls := 0
	spawned := []func(){}
	clock := time.Unix(1_000_000, 0)
	m := NewMeter(func() Parts {
		calls++
		return *value
	}, ttl)
	m.now = func() time.Time { return clock }
	m.spawn = func(f func()) { spawned = append(spawned, f) }
	return m, &calls, &spawned, &clock
}

func TestMeterFirstGetHasNoValueAndStartsOneMeasure(t *testing.T) {
	value := Parts{Install: 1}
	m, _, spawned, _ := newTestMeter(t, 10*time.Minute, &value)

	if _, ok := m.Get("k"); ok {
		t.Fatal("erster Get liefert einen Wert")
	}
	if _, ok := m.Get("k"); ok {
		t.Fatal("zweiter Get liefert vor Ende der Messung einen Wert")
	}
	if len(*spawned) != 1 {
		t.Fatalf("Messungen gestartet = %d, want 1", len(*spawned))
	}
}

func TestMeterServesCachedValueWithinTTL(t *testing.T) {
	value := Parts{Install: 1}
	m, calls, _, clock := newTestMeter(t, 10*time.Minute, &value)
	m.spawn = func(f func()) { f() }

	if _, ok := m.Get("k"); ok {
		t.Fatal("erster Get liefert einen Wert")
	}
	if p, ok := m.Get("k"); !ok || p.Install != 1 {
		t.Fatalf("Get = %+v, %v, want Install 1, true", p, ok)
	}
	*clock = clock.Add(9 * time.Minute)
	if p, ok := m.Get("k"); !ok || p.Install != 1 {
		t.Fatalf("Get nach 9 min = %+v, %v, want Install 1, true", p, ok)
	}
	if *calls != 1 {
		t.Fatalf("measure aufgerufen %d Mal, want 1", *calls)
	}
}

func TestMeterRefreshesStaleValueButServesItMeanwhile(t *testing.T) {
	value := Parts{Install: 1}
	m, _, spawned, clock := newTestMeter(t, 10*time.Minute, &value)
	m.spawn = func(f func()) { f() }
	m.Get("k")
	m.Get("k")
	m.spawn = func(f func()) { *spawned = append(*spawned, f) }

	*clock = clock.Add(11 * time.Minute)
	value = Parts{Install: 2}
	if p, ok := m.Get("k"); !ok || p.Install != 1 {
		t.Fatalf("veralteter Get = %+v, %v, want alter Wert Install 1, true", p, ok)
	}
	if len(*spawned) != 1 {
		t.Fatalf("Messungen gestartet = %d, want 1", len(*spawned))
	}
	(*spawned)[0]()
	if p, ok := m.Get("k"); !ok || p.Install != 2 {
		t.Fatalf("Get nach Messung = %+v, %v, want Install 2, true", p, ok)
	}
}

func TestMeterKeyChangeDropsOldValue(t *testing.T) {
	value := Parts{Install: 1}
	m, _, spawned, _ := newTestMeter(t, 10*time.Minute, &value)
	m.spawn = func(f func()) { f() }
	m.Get("a")
	m.Get("a")
	m.spawn = func(f func()) { *spawned = append(*spawned, f) }

	if _, ok := m.Get("b"); ok {
		t.Fatal("Get mit neuem key liefert den alten Wert")
	}
	if len(*spawned) != 1 {
		t.Fatalf("Messungen gestartet = %d, want 1", len(*spawned))
	}
	(*spawned)[0]()
	if _, ok := m.Get("b"); !ok {
		t.Fatal("Get(b) nach Messung liefert keinen Wert")
	}
	if _, ok := m.Get("a"); ok {
		t.Fatal("Get(a) liefert noch den Wert des alten key")
	}
}

func TestMeterResultOfOldKeyIsNotStoredForNewKey(t *testing.T) {
	value := Parts{Install: 1}
	m, _, spawned, _ := newTestMeter(t, 10*time.Minute, &value)
	m.Get("a")
	if len(*spawned) != 1 {
		t.Fatalf("Messungen gestartet = %d, want 1", len(*spawned))
	}

	if _, ok := m.Get("b"); ok {
		t.Fatal("Get(b) liefert einen Wert")
	}
	if len(*spawned) != 1 {
		t.Fatalf("Get(b) hat eine zweite Messung gestartet, want 1 laufende")
	}

	(*spawned)[0]()
	if _, ok := m.Get("b"); ok {
		t.Fatal("Ergebnis von a wurde für b gespeichert")
	}
	if len(*spawned) != 2 {
		t.Fatalf("Messungen gestartet = %d, want 2", len(*spawned))
	}
}
