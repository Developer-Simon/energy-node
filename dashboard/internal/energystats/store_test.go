package energystats

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAddOwnWritesTheMonthFileAndReloadsIt(t *testing.T) {
	dir := t.TempDir()
	h := hourAt("2026-10-05T10:00:00Z")
	if err := NewStore(dir).AddOwn(HourRecord{Hour: h, KWh: map[string]float64{"pv": 1.5}, Covered: 3600}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-10.json")); err != nil {
		t.Fatalf("Monatsdatei fehlt: %v", err)
	}
	var got float64
	err := NewStore(dir).Each(h, h.Add(time.Hour), func(_ time.Time, m *Month, i int) {
		got = m.Roles["pv"].KWh[i]
	})
	if err != nil || got != 1.5 {
		t.Fatalf("neu geladen: %v, %v", got, err)
	}
}

func TestNoTempFilesAreLeftBehind(t *testing.T) {
	dir := t.TempDir()
	NewStore(dir).AddOwn(HourRecord{Hour: hourAt("2026-10-05T10:00:00Z"), KWh: map[string]float64{"pv": 1}, Covered: 3600})
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			t.Fatalf("Temp-Datei uebrig: %s", entry.Name())
		}
	}
}

func TestCorruptMonthIsMovedAsideAndStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-10.json")
	os.WriteFile(path, []byte("{kaputt"), 0o644)
	store := NewStore(dir)
	var logged string
	store.logf = func(format string, args ...any) { logged = format }
	h := hourAt("2026-10-05T10:00:00Z")
	if err := store.AddOwn(HourRecord{Hour: h, KWh: map[string]float64{"pv": 1}, Covered: 3600}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".corrupt"); err != nil {
		t.Fatalf(".corrupt fehlt: %v", err)
	}
	if logged == "" {
		t.Fatal("keine Logmeldung")
	}
	if _, err := decodeMonthFile(t, path); err != nil {
		t.Fatalf("neue Datei nicht lesbar: %v", err)
	}
}

func TestUnreadableMonthIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	// Ein Verzeichnis an Stelle der Datei: ReadFile scheitert mit einem
	// Fehler, der nicht "fehlt" bedeutet.
	os.Mkdir(filepath.Join(dir, "2026-10.json"), 0o755)
	err := NewStore(dir).AddOwn(HourRecord{Hour: hourAt("2026-10-05T10:00:00Z"), KWh: map[string]float64{"pv": 1}, Covered: 3600})
	if err == nil {
		t.Fatal("erwartet Fehler")
	}
	if info, _ := os.Stat(filepath.Join(dir, "2026-10.json")); !info.IsDir() {
		t.Fatal("Pfad wurde ueberschrieben")
	}
}

func TestFailedWriteIsRetriedWithTheNextRecord(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignoriert Dateirechte")
	}
	dir := t.TempDir()
	store := NewStore(dir)
	// Schreibgeschuetztes Verzeichnis: Lesen meldet "fehlt", CreateTemp
	// scheitert. So entsteht ein echter Schreibfehler.
	os.Chmod(dir, 0o555)
	first := HourRecord{Hour: hourAt("2026-10-31T23:00:00Z"), KWh: map[string]float64{"pv": 1}, Covered: 3600}
	err := store.AddOwn(first)
	os.Chmod(dir, 0o755)
	if err == nil {
		t.Fatal("erwartet Schreibfehler")
	}
	second := HourRecord{Hour: hourAt("2026-11-01T00:00:00Z"), KWh: map[string]float64{"pv": 2}, Covered: 3600}
	if err := store.AddOwn(second); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"2026-10.json", "2026-11.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s fehlt: %v", name, err)
		}
	}
}

func TestEachPassesNilForMonthsWithoutFileAndDoesNotCacheThem(t *testing.T) {
	store := NewStore(t.TempDir())
	calls, nils := 0, 0
	from := hourAt("2030-01-01T00:00:00Z")
	store.Each(from, from.Add(48*time.Hour), func(_ time.Time, m *Month, _ int) {
		calls++
		if m == nil {
			nils++
		}
	})
	if calls != 48 || nils != 48 {
		t.Fatalf("calls=%d nils=%d", calls, nils)
	}
	if len(store.months) != 0 {
		t.Fatalf("%d Monate im Cache, erwartet 0", len(store.months))
	}
	if len(store.missing) != 1 {
		t.Fatalf("%d fehlende Monate gemerkt, erwartet 1", len(store.missing))
	}
	// Spaeter angelegter Monat wird trotz Merker gefunden.
	h := hourAt("2030-01-01T05:00:00Z")
	if err := store.AddOwn(HourRecord{Hour: h, KWh: map[string]float64{"pv": 1}, Covered: 3600}); err != nil {
		t.Fatal(err)
	}
	found := false
	store.Each(h, h.Add(time.Hour), func(_ time.Time, m *Month, _ int) { found = m != nil })
	if !found {
		t.Fatal("neu angelegter Monat nicht gefunden")
	}
}

func decodeMonthFile(t *testing.T, path string) (*Month, error) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeMonth(data)
}

// monthForTest liefert den Monat zu t und legt ihn bei Bedarf an.
func (s *Store) monthForTest(t time.Time) (*Month, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.monthLocked(t, true)
}
