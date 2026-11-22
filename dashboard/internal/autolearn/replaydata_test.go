package autolearn

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

// fakeMinutes is a learnrec.Reader over whole days. A day present in days
// counts as covered. Minutes of a day without an entry are gaps.
type fakeMinutes struct {
	loc  *time.Location
	days map[string]map[string][]float32
}

func (f fakeMinutes) Range(key string, from, to time.Time) learnrec.Series {
	n := int(to.Sub(from) / time.Minute)
	out := learnrec.Series{From: from, Values: make([]float32, n)}
	for i := range out.Values {
		t := from.Add(time.Duration(i) * time.Minute).In(f.loc)
		vals := f.days[t.Format(dayLayout)][key]
		if vals == nil {
			out.Values[i] = float32(math.NaN())
			continue
		}
		out.Values[i] = vals[t.Hour()*60+t.Minute()]
	}
	return out
}

func (f fakeMinutes) Coverage(from, to time.Time) int {
	n := 0
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		if _, ok := f.days[d.In(f.loc).Format(dayLayout)]; ok {
			n++
		}
	}
	return n
}

// quarterDay is a day file with every quarter hour seen and the load total
// given by load.
func quarterDay(day string, load func(q int) float32) DayFile {
	f := DayFile{Day: day, Devices: map[string]*DeviceDay{}}
	for q := 0; q < dayQuarters; q++ {
		f.Site.Seen[q] = 1
		f.Site.LoadTotal[q] = load(q)
	}
	return f
}

// minuteValues is a full day of one key with the same value in every minute.
func minuteValues(v float32) []float32 {
	out := make([]float32, 1440)
	for i := range out {
		out[i] = v
	}
	return out
}

func replayTestNow() time.Time {
	return time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
}

func TestReplayDaysMixesMinutesAndStretchedQuarters(t *testing.T) {
	loc := time.UTC
	now := replayTestNow()
	// Yesterday and the day before have minutes, the twelve days before them
	// only quarter hours.
	minutes := fakeMinutes{loc: loc, days: map[string]map[string][]float32{
		"2026-11-21": {"b:load_total": minuteValues(500)},
		"2026-11-20": {"b:load_total": minuteValues(500)},
	}}
	in := &Input{Now: now, Loc: loc, Minutes: minutes}
	for d := 8; d <= 19; d++ {
		in.Days = append(in.Days, quarterDay(time.Date(2026, 11, d, 0, 0, 0, 0, loc).Format(dayLayout),
			func(int) float32 { return 300 }))
	}

	got, est := ReplayDays(in, 14)
	if len(got) != 14*1440 {
		t.Fatalf("len = %d, want %d", len(got), 14*1440)
	}
	if !est {
		t.Fatal("estimate = false, want true because twelve days are stretched")
	}
	// The first minute day starts at 2026-11-08 and the minutes of 2026-11-20
	// carry the minute values.
	idx := 12 * 1440
	if got[idx].At.Format(dayLayout) != "2026-11-20" {
		t.Fatalf("minute %d at %s, want 2026-11-20", idx, got[idx].At)
	}
	if got[idx].Balance["load_total"] != 500 {
		t.Fatalf("minute day load = %v, want 500", got[idx].Balance["load_total"])
	}
}

func TestReplayDaysWithoutMinutesIsRepeatable(t *testing.T) {
	loc := time.UTC
	ramp := func(q int) float32 { return float32(100 + 10*q) }
	in := &Input{Now: replayTestNow(), Loc: loc, Days: []DayFile{quarterDay("2026-11-21", ramp)},
		Learned: map[string]LearnedEntry{
			"site/pv_reserve_w": {Key: "site/pv_reserve_w", Kind: ValueNumber, Value: 1500.0,
				At: time.Date(2026, 11, 1, 0, 0, 0, 0, loc).Unix()},
		}}

	first, est := ReplayDays(in, 1)
	if !est {
		t.Fatal("estimate = false, want true for a stretched day")
	}
	if len(first) != 1440 {
		t.Fatalf("len = %d, want 1440", len(first))
	}
	second, _ := ReplayDays(in, 1)
	for i := range first {
		if first[i].Balance["load_total"] != second[i].Balance["load_total"] {
			t.Fatalf("minute %d differs between two runs", i)
		}
	}

	// The mean of every quarter hour is its value within one percent.
	for q := 0; q < dayQuarters; q++ {
		want := float64(ramp(q))
		sum := 0.0
		for m := 0; m < 15; m++ {
			sum += first[q*15+m].Balance["load_total"]
		}
		if mean := sum / 15; math.Abs(mean-want) > 0.01*want {
			t.Fatalf("quarter %d mean = %v, want %v within 1 %%", q, mean, want)
		}
	}

	if got := first[0].Learned["site/pv_reserve_w"]; got != 1500 {
		t.Fatalf("learned value = %v, want 1500", got)
	}
}

func TestReplayDaysLeavesOutFutureLearnedValues(t *testing.T) {
	loc := time.UTC
	now := replayTestNow()
	in := &Input{Now: now, Loc: loc, Days: []DayFile{quarterDay("2026-11-21", func(int) float32 { return 1 })},
		Learned: map[string]LearnedEntry{
			"site/pv_reserve_w": {Key: "site/pv_reserve_w", Kind: ValueNumber, Value: 1500.0, At: now.Unix()},
		}}
	got, _ := ReplayDays(in, 1)
	if len(got) == 0 {
		t.Fatal("no minutes")
	}
	if _, ok := got[0].Learned["site/pv_reserve_w"]; ok {
		t.Fatal("a value learned after the minute must not be in that minute")
	}
}

func TestMinuteNoiseFollowsTheNoiseBand(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, 11, 21, 0, 0, 0, 0, loc)
	rng := rand.New(rand.NewSource(1))
	series := make([]float32, 1440)
	for i := range series {
		series[i] = float32(500 + 80*(2*rng.Float64()-1))
	}
	r := fakeMinutes{loc: loc, days: map[string]map[string][]float32{
		"2026-11-21": {"b:load_total": series},
	}}
	got, ok := MinuteNoise(r, "b:load_total", 500, start, start.Add(24*time.Hour))
	if !ok {
		t.Fatal("ok = false, want a noise estimate")
	}
	if math.Abs(got-80) > 15 {
		t.Fatalf("noise = %v, want 80 within 15", got)
	}
}

func TestRealStateFollowsEvents(t *testing.T) {
	loc := time.UTC
	day := time.Date(2026, 11, 21, 0, 0, 0, 0, loc)
	at := func(h, m int) int64 { return day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute).Unix() }
	f := quarterDay("2026-11-21", func(int) float32 { return 0 })
	f.Devices["plug.x"] = &DeviceDay{}
	f.Events = []EventRec{
		{At: at(8, 0), Device: "plug.x", On: true},
		{At: at(9, 0), Device: "plug.x", On: false},
	}
	in := &Input{Now: replayTestNow(), Loc: loc, Days: []DayFile{f}}
	minutes := []ReplayMinute{
		{At: day.Add(7*time.Hour + 59*time.Minute)},
		{At: day.Add(8 * time.Hour)},
		{At: day.Add(8*time.Hour + 30*time.Minute)},
		{At: day.Add(9 * time.Hour)},
		{At: day.Add(10 * time.Hour)},
	}
	want := []bool{false, true, true, false, false}
	got := RealState(in, "plug.x", minutes)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("minute %s: on = %v, want %v", minutes[i].At.Format("15:04"), got[i], want[i])
		}
	}
}

func TestReplayLastYearNeedsHourlyValues(t *testing.T) {
	loc := time.UTC
	now := replayTestNow()
	in := &Input{Now: now, Loc: loc}
	stats := energystats.NewStore(t.TempDir())

	if got, ok := ReplayLastYear(in, stats, 2); ok || got != nil {
		t.Fatalf("without hourly values: ok = %v, len = %d, want false and nil", ok, len(got))
	}

	// Wednesday 2025-11-12 lies in the week one year before the first week.
	hour := time.Date(2025, 11, 12, 8, 0, 0, 0, loc)
	if err := stats.AddOwn(energystats.HourRecord{Hour: hour, KWh: map[string]float64{"load_total": 0.5}, Covered: 3600}); err != nil {
		t.Fatal(err)
	}
	got, ok := ReplayLastYear(in, stats, 2)
	if !ok {
		t.Fatal("ok = false, want true with an hourly value")
	}
	if len(got) != 2*7*1440 {
		t.Fatalf("len = %d, want %d minutes for two weeks", len(got), 2*7*1440)
	}
	var found int
	for _, m := range got {
		if !m.At.Before(hour) && m.At.Before(hour.Add(time.Hour)) && m.Balance["load_total"] == 500 {
			found++
		}
	}
	if found != 60 {
		t.Fatalf("minutes of the hour at 500 W = %d, want 60", found)
	}
}

func TestStretchedDayBorrowsTheMinuteShape(t *testing.T) {
	loc := time.UTC
	// The minute day has a ramp of 10 W per minute inside every quarter hour,
	// mean 170 W. The stretched day has 170 W in every quarter hour.
	ramp := make([]float32, 1440)
	for i := range ramp {
		ramp[i] = float32(100 + 10*(i%15))
	}
	minutes := fakeMinutes{loc: loc, days: map[string]map[string][]float32{
		"2026-11-21": {"b:load_total": ramp},
	}}
	in := &Input{Now: replayTestNow(), Loc: loc, Minutes: minutes,
		Days: []DayFile{quarterDay("2026-11-20", func(int) float32 { return 170 })}}

	got, est := ReplayDays(in, 2)
	if !est {
		t.Fatal("estimate = false, want true for the stretched day")
	}
	if len(got) != 2*1440 {
		t.Fatalf("len = %d, want %d", len(got), 2*1440)
	}
	for q := 0; q < dayQuarters; q++ {
		sum := 0.0
		for m := 0; m < 15; m++ {
			want := float64(100 + 10*m)
			v := got[q*15+m].Balance["load_total"]
			if math.Abs(v-want) > 1e-6 {
				t.Fatalf("quarter %d minute %d = %v, want the ramp value %v", q, m, v, want)
			}
			sum += v
		}
		if mean := sum / 15; math.Abs(mean-170) > 0.01*170 {
			t.Fatalf("quarter %d mean = %v, want 170 within 1 %%", q, mean)
		}
	}
}

func TestEngineInputCarriesTheMinuteReader(t *testing.T) {
	loc := time.UTC
	e := newTestEngine(t, learnrec.Limits{})
	reader := fakeMinutes{loc: loc, days: map[string]map[string][]float32{}}
	if got := e.input(replayTestNow(), learnrec.Limits{}, nil); got.Minutes != nil {
		t.Fatal("without SetMinutes the input has a reader")
	}
	e.SetMinutes(func() learnrec.Reader { return reader })
	if got := e.input(replayTestNow(), learnrec.Limits{}, nil); got.Minutes == nil {
		t.Fatal("after SetMinutes the input has no reader")
	}
}

func TestEngineInputCarriesTheSteps(t *testing.T) {
	e := newTestEngine(t, learnrec.Limits{})
	if got := e.input(replayTestNow(), learnrec.Limits{}, nil); got.Steps != nil {
		t.Fatal("without SetSteps the input has a step reader")
	}
	e.SetSteps(func(string, time.Time, time.Time) []Step { return nil })
	if got := e.input(replayTestNow(), learnrec.Limits{}, nil); got.Steps == nil {
		t.Fatal("after SetSteps the input has no step reader")
	}
}
