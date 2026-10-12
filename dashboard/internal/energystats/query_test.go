package energystats

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func berlin(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("Zeitzonendaten fehlen:", err)
	}
	return loc
}

// fill legt fuer jede Stunde in [from, to) einen eigenen Satz an.
func fill(t *testing.T, store *Store, from, to time.Time, kwh map[string]float64) {
	t.Helper()
	for h := from.UTC(); h.Before(to); h = h.Add(time.Hour) {
		if err := store.AddOwn(HourRecord{Hour: h, KWh: kwh, Covered: 3600}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDayHasOneBucketPerHourIncludingDST(t *testing.T) {
	loc := berlin(t)
	store := NewStore(t.TempDir())
	later := time.Date(2027, 1, 1, 0, 0, 0, 0, loc)
	for at, want := range map[string]int{"2026-03-29": 23, "2026-10-25": 25, "2026-10-11": 24} {
		res, err := Query(store, "day", at, loc, later)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Buckets) != want || res.Total.Hours.None != want {
			t.Errorf("%s: %d Buckets, %d ohne Daten, erwartet %d", at, len(res.Buckets), res.Total.Hours.None, want)
		}
	}
}

func TestMonthHasOneBucketPerCalendarDayAcrossDST(t *testing.T) {
	loc := berlin(t)
	res, err := Query(NewStore(t.TempDir()), "month", "2026-10", loc, time.Date(2027, 1, 1, 0, 0, 0, 0, loc))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Buckets) != 31 || res.Total.Hours.None != 31*24+1 {
		t.Fatalf("%d Buckets, %d Stunden", len(res.Buckets), res.Total.Hours.None)
	}
	if res.Buckets[30].Start.Day() != 31 || res.Buckets[30].Start.Hour() != 0 {
		t.Fatalf("letzter Bucket beginnt %v", res.Buckets[30].Start)
	}
}

func TestSumsRatiosAndSourcesForAYear(t *testing.T) {
	loc := berlin(t)
	store := NewStore(t.TempDir())
	day := time.Date(2026, 6, 10, 0, 0, 0, 0, loc)
	fill(t, store, day, day.Add(24*time.Hour), map[string]float64{"pv": 1, "grid_export": 0.25, "grid_import": 0.1, "load_total": 0.4})
	// Eine Stunde aus Home Assistant ueberschreibt pv.
	store.mu.Lock()
	m, _ := store.monthLocked(day.Add(12*time.Hour), true)
	m.SetHA(day.Add(12*time.Hour), "pv", 2)
	store.mu.Unlock()

	res, err := Query(store, "year", "2026", loc, time.Date(2027, 1, 1, 0, 0, 0, 0, loc))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Buckets) != 12 {
		t.Fatalf("%d Buckets", len(res.Buckets))
	}
	june := res.Buckets[5]
	near(t, "pv Juni", june.KWh["pv"], 23+2)
	near(t, "load Juni", june.KWh["load_total"], 24*0.4)
	if june.Hours.HA != 1 || june.Hours.Own != 23 {
		t.Fatalf("Juni: %+v", june.Hours)
	}
	near(t, "autarky", *res.Total.Autarky, 1-0.1/0.4)
	near(t, "self_consumption", *res.Total.SelfConsumption, 1-6.0/25.0)
	if res.Buckets[0].Autarky != nil || res.Buckets[0].SelfConsumption != nil {
		t.Fatal("Januar ohne Daten: Quoten muessen null sein")
	}
}

func TestFutureHoursAreNotCountedAndEmptyResultEncodes(t *testing.T) {
	loc := berlin(t)
	now := time.Date(2026, 10, 11, 14, 30, 0, 0, loc)
	res, err := Query(NewStore(t.TempDir()), "day", "", loc, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.From.Day() != 11 || res.Total.Hours.None != 14 {
		t.Fatalf("From=%v, None=%d, erwartet 14 vergangene Stunden", res.From, res.Total.Hours.None)
	}
	if _, err := json.Marshal(res); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	future, err := Query(NewStore(t.TempDir()), "year", "2030", loc, now)
	if err != nil {
		t.Fatal(err)
	}
	if h := future.Total.Hours; h.None+h.Own+h.HA != 0 {
		t.Fatalf("Zukunft zaehlt Stunden: %+v", h)
	}
}

func TestInvalidPeriods(t *testing.T) {
	loc := time.UTC
	store := NewStore(t.TempDir())
	for _, c := range [][2]string{{"week", "2026-10"}, {"week", "2026-13-01"}, {"day", "2026-13-01"}, {"month", "2026"}, {"year", "1999"}, {"year", "2101"}, {"day", "gestern"}} {
		if _, err := Query(store, c[0], c[1], loc, time.Now()); !errors.Is(err, ErrInvalidPeriod) {
			t.Errorf("%v: err = %v", c, err)
		}
	}
}

func TestWeekRunsMondayToMondayAcrossDST(t *testing.T) {
	loc := berlin(t)
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, loc)
	res, err := Query(NewStore(t.TempDir()), "week", "2026-10-22", loc, now)
	if err != nil {
		t.Fatal(err)
	}
	wantFrom := time.Date(2026, 10, 19, 0, 0, 0, 0, loc)
	wantTo := time.Date(2026, 10, 26, 0, 0, 0, 0, loc)
	if !res.From.Equal(wantFrom) || !res.To.Equal(wantTo) {
		t.Fatalf("From=%v, To=%v, erwartet %v bis %v", res.From, res.To, wantFrom, wantTo)
	}
	if len(res.Buckets) != 7 {
		t.Fatalf("%d Buckets, erwartet 7", len(res.Buckets))
	}
	if res.Total.Hours.None != 169 {
		t.Fatalf("%d Stunden, erwartet 169 wegen Zeitumstellung", res.Total.Hours.None)
	}
}

func TestWeekAtResolvesToItsMonday(t *testing.T) {
	loc := berlin(t)
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, loc)
	wantFrom := time.Date(2026, 10, 19, 0, 0, 0, 0, loc)
	for _, at := range []string{"2026-10-25", "2026-10-19"} {
		res, err := Query(NewStore(t.TempDir()), "week", at, loc, now)
		if err != nil {
			t.Fatalf("%s: %v", at, err)
		}
		if !res.From.Equal(wantFrom) {
			t.Errorf("%s: From=%v, erwartet %v", at, res.From, wantFrom)
		}
	}
}

func TestWeekDefaultIsCurrentWeek(t *testing.T) {
	loc := berlin(t)
	now := time.Date(2026, 10, 14, 10, 30, 0, 0, loc)
	res, err := Query(NewStore(t.TempDir()), "week", "", loc, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 12, 0, 0, 0, 0, loc); !res.From.Equal(want) {
		t.Fatalf("From=%v, erwartet %v", res.From, want)
	}
}

func TestServiceAddsRecorderStatus(t *testing.T) {
	store := NewStore(t.TempDir())
	svc := &Service{Store: store, Recorder: NewRecorder(store, alwaysReady), Location: time.UTC, Now: func() time.Time { return hourAt("2026-10-11T12:00:00Z") }}
	resp, err := svc.Stats("day", "")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(resp)
	var decoded map[string]any
	json.Unmarshal(data, &decoded)
	for _, key := range []string{"period", "from", "to", "timezone", "buckets", "total", "status"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("Feld %q fehlt in %s", key, data)
		}
	}
}
