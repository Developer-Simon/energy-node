package energystats

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

func TestRevisionRisesOnOwnHour(t *testing.T) {
	store := NewStore(t.TempDir())
	before := store.Revision()
	if err := store.AddOwn(HourRecord{Hour: hourAt("2026-10-05T10:00:00Z"), KWh: map[string]float64{"pv": 1}, Covered: 3600}); err != nil {
		t.Fatal(err)
	}
	if store.Revision() <= before {
		t.Fatalf("Revision %d nach AddOwn, vorher %d", store.Revision(), before)
	}
}

func TestRevisionRisesOnHAHourOnlyWhenSomethingChanged(t *testing.T) {
	store := NewStore(t.TempDir())
	record := HARecord{Hour: hourAt("2026-10-05T10:00:00Z"), Role: "pv", KWh: 2}
	if err := store.SetHA([]HARecord{record}); err != nil {
		t.Fatal(err)
	}
	after := store.Revision()
	if after == 0 {
		t.Fatal("SetHA mit neuer Stunde hat die Revision nicht erhoeht")
	}
	if err := store.SetHA([]HARecord{record}); err != nil {
		t.Fatal(err)
	}
	if store.Revision() != after {
		t.Errorf("SetHA ohne Aenderung hat die Revision erhoeht: %d -> %d", after, store.Revision())
	}
}

func TestRevisionRisesWhenACorruptMonthIsMovedAside(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "2026-10.json"), []byte("{kaputt"), 0o644)
	store := NewStore(dir)
	store.logf = func(string, ...any) {}
	before := store.Revision()
	from := hourAt("2026-10-05T00:00:00Z")
	if err := store.Each(from, from.Add(time.Hour), func(time.Time, *Month, int) {}); err != nil {
		t.Fatal(err)
	}
	if store.Revision() <= before {
		t.Error("Verschieben der kaputten Datei hat die Revision nicht erhoeht")
	}
}

func TestReadingDoesNotChangeTheRevision(t *testing.T) {
	store := NewStore(t.TempDir())
	store.AddOwn(HourRecord{Hour: hourAt("2026-10-05T10:00:00Z"), KWh: map[string]float64{"pv": 1}, Covered: 3600})
	before := store.Revision()
	from := hourAt("2026-10-05T00:00:00Z")
	store.Each(from, from.Add(24*time.Hour), func(time.Time, *Month, int) {})
	store.SourceAt("pv", from)
	store.HADays("pv", from, 1)
	if store.Revision() != before {
		t.Errorf("Lesen hat die Revision geaendert: %d -> %d", before, store.Revision())
	}
}

// Die Statuszeile der Seite zeigt den Modus. Ein Wechsel muss die Revision
// heben, damit die Seite ihn ohne Neuladen sieht.
func TestRevisionRisesWhenTheRecorderStateChanges(t *testing.T) {
	r, store, box := newPolicyRecorder(t, Policy{Mode: RecordingAlways})
	start := hourAt("2026-10-05T10:00:00Z")
	r.Observe(start, 0, snapshotWith(energy.Balance{PV: 1000}))
	before := store.Revision()
	r.Observe(start.Add(time.Second), time.Second, snapshotWith(energy.Balance{PV: 1000}))
	if store.Revision() != before {
		t.Fatalf("gleicher Status hat die Revision geaendert: %d -> %d", before, store.Revision())
	}
	box.p = Policy{Mode: RecordingOff}
	r.Observe(start.Add(2*time.Second), 2*time.Second, snapshotWith(energy.Balance{PV: 1000}))
	if store.Revision() <= before {
		t.Error("Wechsel auf Aus hat die Revision nicht erhoeht")
	}
}

func TestStatsResponseCarriesTheRevision(t *testing.T) {
	store := NewStore(t.TempDir())
	store.AddOwn(HourRecord{Hour: hourAt("2026-10-05T10:00:00Z"), KWh: map[string]float64{"pv": 1}, Covered: 3600})
	svc := &Service{Store: store, Location: time.UTC, Now: func() time.Time { return hourAt("2026-10-05T12:00:00Z") }}
	resp, err := svc.Stats("day", "")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Revision != store.Revision() || resp.Revision == 0 {
		t.Errorf("Revision = %d, Store = %d", resp.Revision, store.Revision())
	}
}
