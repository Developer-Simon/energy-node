package autolearn

import (
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

// seasonDay is a day file with a constant load, export and PV power in W.
func seasonDay(day time.Time, load, export, pv float32) DayFile {
	f := DayFile{V: 1, Day: day.Format(dayLayout), Devices: map[string]*DeviceDay{}}
	for q := 0; q < dayQuarters; q++ {
		f.Site.Seen[q] = 1
		f.Site.LoadTotal[q] = load
		f.Site.GridExport[q] = export
		f.Site.PV[q] = pv
	}
	return f
}

// seasonKWh is the energy of constant W over all quarter hours of a day.
func seasonKWh(w float64) float64 {
	return w * dayQuarters * 0.25 / 1000
}

func TestSeasonWeekOfSevenDays(t *testing.T) {
	var days []DayFile
	var want float64
	for i := 0; i < 7; i++ {
		export := float32(500 + 100*i)
		days = append(days, seasonDay(time.Date(2026, 11, 9+i, 0, 0, 0, 0, time.UTC), 300, export, 0))
		want += seasonKWh(float64(export))
	}

	week, ok := WeekOf(days, nil, time.UTC)
	if !ok {
		t.Fatal("ok = false, want true for 7 days")
	}
	if week.Key != "2026-W46" || week.Source != "days" || week.Days != 7 {
		t.Errorf("week = %q %q %d, want 2026-W46 days 7", week.Key, week.Source, week.Days)
	}
	if math.Abs(week.ExportKWh-want) > 1e-9 {
		t.Errorf("ExportKWh = %v, want %v", week.ExportKWh, want)
	}
	if math.Abs(week.BaseW-300) > 1e-9 {
		t.Errorf("BaseW = %v, want 300", week.BaseW)
	}
	if week.TempC != nil {
		t.Errorf("TempC = %v, want nil until Task 3", *week.TempC)
	}
}

func TestSeasonWeekOfNeedsFourDays(t *testing.T) {
	var days []DayFile
	for i := 0; i < 3; i++ {
		days = append(days, seasonDay(time.Date(2026, 11, 9+i, 0, 0, 0, 0, time.UTC), 300, 100, 0))
	}
	if _, ok := WeekOf(days, nil, time.UTC); ok {
		t.Error("ok = true for 3 days, want false")
	}
}

func TestSeasonWeekOfLearnedTimesAsMinutes(t *testing.T) {
	var days []DayFile
	for i := 0; i < 4; i++ {
		days = append(days, seasonDay(time.Date(2026, 11, 9+i, 0, 0, 0, 0, time.UTC), 300, 100, 0))
	}
	week, ok := WeekOf(days, map[string][]float64{"habit/lamp": {600, 620}}, time.UTC)
	if !ok {
		t.Fatal("ok = false, want true for 4 days")
	}
	if got := week.Learned["habit/lamp"]; got != 610 {
		t.Errorf("Learned = %v, want 610 minutes of the day", got)
	}
}

func TestSeasonWeeksFromStatsSkipsMissingWeek(t *testing.T) {
	var hours []energystats.HourRecord
	addDays := func(first time.Time, n int) {
		for d := 0; d < n; d++ {
			for h := 0; h < 24; h++ {
				hours = append(hours, energystats.HourRecord{
					Hour:    first.AddDate(0, 0, d).Add(time.Duration(h) * time.Hour),
					KWh:     map[string]float64{"grid_export": 0.5, "pv": 1.0, "load_total": 0.4},
					Covered: 3600,
				})
			}
		}
	}
	addDays(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), 4)  // KW 45
	addDays(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC), 4) // KW 47, KW 46 missing

	weeks := WeeksFromStats(hours, time.UTC)
	if len(weeks) != 2 {
		t.Fatalf("len = %d, want 2", len(weeks))
	}
	if weeks[0].Key != "2026-W45" || weeks[1].Key != "2026-W47" {
		t.Errorf("keys = %q, %q, want 2026-W45, 2026-W47", weeks[0].Key, weeks[1].Key)
	}
	for _, w := range weeks {
		if w.Source != "stats" || w.Days != 4 {
			t.Errorf("week %s: source %q days %d, want stats 4", w.Key, w.Source, w.Days)
		}
		if math.Abs(w.ExportKWh-48) > 1e-9 {
			t.Errorf("week %s: ExportKWh = %v, want 48", w.Key, w.ExportKWh)
		}
		if math.Abs(w.BaseW-400) > 1e-9 {
			t.Errorf("week %s: BaseW = %v, want 400", w.Key, w.BaseW)
		}
	}
}

func TestSeasonDaysReplaceStatsNotReverse(t *testing.T) {
	store := LoadSeasonStore(filepath.Join(t.TempDir(), "season.json"), nil)

	store.Put(Week{Key: "2026-W46", Source: "stats", ExportKWh: 1})
	store.Put(Week{Key: "2026-W46", Source: "days", ExportKWh: 2})
	store.Put(Week{Key: "2026-W46", Source: "stats", ExportKWh: 3})

	got := store.Weeks("2026-W46", "2026-W46")
	if len(got) != 1 || got[0].Source != "days" || got[0].ExportKWh != 2 {
		t.Errorf("after stats, days, stats: %+v, want the days week with 2 kWh", got)
	}
}

func TestSeasonWeeksRangeInclusive(t *testing.T) {
	store := LoadSeasonStore(filepath.Join(t.TempDir(), "season.json"), nil)
	for _, k := range []string{"2026-W44", "2026-W45", "2026-W46", "2026-W47"} {
		store.Put(Week{Key: k, Source: "days"})
	}
	var keys []string
	for _, w := range store.Weeks("2026-W45", "2026-W46") {
		keys = append(keys, w.Key)
	}
	if want := []string{"2026-W45", "2026-W46"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %v, want %v", keys, want)
	}
}

func TestSeasonSameWeekOfPriorYears(t *testing.T) {
	store := LoadSeasonStore(filepath.Join(t.TempDir(), "season.json"), nil)
	y := time.Now().Year()
	for _, k := range []int{1, 2} {
		for _, w := range []int{46, 47, 48} {
			store.Put(Week{Key: fmt.Sprintf("%d-W%02d", y-k, w), Source: "days"})
		}
	}
	// Outside the answer: an older year, a week outside the window and this year.
	store.Put(Week{Key: fmt.Sprintf("%d-W47", y-3), Source: "days"})
	store.Put(Week{Key: fmt.Sprintf("%d-W45", y-1), Source: "days"})
	store.Put(Week{Key: fmt.Sprintf("%d-W47", y), Source: "days"})

	var keys []string
	for _, w := range store.Same(47, 2) {
		keys = append(keys, w.Key)
	}
	want := []string{
		fmt.Sprintf("%d-W46", y-2), fmt.Sprintf("%d-W47", y-2), fmt.Sprintf("%d-W48", y-2),
		fmt.Sprintf("%d-W46", y-1), fmt.Sprintf("%d-W47", y-1), fmt.Sprintf("%d-W48", y-1),
	}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("Same(47, 2) = %v, want %v", keys, want)
	}
}

func TestSeasonPruneDropsOlderThanThreeYears(t *testing.T) {
	store := LoadSeasonStore(filepath.Join(t.TempDir(), "season.json"), nil)
	now := testNow
	old := now.AddDate(-3, 0, -1)
	recent := now.AddDate(-1, 0, 0)
	y, w := old.ISOWeek()
	store.Put(Week{Key: fmt.Sprintf("%d-W%02d", y, w), Source: "days"})
	y, w = recent.ISOWeek()
	recentKey := fmt.Sprintf("%d-W%02d", y, w)
	store.Put(Week{Key: recentKey, Source: "days"})

	store.Prune(now)

	got := store.Weeks("0000-W00", "9999-W99")
	if len(got) != 1 || got[0].Key != recentKey {
		t.Errorf("after Prune: %+v, want only %s", got, recentKey)
	}
}

func TestSeasonSaveAndLoadEqual(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autolearn", "season.json")
	store := LoadSeasonStore(path, nil)
	store.Put(Week{
		Key: "2026-W46", Source: "days", Days: 7,
		ExportKWh: 12.5, PVKWh: 30.25, LoadKWh: 40.75, BaseW: 280,
		RunMin:   map[string]float64{"heat_pump": 120},
		Triggers: map[string]float64{"rule-a": 3},
		Learned:  map[string]float64{"habit/lamp": 610},
	})
	store.Put(Week{Key: "2026-W45", Source: "stats", Days: 4, ExportKWh: 3})
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	before := store.Weeks("0000-W00", "9999-W99")
	reloaded := LoadSeasonStore(path, nil)
	after := reloaded.Weeks("0000-W00", "9999-W99")
	if !reflect.DeepEqual(before, after) {
		t.Errorf("reloaded weeks differ:\n got %+v\nwant %+v", after, before)
	}
}
