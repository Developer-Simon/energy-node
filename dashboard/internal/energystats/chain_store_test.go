package energystats

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// storedChainPair returns the pair entry of the hour, false without one.
func storedChainPair(t *testing.T, store *ChainStore, hour time.Time, pair ChainPair) (PairEnergy, bool) {
	t.Helper()
	var got PairEnergy
	found := false
	err := store.Each(hour, hour.Add(time.Hour), func(_ time.Time, m *ChainMonth, i int) {
		if m == nil {
			return
		}
		if e, ok := m.Pairs[pair][i]; ok {
			got, found = e, true
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return got, found
}

// storedChainStandby returns the standby entry of the hour, false without one.
func storedChainStandby(t *testing.T, store *ChainStore, hour time.Time, entity string) (StandbyEnergy, bool) {
	t.Helper()
	var got StandbyEnergy
	found := false
	err := store.Each(hour, hour.Add(time.Hour), func(_ time.Time, m *ChainMonth, i int) {
		if m == nil {
			return
		}
		if e, ok := m.Standby[entity][i]; ok {
			got, found = e, true
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return got, found
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestChainStoreAddsAcrossRestartsInSameHour(t *testing.T) {
	dir := t.TempDir()
	hour := hourAt("2026-11-06T12:00:00Z")
	pair := ChainPair{Source: "a", Target: "b"}

	first := NewChainStore(dir, nil)
	if err := first.Add(ChainHour{
		Hour:    hour,
		Pairs:   map[ChainPair]PairEnergy{pair: {SourceKWh: 1.234, TargetKWh: 1.198, Seconds: 3000}},
		Standby: map[string]StandbyEnergy{"a": {KWh: 0.004, Seconds: 900}},
	}); err != nil {
		t.Fatal(err)
	}
	// Neuer Store, wie nach einem Neustart in derselben Stunde.
	second := NewChainStore(dir, nil)
	if err := second.Add(ChainHour{
		Hour:    hour,
		Pairs:   map[ChainPair]PairEnergy{pair: {SourceKWh: 0.5, TargetKWh: 0.2, Seconds: 1000}},
		Standby: map[string]StandbyEnergy{"a": {KWh: 0.001, Seconds: 2000}},
	}); err != nil {
		t.Fatal(err)
	}

	reloaded := NewChainStore(dir, nil)
	got, ok := storedChainPair(t, reloaded, hour, pair)
	if !ok {
		t.Fatal("Paar nach dem Neuladen nicht gefunden")
	}
	if !approx(got.SourceKWh, 1.734) || !approx(got.TargetKWh, 1.398) {
		t.Fatalf("kWh = %v / %v, erwartet 1.734 / 1.398", got.SourceKWh, got.TargetKWh)
	}
	if got.Seconds != 3600 {
		t.Fatalf("Sekunden = %d, erwartet 3600 (gedeckelt aus 4000)", got.Seconds)
	}
	standby, ok := storedChainStandby(t, reloaded, hour, "a")
	if !ok {
		t.Fatal("Standby nach dem Neuladen nicht gefunden")
	}
	if !approx(standby.KWh, 0.005) || standby.Seconds != 2900 {
		t.Fatalf("Standby = %+v, erwartet 0.005 kWh und 2900 s", standby)
	}
}

func TestChainStoreSeparatesMonths(t *testing.T) {
	dir := t.TempDir()
	pair := ChainPair{Source: "a", Target: "b"}
	octoberHour := hourAt("2026-10-31T23:00:00Z")
	novemberHour := hourAt("2026-11-01T00:00:00Z")

	store := NewChainStore(dir, nil)
	if err := store.Add(ChainHour{Hour: octoberHour, Pairs: map[ChainPair]PairEnergy{pair: {SourceKWh: 2, TargetKWh: 1.8, Seconds: 3600}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(ChainHour{Hour: novemberHour, Pairs: map[ChainPair]PairEnergy{pair: {SourceKWh: 3, TargetKWh: 2.7, Seconds: 3600}}}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"2026-10.json", "2026-11.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s fehlt: %v", name, err)
		}
	}

	var octIndex, novIndex int
	var octMonth, novMonth *ChainMonth
	store.Each(octoberHour, octoberHour.Add(time.Hour), func(_ time.Time, m *ChainMonth, i int) { octMonth, octIndex = m, i })
	store.Each(novemberHour, novemberHour.Add(time.Hour), func(_ time.Time, m *ChainMonth, i int) { novMonth, novIndex = m, i })
	if octIndex != 743 || novIndex != 0 {
		t.Fatalf("Stundenindex: Oktober %d, November %d, erwartet 743 und 0", octIndex, novIndex)
	}
	if octMonth.Start.Format("2006-01") != "2026-10" || novMonth.Start.Format("2006-01") != "2026-11" {
		t.Fatalf("Monate vertauscht: %v / %v", octMonth.Start, novMonth.Start)
	}
	if got := octMonth.Pairs[pair][octIndex].SourceKWh; !approx(got, 2) {
		t.Fatalf("Oktober-Stunde = %v kWh, erwartet 2", got)
	}
	if got := novMonth.Pairs[pair][novIndex].SourceKWh; !approx(got, 3) {
		t.Fatalf("November-Stunde = %v kWh, erwartet 3", got)
	}
	if len(octMonth.Pairs[pair]) != 1 || len(novMonth.Pairs[pair]) != 1 {
		t.Fatal("Stunden eines Monats in den anderen Monat gelaufen")
	}
}

func TestChainStoreRoundTripKeepsDirectionsApart(t *testing.T) {
	dir := t.TempDir()
	hour := hourAt("2026-10-01T10:00:00Z")
	ab := ChainPair{Source: "a", Target: "b"}
	ba := ChainPair{Source: "b", Target: "a"}
	store := NewChainStore(dir, nil)
	if err := store.Add(ChainHour{Hour: hour, Pairs: map[ChainPair]PairEnergy{
		ab: {SourceKWh: 1, TargetKWh: 0.9, Seconds: 3600},
		ba: {SourceKWh: 0.3, TargetKWh: 0.2, Seconds: 3600},
	}}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "2026-10.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"version":1,"start":"2026-10-01T00:00:00Z","pairs":[{"source":"a","target":"b","hours":{"10":[1,0.9,3600]}},{"source":"b","target":"a","hours":{"10":[0.3,0.2,3600]}}],"standby":[]}`
	if string(data) != want {
		t.Fatalf("Dateiinhalt:\n%s\nerwartet:\n%s", data, want)
	}

	reloaded := NewChainStore(dir, nil)
	got, ok := storedChainPair(t, reloaded, hour, ab)
	if !ok || !approx(got.SourceKWh, 1) || !approx(got.TargetKWh, 0.9) {
		t.Fatalf("a->b = %+v, %v", got, ok)
	}
	got, ok = storedChainPair(t, reloaded, hour, ba)
	if !ok || !approx(got.SourceKWh, 0.3) || !approx(got.TargetKWh, 0.2) {
		t.Fatalf("b->a = %+v, %v", got, ok)
	}
}

func TestChainStoreMovesCorruptFileAside(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-11.json")
	os.WriteFile(path, []byte("{kaputt"), 0o644)
	store := NewChainStore(dir, nil)
	var logged string
	store.logf = func(format string, args ...any) { logged = format }
	pair := ChainPair{Source: "a", Target: "b"}
	hour := hourAt("2026-11-02T08:00:00Z")
	if err := store.Add(ChainHour{Hour: hour, Pairs: map[ChainPair]PairEnergy{pair: {SourceKWh: 1, TargetKWh: 0.9, Seconds: 3600}}}); err != nil {
		t.Fatalf("Add nach beschaedigter Datei: %v", err)
	}
	if _, err := os.Stat(path + ".corrupt"); err != nil {
		t.Fatalf(".corrupt fehlt: %v", err)
	}
	if logged == "" {
		t.Fatal("keine Logmeldung")
	}
	if got, ok := storedChainPair(t, NewChainStore(dir, nil), hour, pair); !ok || !approx(got.SourceKWh, 1) {
		t.Fatalf("neue Datei enthaelt die Stunde nicht: %+v, %v", got, ok)
	}
}

func TestChainStoreRejectsWrongMonthInFile(t *testing.T) {
	dir := t.TempDir()
	pair := ChainPair{Source: "a", Target: "b"}
	// Eine gueltige Oktoberdatei, unter dem Namen November abgelegt.
	octoberStore := NewChainStore(dir, nil)
	if err := octoberStore.Add(ChainHour{Hour: hourAt("2026-10-05T10:00:00Z"), Pairs: map[ChainPair]PairEnergy{pair: {SourceKWh: 9, TargetKWh: 8, Seconds: 3600}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "2026-10.json"), filepath.Join(dir, "2026-11.json")); err != nil {
		t.Fatal(err)
	}

	store := NewChainStore(dir, nil)
	store.logf = func(string, ...any) {}
	hour := hourAt("2026-11-03T07:00:00Z")
	if err := store.Add(ChainHour{Hour: hour, Pairs: map[ChainPair]PairEnergy{pair: {SourceKWh: 1, TargetKWh: 0.9, Seconds: 3600}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-11.json.corrupt")); err != nil {
		t.Fatalf("falscher Monat nicht beiseitegelegt: %v", err)
	}
	// Der Novembermonat enthaelt nur die neue Stunde, nicht die Oktoberwerte
	// (9 kWh). Index 55 ist der 3. November, 07:00 UTC.
	var month *ChainMonth
	store.Each(hour, hour.Add(time.Hour), func(_ time.Time, m *ChainMonth, _ int) { month = m })
	if month == nil || len(month.Pairs[pair]) != 1 {
		t.Fatalf("Novembermonat: %+v", month)
	}
	if got, ok := month.Pairs[pair][55]; !ok || !approx(got.SourceKWh, 1) {
		t.Fatalf("Novemberstunde = %+v, %v", got, ok)
	}
}

func TestChainStoreEachWithoutFileGivesNilMonth(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "chains")
	store := NewChainStore(dir, nil)
	calls, nils := 0, 0
	from := hourAt("2030-01-01T00:00:00Z")
	if err := store.Each(from, from.Add(48*time.Hour), func(_ time.Time, m *ChainMonth, _ int) {
		calls++
		if m == nil {
			nils++
		}
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 48 || nils != 48 {
		t.Fatalf("calls=%d nils=%d, erwartet 48 und 48", calls, nils)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("Lesen hat Dateien angelegt: %v", err)
	}
}

func TestChainStoreOnChangeFiresOnWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-11.json")
	pair := ChainPair{Source: "a", Target: "b"}
	calls := 0
	store := NewChainStore(dir, func() {
		calls++
		// Der Aufruf kommt nach dem Schreiben, die Datei ist schon da.
		if _, err := os.Stat(path); err != nil {
			t.Errorf("onChange vor dem Schreiben: %v", err)
		}
	})
	hour := hourAt("2026-11-04T09:00:00Z")
	if err := store.Add(ChainHour{Hour: hour, Pairs: map[ChainPair]PairEnergy{pair: {SourceKWh: 1, TargetKWh: 0.9, Seconds: 3600}}}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("onChange %d-mal, erwartet 1", calls)
	}
	if err := store.Add(ChainHour{Hour: hour}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("leere Stunde hat onChange ausgeloest (%d)", calls)
	}
	if err := store.Add(ChainHour{Hour: hour, Pairs: map[ChainPair]PairEnergy{pair: {SourceKWh: 1, TargetKWh: 0.9, Seconds: 3600}}}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("onChange %d-mal, erwartet 2", calls)
	}
	// Index 81 ist der 4. November, 09:00 UTC.
	if !strings.Contains(readFileString(t, path), `"hours":{"81":[2,1.8,3600]}`) {
		t.Fatalf("Summe nicht geschrieben: %s", readFileString(t, path))
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
