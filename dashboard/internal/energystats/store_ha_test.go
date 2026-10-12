package energystats

import (
	"testing"
	"time"
)

func TestSetHAReplacesOwnAndSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	h := hourAt("2026-10-05T10:00:00Z")
	store := NewStore(dir)
	store.AddOwn(HourRecord{Hour: h, KWh: map[string]float64{"pv": 0.4}, Covered: 3600})
	if err := store.SetHA([]HARecord{{Hour: h, Role: "pv", KWh: 0.5}, {Hour: h.Add(time.Hour), Role: "pv", KWh: 0.6}}); err != nil {
		t.Fatal(err)
	}
	reloaded := NewStore(dir)
	for _, c := range []struct {
		hour time.Time
		kwh  float64
	}{{h, 0.5}, {h.Add(time.Hour), 0.6}} {
		source, err := reloaded.SourceAt("pv", c.hour)
		if err != nil || source != SourceHA {
			t.Fatalf("%v: Quelle %q, %v", c.hour, source, err)
		}
	}
	var got float64
	reloaded.Each(h, h.Add(time.Hour), func(_ time.Time, m *Month, i int) { got = m.Roles["pv"].KWh[i] })
	if got != 0.5 {
		t.Fatalf("pv = %v", got)
	}
}

func TestHADaysCountsHAHoursPerUTCDay(t *testing.T) {
	store := NewStore(t.TempDir())
	day := hourAt("2026-10-30T00:00:00Z")
	var records []HARecord
	for h := 0; h < 24; h++ {
		records = append(records, HARecord{Hour: day.Add(time.Duration(h) * time.Hour), Role: "pv", KWh: 0.1})
	}
	// Tag 3 liegt im Folgemonat (November), 5 Stunden.
	for h := 0; h < 5; h++ {
		records = append(records, HARecord{Hour: day.Add(48*time.Hour + time.Duration(h)*time.Hour), Role: "pv", KWh: 0.1})
	}
	store.SetHA(records)
	store.AddOwn(HourRecord{Hour: day.Add(24 * time.Hour), KWh: map[string]float64{"pv": 1}, Covered: 3600})

	got, err := store.HADays("pv", day.Add(-24*time.Hour), 5)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{0, 24, 0, 5, 0}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("HADays = %v, erwartet %v", got, want)
		}
	}
	if counts, _ := store.HADays("load_total", day, 2); counts[0] != 0 || counts[1] != 0 {
		t.Fatalf("fremde Rolle = %v", counts)
	}
}

func TestHADaysOverYearsWithoutFilesReadsEachMonthOnce(t *testing.T) {
	store := NewStore(t.TempDir())
	counts, err := store.HADays("pv", hourAt("2016-10-01T00:00:00Z"), 3650)
	if err != nil || len(counts) != 3650 {
		t.Fatalf("len=%d err=%v", len(counts), err)
	}
	if len(store.missing) < 119 || len(store.missing) > 121 {
		t.Fatalf("%d fehlende Monate gemerkt", len(store.missing))
	}
}

func TestSourceAtWithoutDataIsNone(t *testing.T) {
	source, err := NewStore(t.TempDir()).SourceAt("pv", hourAt("2026-10-05T10:00:00Z"))
	if err != nil || source != SourceNone {
		t.Fatalf("%q %v", source, err)
	}
}
