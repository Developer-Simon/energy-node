package autolearn

import (
	"fmt"
	"math"
	"os"
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

func TestSeasonWeekOfBaseIsMedianOfNightWindows(t *testing.T) {
	// Daytime load is 1000 W everywhere. The night window 00:00 to 05:00 is
	// 200, 300, 400 and 900 W on the four days. The median is 350, the mean 450.
	nights := []float32{200, 300, 400, 900}
	var days []DayFile
	for i, night := range nights {
		d := seasonDay(time.Date(2026, 11, 9+i, 0, 0, 0, 0, time.UTC), 1000, 100, 0)
		for q := 0; q < seasonNightQuarters; q++ {
			d.Site.LoadTotal[q] = night
		}
		days = append(days, d)
	}
	week, ok := WeekOf(days, nil, time.UTC)
	if !ok {
		t.Fatal("ok = false, want true for 4 days")
	}
	if math.Abs(week.BaseW-350) > 1e-9 {
		t.Errorf("BaseW = %v, want 350 (median of the night values)", week.BaseW)
	}
}

func TestSeasonWeekOfLearnedTimesAsMedian(t *testing.T) {
	var days []DayFile
	for i := 0; i < 4; i++ {
		days = append(days, seasonDay(time.Date(2026, 11, 9+i, 0, 0, 0, 0, time.UTC), 300, 100, 0))
	}
	// 1200 is an outlier: the median 610 is the usual time, the mean would be 806.
	week, ok := WeekOf(days, map[string][]float64{"habit/lamp": {600, 610, 1200}}, time.UTC)
	if !ok {
		t.Fatal("ok = false, want true for 4 days")
	}
	if got := week.Learned["habit/lamp"]; got != 610 {
		t.Errorf("Learned = %v, want 610 minutes of the day (median)", got)
	}
}

func TestSeasonWeeksFromStatsSkipsMissingWeek(t *testing.T) {
	var hours []energystats.HourRecord
	// Night load per day (kWh per hour) for 00:00 to 05:00: 0.2, 0.3, 0.4, 0.9.
	// Daytime load is 1.0 kWh per hour. The median of the nights is 0.35 kWh, so
	// 350 W, the mean would be 450 W.
	nights := []float64{0.2, 0.3, 0.4, 0.9}
	addDays := func(first time.Time) {
		for d := range nights {
			for h := 0; h < 24; h++ {
				load := 1.0
				if h < 5 {
					load = nights[d]
				}
				hours = append(hours, energystats.HourRecord{
					Hour:    first.AddDate(0, 0, d).Add(time.Duration(h) * time.Hour),
					KWh:     map[string]float64{"grid_export": 0.5, "pv": 1.0, "load_total": load},
					Covered: 3600,
				})
			}
		}
	}
	addDays(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC))  // KW 45
	addDays(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)) // KW 47, KW 46 missing

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
		if math.Abs(w.BaseW-350) > 1e-9 {
			t.Errorf("week %s: BaseW = %v, want 350 (median of the nights)", w.Key, w.BaseW)
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

// seasonTempDay sets the outdoor temperature of the given quarters of a day file.
func seasonTempDay(f DayFile, temp float32, quarters ...int) DayFile {
	for _, q := range quarters {
		v := temp
		f.Site.TempC[q] = &v
	}
	return f
}

func TestSeasonWeekOfTempIsMeanOfDays(t *testing.T) {
	var days []DayFile
	for i := 0; i < 7; i++ {
		var quarters []int
		for q := 0; q < dayQuarters; q++ {
			quarters = append(quarters, q)
		}
		days = append(days, seasonTempDay(seasonDay(time.Date(2026, 11, 9+i, 0, 0, 0, 0, time.UTC), 300, 0, 0), float32(i), quarters...))
	}
	week, ok := WeekOf(days, nil, time.UTC)
	if !ok {
		t.Fatal("ok = false, want true for 7 days")
	}
	if week.TempC == nil || math.Abs(*week.TempC-3) > 1e-9 {
		t.Errorf("TempC = %v, want the mean of the daily means 3", week.TempC)
	}
}

func TestSeasonWeekOfTempCountsOnlyDaysWithTemperature(t *testing.T) {
	var days []DayFile
	for i := 0; i < 4; i++ {
		days = append(days, seasonDay(time.Date(2026, 11, 9+i, 0, 0, 0, 0, time.UTC), 300, 0, 0))
	}
	if week, ok := WeekOf(days, nil, time.UTC); !ok || week.TempC != nil {
		t.Fatalf("TempC = %v, want nil without any temperature", week.TempC)
	}
	// Day 5 has one quarter of 10 degC, day 6 has two quarters of 20 and 30 degC:
	// the daily means are 10 and 25, the week mean is 17.5. The four days without
	// temperature do not count.
	days = append(days, seasonTempDay(seasonDay(time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC), 300, 0, 0), 10, 0))
	days = append(days, seasonTempDay(seasonDay(time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC), 300, 0, 0), 20, 0))
	days[len(days)-1] = seasonTempDay(days[len(days)-1], 30, 1)
	week, ok := WeekOf(days, nil, time.UTC)
	if !ok || week.TempC == nil {
		t.Fatalf("ok = %v TempC = %v, want a mean", ok, week.TempC)
	}
	if math.Abs(*week.TempC-17.5) > 1e-9 {
		t.Errorf("TempC = %v, want 17.5 (mean of the daily means 10 and 25)", *week.TempC)
	}
}

// TestSeasonPriorEmptyShareReadsPriorYears: the median of the empty share of the
// same weeks of the years before. The current year does not count, and a week
// without a share gives no answer.
func TestSeasonPriorEmptyShareReadsPriorYears(t *testing.T) {
	store := LoadSeasonStore(filepath.Join(t.TempDir(), "season.json"), nil)
	y := time.Now().Year()
	a, b, cur := 0.8, 0.6, 0.1
	store.Put(Week{Key: fmt.Sprintf("%d-W45", y-1), Source: seasonSourceDays, EmptyShare: &a})
	store.Put(Week{Key: fmt.Sprintf("%d-W45", y-2), Source: seasonSourceDays, EmptyShare: &b})
	store.Put(Week{Key: fmt.Sprintf("%d-W45", y), Source: seasonSourceDays, EmptyShare: &cur})
	store.Put(Week{Key: fmt.Sprintf("%d-W20", y-1), Source: seasonSourceDays})
	if got, ok := store.PriorEmptyShare(45); !ok || math.Abs(got-0.7) > 1e-9 {
		t.Errorf("PriorEmptyShare(45) = %v %v, want median 0.7 true", got, ok)
	}
	if _, ok := store.PriorEmptyShare(10); ok {
		t.Errorf("PriorEmptyShare(10) gave an answer without a record")
	}
	var nilStore *SeasonStore
	if _, ok := nilStore.PriorEmptyShare(45); ok {
		t.Errorf("nil store gave an answer")
	}
}

// TestSeasonWeekWithoutEmptyShareStillLoads: a season file written before the
// empty share has weeks without the field, they load with a nil share.
func TestSeasonWeekWithoutEmptyShareStillLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "season.json")
	old := `{"version":1,"weeks":[{"key":"2026-W46","source":"days","days":7,"base_w":280}]}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	store := LoadSeasonStore(path, nil)
	ws := store.Weeks("2026-W46", "2026-W46")
	if len(ws) != 1 || ws[0].BaseW != 280 || ws[0].EmptyShare != nil {
		t.Fatalf("old week = %+v, want base 280 and no empty share", ws)
	}
}

func TestSeasonEmptyShareSavedAndLoaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "season.json")
	share := 0.25
	store := LoadSeasonStore(path, nil)
	store.Put(Week{Key: "2026-W46", Source: seasonSourceDays, Days: 7, EmptyShare: &share})
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	ws := LoadSeasonStore(path, nil).Weeks("2026-W46", "2026-W46")
	if len(ws) != 1 || ws[0].EmptyShare == nil || *ws[0].EmptyShare != 0.25 {
		t.Fatalf("reloaded week = %+v, want empty share 0.25", ws)
	}
}
