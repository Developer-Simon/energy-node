package autolearn

import (
	"math"
	"testing"
	"time"
)

// runSpec is one run of a device in the test days. It starts at minute Start of
// day index Day and lasts Minutes. EndOrigin is the origin of the off event, an
// empty value means a hand switch.
type runSpec struct {
	Day       int
	Start     float64
	Minutes   float64
	EndOrigin Origin
}

// runDays builds n day files from start with the runs of one device at watts.
// Every quarter counts as seen, each event goes into the day of its local time,
// and Wh counts the minutes of each run into the quarters they fall in.
func runDays(t *testing.T, start time.Time, n int, device string, watts float64, runs []runSpec) []DayFile {
	t.Helper()
	loc := start.Location()
	days := make([]DayFile, n)
	index := map[string]int{}
	for i := range days {
		day := time.Date(start.Year(), start.Month(), start.Day()+i, 0, 0, 0, 0, loc)
		f := DayFile{V: dayFileVersion, Day: day.Format(dayLayout),
			Devices: map[string]*DeviceDay{device: {}}, Events: []EventRec{}, Actions: []ActionRec{}}
		for q := 0; q < dayQuarters; q++ {
			f.Site.Seen[q] = dayCounterMax
			f.Site.LoadTotal[q] = 300
		}
		days[i] = f
		index[f.Day] = i
	}
	addEvent := func(at time.Time, on bool, origin Origin) {
		i, ok := index[at.In(loc).Format(dayLayout)]
		if !ok {
			t.Fatalf("event at %v outside the test days", at)
		}
		days[i].Events = append(days[i].Events, EventRec{At: at.Unix(), Device: device,
			Entity: "switch." + device, On: on, Origin: string(origin)})
	}
	for _, r := range runs {
		on := time.Date(start.Year(), start.Month(), start.Day()+r.Day, 0, 0, 0, 0, loc).
			Add(time.Duration(r.Start * float64(time.Minute)))
		off := on.Add(time.Duration(r.Minutes * float64(time.Minute)))
		origin := r.EndOrigin
		if origin == "" {
			origin = OriginHand
		}
		addEvent(on, true, OriginHand)
		addEvent(off, false, origin)
		for m := 0; m < int(r.Minutes); m++ {
			at := on.Add(time.Duration(m) * time.Minute).In(loc)
			i, ok := index[at.Format(dayLayout)]
			if !ok {
				continue
			}
			dd := days[i].Devices[device]
			dd.Wh[at.Hour()*4+at.Minute()/15] += float32(watts / 60)
		}
	}
	return days
}

func TestRunsOfCrossingMidnightIsOneRun(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := runDays(t, start, 2, "heater", 1000, []runSpec{{Day: 0, Start: 23*60 + 45, Minutes: 30}})
	runs := runsIn(days, loc)
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1: %+v", len(runs), runs)
	}
	r := runs[0]
	wantStart := time.Date(2026, 1, 5, 23, 45, 0, 0, loc)
	wantEnd := time.Date(2026, 1, 6, 0, 15, 0, 0, loc)
	if !r.Start.Equal(wantStart) || !r.End.Equal(wantEnd) {
		t.Errorf("run = %v to %v, want %v to %v", r.Start, r.End, wantStart, wantEnd)
	}
	if r.Device != "heater" || r.Entity != "switch.heater" || r.EndOrigin != OriginHand {
		t.Errorf("run identity = %+v", r)
	}
	// 1000 W for half an hour is 500 Wh, the quarters 23:45 and 00:00 hold it.
	if math.Abs(r.Wh-500) > 0.5 {
		t.Errorf("wh = %v, want 500", r.Wh)
	}
}

func TestRunsOfSplitsEnergyByQuarterShare(t *testing.T) {
	loc := berlinLoc(t)
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	f := DayFile{V: dayFileVersion, Day: day.Format(dayLayout), Devices: map[string]*DeviceDay{"x": {}},
		Events: []EventRec{
			{At: day.Add(10*time.Hour + 7*time.Minute).Unix(), Device: "x", Entity: "switch.x", On: true, Origin: "hand"},
			{At: day.Add(10*time.Hour + 22*time.Minute).Unix(), Device: "x", Entity: "switch.x", On: false, Origin: "rule"},
			// Open at the end of the days: dropped.
			{At: day.Add(18 * time.Hour).Unix(), Device: "x", Entity: "switch.x", On: true, Origin: "hand"},
		}}
	f.Devices["x"].Wh[40] = 100 // 10:00 to 10:15
	f.Devices["x"].Wh[41] = 200 // 10:15 to 10:30
	runs := runsIn([]DayFile{f}, loc)
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1 (open run dropped): %+v", len(runs), runs)
	}
	want := 100*8.0/15 + 200*7.0/15
	if math.Abs(runs[0].Wh-want) > 0.01 {
		t.Errorf("wh = %v, want %v", runs[0].Wh, want)
	}
	if runs[0].EndOrigin != OriginRule {
		t.Errorf("end origin = %q, want rule", runs[0].EndOrigin)
	}
}

func TestRunsOfIgnoresOffWithoutOn(t *testing.T) {
	loc := berlinLoc(t)
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	f := DayFile{V: dayFileVersion, Day: day.Format(dayLayout), Devices: map[string]*DeviceDay{},
		Events: []EventRec{{At: day.Add(time.Hour).Unix(), Device: "x", Entity: "switch.x", On: false}}}
	if runs := runsIn([]DayFile{f}, loc); len(runs) != 0 {
		t.Fatalf("runs = %+v, want none", runs)
	}
}

func TestDominantEntityPicksMostFrequentThenSmallest(t *testing.T) {
	if got := dominantEntity([]string{"b", "a", "b"}); got != "b" {
		t.Errorf("most frequent = %q, want b", got)
	}
	if got := dominantEntity([]string{"b", "a"}); got != "a" {
		t.Errorf("tie = %q, want the smallest a", got)
	}
	if got := dominantEntity(nil); got != "" {
		t.Errorf("empty = %q, want empty", got)
	}
}

func TestSwitchEntityOfTakesTheMostUsedEntity(t *testing.T) {
	in := &Input{Days: []DayFile{
		{Events: []EventRec{{Device: "pump", Entity: "pump_switch"}, {Device: "pump", Entity: "pump_switch"}, {Device: "lamp", Entity: "lamp_switch"}}},
		{Events: []EventRec{{Device: "pump", Entity: "pump_other"}}},
	}}
	if got := switchEntityOf(in, "pump"); got != "pump_switch" {
		t.Fatalf("switchEntityOf(pump) = %q, want pump_switch", got)
	}
	if got := switchEntityOf(in, "fridge"); got != "" {
		t.Fatalf("switchEntityOf(fridge) = %q, want empty", got)
	}
}
