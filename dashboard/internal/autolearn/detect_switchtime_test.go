package autolearn

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
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
	if c.Finding.Device != "lamp" || len(c.Finding.Apply) != 2 || c.Finding.Apply[0].Template != "time_point" {
		t.Errorf("apply = %+v, want a time_point fix and a learned one for lamp", c.Finding.Apply)
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

// switchTimeWeekGroups gives the input two day type groups, Monday to Friday
// and Saturday with Sunday, and the days their dates belong to.
func switchTimeWeekGroups(t *testing.T, days []DayFile) *Input {
	t.Helper()
	in := switchTimeInput(t, days)
	in.Base.Groups = [][]time.Weekday{
		{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
		{time.Saturday, time.Sunday},
	}
	for _, f := range days {
		day, err := time.ParseInLocation(dayLayout, f.Day, in.Loc)
		if err != nil {
			t.Fatal(err)
		}
		in.Base.DayType[f.Day] = 0
		if wd := day.Weekday(); wd == time.Saturday || wd == time.Sunday {
			in.Base.DayType[f.Day] = 1
		}
	}
	return in
}

// TestSwitchTimePrefillDaysStartAtMonday: the rule document counts weekdays
// from Monday (0) to Sunday (6), so Monday to Friday gives [0 1 2 3 4] and
// Saturday with Sunday gives [5 6].
func TestSwitchTimePrefillDaysStartAtMonday(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc) // Monday
	weekend := []time.Weekday{time.Saturday, time.Sunday}
	weekdays := []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
	days := synthDays(start, 42, synthOpts{
		Seed:    18,
		Devices: []string{"lamp"},
		Habits: []synthHabit{
			{Device: "lamp", On: true, Minute: 1140, Spread: 0, Weekdays: weekdays, Hit: 1},
			{Device: "lamp", On: false, Minute: 1350, Spread: 8, Weekdays: weekdays, Hit: 1},
			{Device: "lamp", On: true, Minute: 540, Spread: 0, Weekdays: weekend, Hit: 1},
			{Device: "lamp", On: false, Minute: 600, Spread: 8, Weekdays: weekend, Hit: 1},
		},
	})
	got := SwitchTimeDetector.Run(context.Background(), switchTimeWeekGroups(t, days), "lamp")
	// Habit centres and the weekdays their prefill must carry.
	type habit struct {
		minute int
		days   []int
	}
	habits := []habit{{minute: 1350, days: []int{0, 1, 2, 3, 4}}, {minute: 600, days: []int{5, 6}}}
	found := make([]bool, len(habits))
	for _, c := range got {
		if c.Finding.Summary.Code != "switch_time.off" {
			continue
		}
		clock := c.Finding.Summary.Params["time"].(string)
		var h, m int
		fmt.Sscanf(clock, "%d:%d", &h, &m)
		for k, hb := range habits {
			if math.Abs(float64(h*60+m-hb.minute)) > 15 {
				continue
			}
			days, ok := c.Finding.Apply[0].Prefill["days"].([]int)
			if !ok {
				t.Fatalf("prefill days has type %T", c.Finding.Apply[0].Prefill["days"])
			}
			if !reflect.DeepEqual(days, hb.days) {
				t.Errorf("time %s: prefill days = %v, want %v", clock, days, hb.days)
			}
			found[k] = true
		}
	}
	for k, hb := range habits {
		if !found[k] {
			t.Errorf("no off candidate near minute %d, got %d candidates", hb.minute, len(got))
		}
	}
}

// TestSwitchTimePrefillCarriesSwitchEntity: the assistant selects the switch
// entity, not the device, so the prefill carries the entity of the events.
func TestSwitchTimePrefillCarriesSwitchEntity(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := synthDays(start, 14, synthOpts{
		Seed:    11,
		Devices: []string{"lamp"},
		Habits: []synthHabit{
			{Device: "lamp", On: true, Minute: 1140, Spread: 0, Hit: 1},
			{Device: "lamp", On: false, Minute: 1350, Spread: 8, Hit: 12.0 / 14.0},
		},
	})
	got := switchTimeFor(SwitchTimeDetector.Run(context.Background(), switchTimeInput(t, days), "lamp"), "lamp", "off")
	if len(got) != 1 {
		t.Fatalf("off candidates = %d, want 1", len(got))
	}
	p := got[0].Finding.Apply[0].Prefill
	if p["device"] != "lamp" || p["entity"] != "switch.lamp" {
		t.Errorf("prefill device and entity = %v and %v, want lamp and switch.lamp", p["device"], p["entity"])
	}
}

func TestSwitchTimeCandidateOffersLearnedSecondApply(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := synthDays(start, 14, synthOpts{
		Seed:    11,
		Devices: []string{"lamp"},
		Habits: []synthHabit{
			{Device: "lamp", On: true, Minute: 1140, Spread: 0, Hit: 1},
			{Device: "lamp", On: false, Minute: 1350, Spread: 8, Hit: 12.0 / 14.0},
		},
	})
	got := switchTimeFor(SwitchTimeDetector.Run(context.Background(), switchTimeInput(t, days), "lamp"), "lamp", "off")
	if len(got) != 1 {
		t.Fatalf("off candidates = %d, want 1", len(got))
	}
	apply := got[0].Finding.Apply
	if len(apply) != 2 {
		t.Fatalf("apply = %+v, want two entries", apply)
	}
	if apply[0].Code != "time_point" || apply[0].Prefill["learned"] != nil {
		t.Errorf("first apply = %+v, want a fixed time_point", apply[0])
	}
	second := apply[1]
	if second.Code != "time_point_learned" || second.Template != "time_point" {
		t.Errorf("second apply = %+v, want time_point_learned on template time_point", second)
	}
	if second.Prefill["learned"] != "device/lamp/usual_off" {
		t.Errorf("learned = %v, want device/lamp/usual_off", second.Prefill["learned"])
	}
	if second.Prefill["device"] != "lamp" || second.Prefill["time"] != "22:30" || second.Prefill["on"] != false {
		t.Errorf("second prefill = %+v, want the prefill of the first entry", second.Prefill)
	}
}

// sunHabitDays builds one day per date from start for n days. Each day has one
// switch of device in direction on at the sunrise or sunset of that day plus
// offset minutes, in local time of loc. Each day is built on its own, so the
// minute follows the sun.
func sunHabitDays(t *testing.T, loc *time.Location, start time.Time, n int, lat, lon float64, device string, on, sunrise bool, offset, spread float64) []DayFile {
	t.Helper()
	days := make([]DayFile, 0, n)
	for i := 0; i < n; i++ {
		day := time.Date(start.Year(), start.Month(), start.Day()+i, 0, 0, 0, 0, loc)
		rise, set, ok := sun.Times(day, lat, lon)
		if !ok {
			t.Fatalf("no sun times on %s", day.Format(dayLayout))
		}
		at := set
		if sunrise {
			at = rise
		}
		lt := at.In(loc)
		minute := float64(lt.Hour()*60+lt.Minute()) + float64(lt.Second())/60 + offset
		one := synthDays(day, 1, synthOpts{
			Seed:    int64(21 + i),
			Devices: []string{device},
			Habits:  []synthHabit{{Device: device, On: on, Minute: minute, Spread: spread, Hit: 1}},
		})
		days = append(days, one[0])
	}
	return days
}

// switchTimeAt is the input of the detector with the rule document location.
func switchTimeAt(t *testing.T, days []DayFile, lat, lon float64) *Input {
	t.Helper()
	in := switchTimeInput(t, days)
	in.Doc.Location = &sun.Location{Lat: lat, Lon: lon}
	return in
}

func TestSwitchTimeSunHabitAfterSunsetIsSun(t *testing.T) {
	loc := berlinLoc(t)
	// September: the sunset drifts about 2 minutes a day. Over three months the
	// drift leaves the clock cluster, so no candidate forms, see the report.
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, loc)
	days := sunHabitDays(t, loc, start, 30, 48, 11, "lamp", true, false, 20, 0)
	got := switchTimeFor(SwitchTimeDetector.Run(context.Background(), switchTimeAt(t, days, 48, 11), "lamp"), "lamp", "on")
	if len(got) != 1 {
		t.Fatalf("on candidates = %d, want 1: %+v", len(got), got)
	}
	c := got[0].Finding
	if c.Summary.Code != "switch_time.sun_on" {
		t.Fatalf("summary = %s, want switch_time.sun_on", c.Summary.Code)
	}
	if c.Summary.Params["event"] != "sunset" || c.Summary.Params["offset_min"] != 20 {
		t.Errorf("params = %+v, want event sunset and offset 20", c.Summary.Params)
	}
	if len(c.Apply) != 1 || c.Apply[0].Template != "time_point" {
		t.Fatalf("apply = %+v, want one time_point fix", c.Apply)
	}
	if c.Apply[0].Prefill["anchor"] != "sunset" || c.Apply[0].Prefill["offset_min"] != 20 {
		t.Errorf("prefill = %+v, want anchor sunset and offset 20", c.Apply[0].Prefill)
	}
}

func TestSwitchTimeClockHabitAcrossDSTStaysClock(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 10, 10, 0, 0, 0, 0, loc)
	days := synthDays(start, 40, synthOpts{
		Seed:    31,
		Devices: []string{"lamp"},
		Habits: []synthHabit{
			{Device: "lamp", On: true, Minute: 1140, Spread: 0, Hit: 1},
			{Device: "lamp", On: false, Minute: 1350, Spread: 8, Hit: 1},
		},
	})
	got := switchTimeFor(SwitchTimeDetector.Run(context.Background(), switchTimeAt(t, days, 48, 11), "lamp"), "lamp", "off")
	if len(got) != 1 {
		t.Fatalf("off candidates = %d, want 1", len(got))
	}
	if got[0].Finding.Summary.Code != "switch_time.off" || got[0].Finding.Summary.Params["time"] != "22:30" {
		t.Errorf("summary = %+v, want clock switch_time.off at 22:30", got[0].Finding.Summary)
	}
}

func TestSwitchTimeFourteenDaysAroundSolsticeIsClock(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 6, 14, 0, 0, 0, 0, loc)
	// The sunset barely moves around the solstice. With the noise of a hand
	// switch the period cannot tell the sun from the clock, so the clock holds.
	// Ten days give no candidate at all, the effective days stay below 10.
	days := sunHabitDays(t, loc, start, 14, 48, 11, "lamp", true, false, 20, 8)
	got := switchTimeFor(SwitchTimeDetector.Run(context.Background(), switchTimeAt(t, days, 48, 11), "lamp"), "lamp", "on")
	if len(got) != 1 {
		t.Fatalf("on candidates = %d, want 1", len(got))
	}
	if got[0].Finding.Summary.Code != "switch_time.on" {
		t.Errorf("summary = %s, want clock switch_time.on", got[0].Finding.Summary.Code)
	}
}

func TestSwitchTimeNorthOf70InDecemberIsClockWithoutCrash(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 12, 1, 0, 0, 0, 0, loc)
	days := synthDays(start, 14, synthOpts{
		Seed:    33,
		Devices: []string{"lamp"},
		Habits: []synthHabit{
			{Device: "lamp", On: true, Minute: 1140, Spread: 0, Hit: 1},
			{Device: "lamp", On: false, Minute: 1350, Spread: 8, Hit: 1},
		},
	})
	got := switchTimeFor(SwitchTimeDetector.Run(context.Background(), switchTimeAt(t, days, 70, 19), "lamp"), "lamp", "off")
	if len(got) != 1 {
		t.Fatalf("off candidates = %d, want 1", len(got))
	}
	if got[0].Finding.Summary.Code != "switch_time.off" {
		t.Errorf("summary = %s, want clock switch_time.off", got[0].Finding.Summary.Code)
	}
}

func TestSwitchTimeWithoutLocationIsClockUntilLocationIsSet(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, loc)
	days := sunHabitDays(t, loc, start, 30, 48, 11, "lamp", true, false, 20, 0)

	first := switchTimeFor(SwitchTimeDetector.Run(context.Background(), switchTimeInput(t, days), "lamp"), "lamp", "on")
	if len(first) != 1 {
		t.Fatalf("on candidates without location = %d, want 1", len(first))
	}
	if first[0].Finding.Summary.Code != "switch_time.on" {
		t.Errorf("summary without location = %s, want clock switch_time.on", first[0].Finding.Summary.Code)
	}
	hasFact := false
	for _, f := range first[0].Finding.Facts {
		if f.Code == "no_location" {
			hasFact = true
		}
	}
	if !hasFact {
		t.Errorf("facts = %+v, want no_location", first[0].Finding.Facts)
	}

	second := switchTimeFor(SwitchTimeDetector.Run(context.Background(), switchTimeAt(t, days, 48, 11), "lamp"), "lamp", "on")
	if len(second) != 1 || second[0].Finding.Summary.Code != "switch_time.sun_on" {
		t.Fatalf("with location = %+v, want switch_time.sun_on", second)
	}
	if second[0].Finding.ID != first[0].Finding.ID {
		t.Errorf("ID changed from %s to %s, dismissing would be lost", first[0].Finding.ID, second[0].Finding.ID)
	}
}

// A sun habit five hours before the sunrise lies outside the offsets the
// assistant offers (-240..235), so the reading falls back to the clock.
func TestSwitchTimeSunOffsetOutOfRangeIsClock(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, loc)
	days := sunHabitDays(t, loc, start, 30, 48, 11, "lamp", true, true, -300, 0)
	got := switchTimeFor(SwitchTimeDetector.Run(context.Background(), switchTimeAt(t, days, 48, 11), "lamp"), "lamp", "on")
	if len(got) != 1 {
		t.Fatalf("on candidates = %d, want 1: %+v", len(got), got)
	}
	c := got[0].Finding
	if c.Summary.Code == "switch_time.sun_on" {
		t.Fatalf("summary = %s, want the clock reading for an offset of -300 minutes", c.Summary.Code)
	}
	if len(c.Apply) == 1 && c.Apply[0].Template == "time_point" && c.Apply[0].Prefill["anchor"] != nil {
		t.Errorf("prefill = %+v, want no sun anchor", c.Apply[0].Prefill)
	}
}
