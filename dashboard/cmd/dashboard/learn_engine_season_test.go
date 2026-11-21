package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

// seasonTestDays returns n day files starting at the given day, each with a
// valid date and an empty site.
func seasonTestDays(start time.Time, n int) []autolearn.DayFile {
	out := make([]autolearn.DayFile, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, autolearn.DayFile{Day: start.AddDate(0, 0, i).Format("2006-01-02")})
	}
	return out
}

// newSeasonCloserForTest builds a closer on a fresh store file. The days func
// counts its calls and returns the given day files.
func newSeasonCloserForTest(t *testing.T, days []autolearn.DayFile, calls *int) (*seasonCloser, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "season.json")
	store := autolearn.LoadSeasonStore(path, t.Logf)
	closer := &seasonCloser{
		store: store,
		days: func(from, to time.Time) ([]autolearn.DayFile, error) {
			*calls++
			return days, nil
		},
		learned: func() map[string][]float64 { return nil },
		loc:     time.UTC,
	}
	return closer, path
}

// Wednesday 2026-11-25 closes ISO week 2026-W47 (Mon 16 to Sun 22 November).
var (
	seasonWednesday = time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	seasonPrevMon   = time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
)

func TestSeasonCloserClosesPreviousWeekOnWednesday(t *testing.T) {
	calls := 0
	closer, path := newSeasonCloserForTest(t, seasonTestDays(seasonPrevMon, 4), &calls)

	closer.Run(seasonWednesday)

	if calls != 1 {
		t.Fatalf("days calls = %d, want 1", calls)
	}
	if got := closer.store.Weeks("2026-W47", "2026-W47"); len(got) != 1 || got[0].Source != "days" || got[0].Days != 4 {
		t.Fatalf("weeks 2026-W47 = %+v, want one day-built week with 4 days", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("season store not saved: %v", err)
	}
}

func TestSeasonCloserSameDayIsAttemptedOnce(t *testing.T) {
	calls := 0
	// Only three day files, so the week is not closed and the store stays
	// without it. The second run on the same day must still not read the days.
	closer, _ := newSeasonCloserForTest(t, seasonTestDays(seasonPrevMon, 3), &calls)

	closer.Run(seasonWednesday)
	closer.Run(seasonWednesday.Add(5 * time.Hour))

	if calls != 1 {
		t.Fatalf("days calls = %d, want 1", calls)
	}
}

func TestSeasonCloserSkipsWeekAlreadyInStore(t *testing.T) {
	calls := 0
	closer, _ := newSeasonCloserForTest(t, seasonTestDays(seasonPrevMon, 4), &calls)
	closer.store.Put(autolearn.Week{Key: "2026-W47", Source: "days", Days: 7})

	closer.Run(seasonWednesday)

	if calls != 0 {
		t.Fatalf("days calls = %d, want 0", calls)
	}
}

func TestSeasonCloserFewerThanFourDaysPutsNoWeek(t *testing.T) {
	calls := 0
	closer, _ := newSeasonCloserForTest(t, seasonTestDays(seasonPrevMon, 3), &calls)

	closer.Run(seasonWednesday)

	if calls != 1 {
		t.Fatalf("days calls = %d, want 1", calls)
	}
	if got := closer.store.Weeks("2026-W47", "2026-W47"); len(got) != 0 {
		t.Fatalf("weeks 2026-W47 = %+v, want none", got)
	}
}

func TestBackfillSeasonFillsWeeksFromStats(t *testing.T) {
	stats := energystats.NewStore(t.TempDir())
	for _, h := range []time.Time{
		time.Date(2026, 11, 16, 3, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 17, 3, 0, 0, 0, time.UTC),
	} {
		rec := energystats.HourRecord{Hour: h, KWh: map[string]float64{"load_total": 0.4}, Covered: 3600}
		if err := stats.AddOwn(rec); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "season.json")
	store := autolearn.LoadSeasonStore(path, t.Logf)

	if err := backfillSeason(store, stats, seasonWednesday, time.UTC); err != nil {
		t.Fatal(err)
	}

	got := store.Weeks("2026-W47", "2026-W47")
	if len(got) != 1 || got[0].Source != "stats" {
		t.Fatalf("weeks 2026-W47 = %+v, want one stats week", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("season store not saved: %v", err)
	}
}

func TestBackfillSeasonSkipsCurrentWeek(t *testing.T) {
	stats := energystats.NewStore(t.TempDir())
	// seasonWednesday is in ISO week 2026-W48, which is still running.
	for _, h := range []time.Time{
		time.Date(2026, 11, 16, 3, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 23, 3, 0, 0, 0, time.UTC),
	} {
		rec := energystats.HourRecord{Hour: h, KWh: map[string]float64{"load_total": 0.4}, Covered: 3600}
		if err := stats.AddOwn(rec); err != nil {
			t.Fatal(err)
		}
	}
	store := autolearn.LoadSeasonStore(filepath.Join(t.TempDir(), "season.json"), t.Logf)

	if err := backfillSeason(store, stats, seasonWednesday, time.UTC); err != nil {
		t.Fatal(err)
	}

	if got := store.Weeks("2026-W48", "2026-W48"); len(got) != 0 {
		t.Fatalf("weeks 2026-W48 = %+v, want none for the running week", got)
	}
	if got := store.Weeks("2026-W47", "2026-W47"); len(got) != 1 || got[0].Source != "stats" {
		t.Fatalf("weeks 2026-W47 = %+v, want one stats week", got)
	}
}

func TestSeasonCloserReplacesStatsWeekWithDays(t *testing.T) {
	calls := 0
	closer, _ := newSeasonCloserForTest(t, seasonTestDays(seasonPrevMon, 4), &calls)
	closer.store.Put(autolearn.Week{Key: "2026-W47", Source: "stats", Days: 7})

	closer.Run(seasonWednesday)

	if calls != 1 {
		t.Fatalf("days calls = %d, want 1 for a week that has only statistics", calls)
	}
	if got := closer.store.Weeks("2026-W47", "2026-W47"); len(got) != 1 || got[0].Source != "days" || got[0].Days != 4 {
		t.Fatalf("weeks 2026-W47 = %+v, want the day-built week", got)
	}
}

// TestDailyShareSaverRecordsOnlyWhenRunning checks that the hourly share is
// written only while a group of the engine is on and after a full day of running.
func TestDailyShareSaverRecordsOnlyWhenRunning(t *testing.T) {
	start := time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)
	clock := start
	now := func() time.Time { return clock }
	on := func() map[string]bool { return map[string]bool{"habit": true, "energy": false} }
	off := func() map[string]bool { return map[string]bool{"habit": false, "energy": false} }
	cases := []struct {
		name   string
		groups func() map[string]bool
		after  time.Duration
		want   bool
	}{
		{"groups off after a day", off, 25 * time.Hour, false},
		{"running for an hour", on, time.Hour, false},
		{"running for a day", on, 24 * time.Hour, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock = start
			path := filepath.Join(t.TempDir(), "learn.json")
			var mu sync.Mutex
			st := learnrec.State{}
			engine := autolearn.NewEngine(nil, nil, nil,
				func() map[string]autolearn.DeviceInfo { return map[string]autolearn.DeviceInfo{} },
				func() learnrec.Limits { return learnrec.LimitsFor(learnrec.LevelBalanced, 4) }, time.UTC)
			saver := dailyShareSaver(engine, tc.groups, &mu, &st, path, now)

			clock = start.Add(tc.after)
			if err := saver.Save(); err != nil {
				t.Fatal(err)
			}

			_, recorded := st.DailyShare[clock.Format("2006-01-02")]
			if recorded != tc.want {
				t.Errorf("recorded = %v, want %v", recorded, tc.want)
			}
			_, statErr := os.Stat(path)
			if (statErr == nil) != tc.want {
				t.Errorf("state file written = %v, want %v", statErr == nil, tc.want)
			}
		})
	}
}
