package autolearn

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"
)

// switchTimeInput builds the input of the detector over the given days. The
// base puts all days into one group and marks days without any site data as
// absent, so the test does not depend on the day type clustering.
func switchTimeInput(t *testing.T, days []DayFile) *Input {
	t.Helper()
	loc := berlinLoc(t)
	base := Base{
		DayType:   map[string]int{},
		Groups:    [][]time.Weekday{{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday}},
		Away:      map[string]bool{},
		DayStart:  map[int]float64{},
		NightRest: map[int]float64{},
	}
	for _, f := range days {
		base.DayType[f.Day] = 0
		if f.Site.Seen[0] == 0 {
			base.Away[f.Day] = true
		}
	}
	last, err := time.ParseInLocation(dayLayout, days[len(days)-1].Day, loc)
	if err != nil {
		t.Fatal(err)
	}
	return &Input{Now: last.Add(24 * time.Hour), Loc: loc, Days: days, Base: base}
}

// switchTimeFor keeps the candidates of one device and direction.
func switchTimeFor(cands []Candidate, device, dir string) []Candidate {
	prefix := fmt.Sprintf("switch_time:%s:%s:", device, dir)
	var out []Candidate
	for _, c := range cands {
		if len(c.Finding.ID) >= len(prefix) && c.Finding.ID[:len(prefix)] == prefix {
			out = append(out, c)
		}
	}
	return out
}

func TestSwitchTimeOffAt2230IsHigh(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := synthDays(start, 14, synthOpts{
		Seed:    11,
		Devices: []string{"lamp"},
		Habits: []synthHabit{
			// The lamp must be on before it goes off, otherwise every day is
			// already in the target state.
			{Device: "lamp", On: true, Minute: 1140, Spread: 0, Hit: 1},
			{Device: "lamp", On: false, Minute: 1350, Spread: 8, Hit: 12.0 / 14.0},
		},
	})
	got := switchTimeFor(SwitchTimeDetector.Run(context.Background(), switchTimeInput(t, days), "lamp"), "lamp", "off")
	if len(got) != 1 {
		t.Fatalf("off candidates = %d, want 1", len(got))
	}
	c := got[0]
	if c.Finding.Confidence != "high" {
		t.Errorf("confidence = %s, want high (lower %.2f, spread %v)", c.Finding.Confidence, c.Lower, c.Spread["time"])
	}
	if c.Finding.Summary.Code != "switch_time.off" || c.Finding.Summary.Params["time"] != "22:30" {
		t.Errorf("summary = %+v, want switch_time.off at 22:30", c.Finding.Summary)
	}
	if c.Values["time"] != 1350 || c.Spread["time"] < 5 {
		t.Errorf("Values[time] = %v, Spread[time] = %v, want 1350 and at least 5", c.Values["time"], c.Spread["time"])
	}
	if c.Finding.Device != "lamp" || len(c.Finding.Apply) != 1 || c.Finding.Apply[0].Template != "time_point" {
		t.Errorf("apply = %+v, want one time_point fix for lamp", c.Finding.Apply)
	}
	if c.Finding.Benefit == nil || c.Finding.Benefit.Unit != "actions_week" || c.Finding.Benefit.Value <= 0 {
		t.Errorf("benefit = %+v, want actions_week above zero", c.Finding.Benefit)
	}
}

func TestSwitchTimeAroundMidnightIsOneCandidate(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := synthDays(start, 14, synthOpts{
		Seed:    12,
		Devices: []string{"lamp"},
		Habits: []synthHabit{
			{Device: "lamp", On: true, Minute: 1140, Spread: 0, Hit: 1},
			// Alternating 23:55 and 00:05, balanced at six days each. Sundays
			// have no switch, the circular median is only fair with equal halves.
			{Device: "lamp", On: false, Minute: 1435, Spread: 2, Weekdays: []time.Weekday{time.Monday, time.Wednesday, time.Friday}, Hit: 1},
			{Device: "lamp", On: false, Minute: 5, Spread: 2, Weekdays: []time.Weekday{time.Tuesday, time.Thursday, time.Saturday}, Hit: 1},
		},
	})
	got := switchTimeFor(SwitchTimeDetector.Run(context.Background(), switchTimeInput(t, days), "lamp"), "lamp", "off")
	if len(got) != 1 {
		t.Fatalf("off candidates = %d, want 1: %+v", len(got), got)
	}
	if got[0].Finding.Summary.Params["time"] != "00:00" {
		t.Errorf("time = %v, want 00:00", got[0].Finding.Summary.Params["time"])
	}
	if got[0].Finding.Confidence != "high" {
		t.Errorf("confidence = %s, want high", got[0].Finding.Confidence)
	}
}

func TestSwitchTimeStaysHighAcrossVacation(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := synthDays(start, 21, synthOpts{
		Seed:     13,
		Devices:  []string{"lamp"},
		Vacation: []synthSpan{{From: 5, To: 15}},
		Habits: []synthHabit{
			{Device: "lamp", On: true, Minute: 1140, Spread: 0, Hit: 1},
			{Device: "lamp", On: false, Minute: 1350, Spread: 8, Hit: 1},
		},
	})
	got := switchTimeFor(SwitchTimeDetector.Run(context.Background(), switchTimeInput(t, days), "lamp"), "lamp", "off")
	if len(got) != 1 {
		t.Fatalf("off candidates = %d, want 1", len(got))
	}
	if got[0].Finding.Confidence != "high" {
		t.Errorf("confidence = %s, want high (NEff %.1f)", got[0].Finding.Confidence, got[0].NEff)
	}
	if got[0].Finding.Season != nil {
		t.Errorf("season = %+v, want nil: hits cover every counted week", got[0].Finding.Season)
	}
}

func TestSwitchTimeMorningAndEveningAreTwoCandidates(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := synthDays(start, 14, synthOpts{
		Seed:    14,
		Devices: []string{"lamp"},
		Habits: []synthHabit{
			{Device: "lamp", On: true, Minute: 390, Spread: 10, Hit: 0.9},
			{Device: "lamp", On: true, Minute: 1140, Spread: 10, Hit: 0.9},
			{Device: "lamp", On: false, Minute: 480, Spread: 0, Hit: 1},
			{Device: "lamp", On: false, Minute: 1380, Spread: 0, Hit: 1},
		},
	})
	got := switchTimeFor(SwitchTimeDetector.Run(context.Background(), switchTimeInput(t, days), "lamp"), "lamp", "on")
	if len(got) != 2 {
		t.Fatalf("on candidates = %d, want 2", len(got))
	}
	times := map[string]bool{}
	for _, c := range got {
		times[c.Finding.Summary.Params["time"].(string)] = true
	}
	// The circular median of 14 days with a spread of 10 minutes lands a few
	// minutes off the simulated centre, so the check allows 10 minutes.
	near := func(clock string, want int) bool {
		var h, m int
		fmt.Sscanf(clock, "%d:%d", &h, &m)
		d := h*60 + m - want
		return d >= -10 && d <= 10
	}
	var morning, evening bool
	for clock := range times {
		morning = morning || near(clock, 390)
		evening = evening || near(clock, 1140)
	}
	if !morning || !evening {
		t.Errorf("times = %v, want about 06:30 and 19:00", times)
	}
}

func TestSwitchTimeNoiseGivesNoCandidate(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	for seed := int64(15); seed < 20; seed++ {
		days := synthDays(start, 14, synthOpts{Seed: seed, Devices: []string{"lamp"}})
		rng := rand.New(rand.NewSource(seed + 100))
		for i := range days {
			day, err := time.ParseInLocation(dayLayout, days[i].Day, loc)
			if err != nil {
				t.Fatal(err)
			}
			for k := 0; k < 3; k++ {
				at := day.Add(time.Duration(rng.Intn(1440)) * time.Minute)
				days[i].Events = append(days[i].Events, EventRec{At: at.Unix(), Device: "lamp",
					Entity: "switch.lamp", On: rng.Intn(2) == 0, Origin: string(OriginHand)})
			}
		}
		if got := SwitchTimeDetector.Run(context.Background(), switchTimeInput(t, days), "lamp"); len(got) != 0 {
			t.Errorf("seed %d: noise gave %d candidates, first %s", seed, len(got), got[0].Finding.ID)
		}
	}
}

// TestSwitchTimeMidnightReadsPreviousDay: the off habit is at 00:02. Day 1 ends
// with the lamp off, so day 2 at 00:02 is already in the target state and must
// not count. Day 0 has no previous day and does not count either. Reading the
// quarter before from the same day would count day 1 and not day 2, which
// gives 13 days instead of 12.
func TestSwitchTimeMidnightReadsPreviousDay(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	var days []DayFile
	for i := 0; i < 14; i++ {
		day := start.AddDate(0, 0, i)
		dd := &DeviceDay{}
		for q := range dd.Seen {
			dd.Seen[q] = dayCounterMax
			dd.Reach[q] = dayCounterMax
			dd.OnMin[q] = 15
		}
		if i == 1 {
			dd.OnMin[dayQuarters-1] = 0
		}
		f := DayFile{V: dayFileVersion, Day: day.Format(dayLayout), Devices: map[string]*DeviceDay{"lamp": dd},
			Events:  []EventRec{{At: day.Add(2 * time.Minute).Unix(), Device: "lamp", Entity: "switch.lamp", Origin: string(OriginHand)}},
			Actions: []ActionRec{}}
		for q := range f.Site.Seen {
			f.Site.Seen[q] = dayCounterMax
		}
		days = append(days, f)
	}
	got := switchTimeFor(SwitchTimeDetector.Run(context.Background(), switchTimeInput(t, days), "lamp"), "lamp", "off")
	if len(got) != 1 {
		t.Fatalf("off candidates = %d, want 1", len(got))
	}
	if got[0].Finding.Summary.Params["time"] != "00:00" {
		t.Errorf("time = %v, want 00:00", got[0].Finding.Summary.Params["time"])
	}
	if d := got[0].Finding.Summary.Params["days"]; d != 12 {
		t.Errorf("counted days = %v, want 12", d)
	}
}

func TestSwitchTimeSeasonNilAcrossYearEnd(t *testing.T) {
	loc := berlinLoc(t)
	var refs []swRef
	for d := 0; d < 42; d += 7 {
		refs = append(refs, swRef{t: time.Date(2025, 12, 1, 0, 0, 0, 0, loc).AddDate(0, 0, d)})
	}
	counted := []int{0, 1, 2, 3, 4, 5}
	// Hits in weeks 52 and 1 of the year end: a range 52 to 1 would wrap.
	if got := switchTimeSeason(refs, counted, []int{3, 4}); got != nil {
		t.Errorf("season across year end = %+v, want nil", got)
	}
	// Hits in weeks 50 and 51 stay a plain range.
	if got := switchTimeSeason(refs, counted, []int{1, 2}); got == nil || got.FromWeek != 50 || got.ToWeek != 51 {
		t.Errorf("season = %+v, want weeks 50 to 51", got)
	}
}

func TestSwitchTimeRuleEventsGiveNoCandidate(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := synthDays(start, 14, synthOpts{
		Seed:    17,
		Devices: []string{"lamp"},
		Rules:   []synthRule{{RuleID: "r1", Device: "lamp", On: false, Minute: 1350, Spread: 8, Hit: 1}},
	})
	rules := 0
	for _, f := range days {
		for _, ev := range f.Events {
			if ev.Origin == string(OriginRule) {
				rules++
			}
		}
	}
	if rules == 0 {
		t.Fatal("test data has no rule events")
	}
	if got := SwitchTimeDetector.Run(context.Background(), switchTimeInput(t, days), "lamp"); len(got) != 0 {
		t.Errorf("rule events gave %d candidates, want 0", len(got))
	}
}

func TestQuietSpreadNeverBelowFiveOrNaN(t *testing.T) {
	cases := map[string]struct{ in, want float64 }{
		"nan":   {math.NaN(), 5},
		"small": {2, 5},
		"large": {8, 8},
	}
	for name, c := range cases {
		if got := quietSpread(c.in); got != c.want {
			t.Errorf("%s: quietSpread(%v) = %v, want %v", name, c.in, got, c.want)
		}
	}
}
