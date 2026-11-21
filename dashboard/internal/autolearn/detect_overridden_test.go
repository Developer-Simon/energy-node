package autolearn

import (
	"context"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// ovDays writes one day per entry of reversed. The rule "heat" switches the
// device on at 18:00 with the given house load. When the day is reversed, the
// user switches it off again three minutes later.
func ovDays(t *testing.T, loc *time.Location, start time.Time, reversed []bool, house func(i int) float64) []DayFile {
	t.Helper()
	var days []DayFile
	for i, rev := range reversed {
		day := start.AddDate(0, 0, i)
		f := relDay(day)
		at := relAt(day, 18, 0, 0)
		f.Events = append(f.Events, EventRec{At: at, Device: "plug.heater", Entity: "switch.plug.heater",
			On: true, Origin: string(OriginRule), Rule: "heat", House: house(i)})
		if rev {
			f.Events = append(f.Events, EventRec{At: at + 180, Device: "plug.heater", Entity: "switch.plug.heater",
				On: false, Origin: string(OriginHand)})
		}
		days = append(days, f)
	}
	return days
}

// ovBase gives the weekdays group 0 and the weekend group 1 for the days from start.
func ovBase(loc *time.Location, start time.Time, n int) Base {
	base := Base{DayType: map[string]int{}, Groups: [][]time.Weekday{
		{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
		{time.Saturday, time.Sunday},
	}}
	for i := 0; i < n; i++ {
		day := start.AddDate(0, 0, i)
		g := 0
		if wd := day.Weekday(); wd == time.Saturday || wd == time.Sunday {
			g = 1
		}
		base.DayType[day.Format(dayLayout)] = g
	}
	return base
}

// ovRun runs the overridden detector on the heater with the given days, base and rules.
func ovRun(loc *time.Location, days []DayFile, base Base, rules ...Rule) []Candidate {
	in := relInput(loc, days, map[string]DeviceInfo{"plug.heater": relDevice("")}, rules...)
	in.Base = base
	return relRun(OverriddenDetector.Run, in, "plug.heater")
}

func TestOverriddenOnWeekdaysGivesAddConditionWithWeekends(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 11, 2, 0, 0, 0, 0, loc) // Montag
	n := 35
	reversed := make([]bool, n)
	for i := range reversed {
		wd := start.AddDate(0, 0, i).Weekday()
		reversed[i] = wd != time.Saturday && wd != time.Sunday
	}
	days := ovDays(t, loc, start, reversed, func(int) float64 { return 300 })
	got := ovRun(loc, days, ovBase(loc, start, n), relRule("heat", "Heizung"))
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1: %+v", len(got), got)
	}
	f := got[0].Finding
	if f.Kind != "overridden" || f.Group != "check" || f.Severity != "info" {
		t.Fatalf("finding = kind %q group %q severity %q", f.Kind, f.Group, f.Severity)
	}
	if f.ID != FindingID("overridden", "heat")+":plug.heater" {
		t.Errorf("ID = %q", f.ID)
	}
	if f.Evidence.Shape != "day" {
		t.Errorf("evidence shape = %q, want day", f.Evidence.Shape)
	}
	if len(f.Apply) != 1 || f.Apply[0].Code != "add_condition" || f.Apply[0].RuleID != "heat" {
		t.Fatalf("apply = %+v, want add_condition on heat", f.Apply)
	}
	op := f.Apply[0].Ops[0]
	if len(op.Path) != 2 || op.Path[0] != "conditions" || op.Path[1] != 1 {
		t.Errorf("op path = %v, want conditions, 1", op.Path)
	}
	cond, ok := op.Value.(map[string]any)
	if !ok || cond["type"] != "time_window" {
		t.Fatalf("op value = %+v, want a time_window condition", op.Value)
	}
	weekdays, ok := cond["weekdays"].([]int)
	if !ok || len(weekdays) != 2 || weekdays[0] != 5 || weekdays[1] != 6 {
		t.Errorf("weekdays = %v, want the weekend [5 6] (0 is Monday)", cond["weekdays"])
	}
}

func TestOverriddenHouseLoadGivesBalanceCondition(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 11, 2, 0, 0, 0, 0, loc)
	n := 20
	reversed := make([]bool, n)
	for i := 0; i < 14; i++ {
		reversed[i] = true
	}
	days := ovDays(t, loc, start, reversed, func(i int) float64 {
		if reversed[i] {
			return 900
		}
		return 200
	})
	got := ovRun(loc, days, Base{}, relRule("heat", "Heizung"))
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1: %+v", len(got), got)
	}
	f := got[0].Finding
	if len(f.Apply) != 1 || f.Apply[0].Code != "add_condition" {
		t.Fatalf("apply = %+v, want add_condition", f.Apply)
	}
	cond, ok := f.Apply[0].Ops[0].Value.(map[string]any)
	if !ok {
		t.Fatalf("op value = %+v", f.Apply[0].Ops[0].Value)
	}
	if cond["type"] != "balance_threshold" || cond["field"] != "load_total" || cond["comparison"] != "below" {
		t.Errorf("condition = %+v, want balance_threshold load_total below", cond)
	}
	if cond["threshold"] != 550.0 {
		t.Errorf("threshold = %v, want 550 between 200 and 900 W", cond["threshold"])
	}
}

func TestOverriddenClearCaseWithoutSplitOrWindowHasNoFix(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 11, 2, 0, 0, 0, 0, loc)
	reversed := make([]bool, 14)
	for i := range reversed {
		reversed[i] = true
	}
	days := ovDays(t, loc, start, reversed, func(int) float64 { return 300 })
	got := ovRun(loc, days, Base{}, relRule("heat", "Heizung"))
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1: %+v", len(got), got)
	}
	if len(got[0].Finding.Apply) != 0 {
		t.Errorf("apply = %+v, want none without a split or a time window", got[0].Finding.Apply)
	}
}

func TestOverriddenSimilarSwitchBackShiftsTime(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 11, 2, 0, 0, 0, 0, loc)
	reversed := make([]bool, 14)
	for i := range reversed {
		reversed[i] = true
	}
	days := ovDays(t, loc, start, reversed, func(int) float64 { return 300 })
	rule := relRule("heat", "Heizung")
	rule.Conditions = append(rule.Conditions, Condition{Type: "time_window"})
	got := ovRun(loc, days, Base{}, rule)
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1", len(got))
	}
	apply := got[0].Finding.Apply
	if len(apply) != 1 || apply[0].Code != "shift_time" {
		t.Fatalf("apply = %+v, want shift_time", apply)
	}
	op := apply[0].Ops[0]
	if len(op.Path) != 3 || op.Path[1] != 1 || op.Path[2] != "start" {
		t.Errorf("op path = %v, want conditions, 1, start", op.Path)
	}
	// The user switches back at 18:03, rounded to 5 minutes.
	if op.Value != "18:05" {
		t.Errorf("shifted start = %v, want 18:05", op.Value)
	}
}

func TestOverriddenNoiseGivesNothing(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 11, 2, 0, 0, 0, 0, loc)
	reversed := make([]bool, 20)
	reversed[3], reversed[11] = true, true
	days := ovDays(t, loc, start, reversed, func(int) float64 { return 300 })
	if got := ovRun(loc, days, ovBase(loc, start, 20), relRule("heat", "Heizung")); len(got) != 0 {
		t.Fatalf("noise gave %d candidates, want 0: %+v", len(got), got)
	}
}

func TestOverriddenCancelledRunGivesNothing(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 11, 2, 0, 0, 0, 0, loc)
	reversed := make([]bool, 14)
	for i := range reversed {
		reversed[i] = true
	}
	days := ovDays(t, loc, start, reversed, func(int) float64 { return 300 })
	in := relInput(loc, days, map[string]DeviceInfo{"plug.heater": relDevice("")}, relRule("heat", "Heizung"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := OverriddenDetector.Run(ctx, in, "plug.heater"); len(got) != 0 {
		t.Fatalf("cancelled run gave %d candidates", len(got))
	}
}

func TestOverriddenSplitTakesBestWellSeparatedFeature(t *testing.T) {
	// Reversed at house load index 0 and 6 of 20 cases. The cut after index 6
	// has the largest gain but classifies only 75 % right. The cut after index 3
	// classifies 80 % right with a smaller gain, and must be chosen.
	reversedAt := map[int]bool{0: true, 6: true}
	var cases []overrideCase
	for i := 0; i < 20; i++ {
		cases = append(cases, overrideCase{house: float64(i), hour: 18, reversed: reversedAt[i]})
	}
	split, ok := overriddenSplit(cases, Base{})
	if !ok {
		t.Fatal("no split found")
	}
	if split.feature != overriddenHouseLoad {
		t.Errorf("feature = %q, want house_load", split.feature)
	}
	if split.cut != 3.5 {
		t.Errorf("cut = %v, want 3.5 (the well separated cut, not 6.5)", split.cut)
	}
	if split.acc < overriddenMinSeparation {
		t.Errorf("acc = %v, want at least %v", split.acc, overriddenMinSeparation)
	}
}

func TestOverriddenSplitWithoutBuildableFixIsNotReported(t *testing.T) {
	// The day type split is the only one, but its accepted side covers all seven
	// days, so no add_condition can be built. The split must not be reported.
	loc := berlinLoc(t)
	start := time.Date(2026, 11, 2, 0, 0, 0, 0, loc)
	n := 35
	reversed := make([]bool, n)
	for i := range reversed {
		wd := start.AddDate(0, 0, i).Weekday()
		reversed[i] = wd != time.Saturday && wd != time.Sunday
	}
	days := ovDays(t, loc, start, reversed, func(int) float64 { return 300 })
	base := ovBase(loc, start, n)
	base.Groups = [][]time.Weekday{
		{},
		{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday},
	}
	got := ovRun(loc, days, base, relRule("heat", "Heizung"))
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1: %+v", len(got), got)
	}
	f := got[0].Finding
	if len(f.Apply) != 0 {
		t.Errorf("apply = %+v, want none", f.Apply)
	}
	if f.Summary.Params["feature"] != "none" {
		t.Errorf("feature = %v, want none for an unbuildable split", f.Summary.Params["feature"])
	}
}

// ovSunDays writes one day per entry of reversed on the days from start. The
// rule switches the device on ten minutes before the sunset of the day when
// reversed is set, and ten minutes after it otherwise. Reversed switches are
// switched off again three minutes later.
func ovSunDays(t *testing.T, start time.Time, reversed []bool, site *sun.Location) []DayFile {
	t.Helper()
	var days []DayFile
	for i, rev := range reversed {
		day := start.AddDate(0, 0, i)
		_, set, ok := sun.Times(day, site.Lat, site.Lon)
		if !ok {
			t.Fatalf("no sunset on %s", day.Format(dayLayout))
		}
		f := relDay(day)
		at := set.Add(10 * time.Minute).Unix()
		if rev {
			at = set.Add(-10 * time.Minute).Unix()
		}
		f.Events = append(f.Events, EventRec{At: at, Device: "plug.heater", Entity: "switch.plug.heater",
			On: true, Origin: string(OriginRule), Rule: "heat"})
		if rev {
			f.Events = append(f.Events, EventRec{At: at + 180, Device: "plug.heater", Entity: "switch.plug.heater",
				On: false, Origin: string(OriginHand)})
		}
		days = append(days, f)
	}
	return days
}

func TestOverriddenOnlyInBrightSkyGivesSunWindow(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 6, 14, 0, 0, 0, 0, loc)
	site := &sun.Location{Lat: 48, Lon: 13.4}
	n := 24
	reversed := make([]bool, n)
	for i := range reversed {
		reversed[i] = i%4 != 3
	}
	in := relInput(loc, ovSunDays(t, start, reversed, site), map[string]DeviceInfo{"plug.heater": relDevice("")}, relRule("heat", "Heizung"))
	in.Doc.Location = site
	got := relRun(OverriddenDetector.Run, in, "plug.heater")
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1: %+v", len(got), got)
	}
	f := got[0].Finding
	if f.Summary.Params["feature"] != overriddenToSunset {
		t.Fatalf("feature = %v, want %s", f.Summary.Params["feature"], overriddenToSunset)
	}
	if len(f.Apply) != 1 || f.Apply[0].Code != "add_condition" {
		t.Fatalf("apply = %+v, want add_condition", f.Apply)
	}
	cond, ok := f.Apply[0].Ops[0].Value.(map[string]any)
	if !ok {
		t.Fatalf("op value = %+v", f.Apply[0].Ops[0].Value)
	}
	// The dark side is accepted, so the window starts at the sunset and runs to
	// the sunrise. The cut lies between -10 and +10 minutes, so no offset.
	if cond["type"] != "sun_window" || cond["from"] != "sunset" || cond["to"] != "sunrise" {
		t.Errorf("condition = %+v, want sun_window from sunset to sunrise", cond)
	}
	if cond["from_offset_min"] != 0 || cond["to_offset_min"] != 0 {
		t.Errorf("offsets = %v / %v, want 0 / 0", cond["from_offset_min"], cond["to_offset_min"])
	}
}

func TestOverriddenElevationFixStartsWhereSunStandsAtCut(t *testing.T) {
	loc := berlinLoc(t)
	site := &sun.Location{Lat: 48, Lon: 13.4}
	day := time.Date(2026, 6, 14, 0, 0, 0, 0, loc)
	_, set, ok := sun.Times(day, site.Lat, site.Lon)
	if !ok {
		t.Fatal("no sunset")
	}
	var cases []overrideCase
	for _, m := range []time.Duration{-30, -20, -10, 0, 10, 20, 30} {
		to, sunset, elev, ok := overrideSunState(set.Add(m*time.Minute), site)
		if !ok {
			t.Fatalf("no sun state at %v", m)
		}
		cases = append(cases, overrideCase{sunOK: true, toSunset: to, sunset: sunset, elevation: elev})
	}
	split := overrideSplit{feature: overriddenElevation, cut: 5, acceptedLeft: true}
	fix, _, ok := overriddenAddFix(Rule{ID: "heat"}, Base{}, split, cases, site)
	if !ok || fix.Code != "add_condition" {
		t.Fatalf("fix = %+v ok %v, want add_condition", fix, ok)
	}
	cond, ok := fix.Ops[0].Value.(map[string]any)
	if !ok {
		t.Fatalf("op value = %+v", fix.Ops[0].Value)
	}
	off, ok := cond["from_offset_min"].(int)
	if cond["type"] != "sun_window" || cond["from"] != "sunset" || !ok {
		t.Fatalf("condition = %+v, want sun_window from sunset", cond)
	}
	// Five degrees of elevation lie about 20 minutes before the sunset in June
	// at 48 degrees north. The window starts that long before the sunset.
	if off%5 != 0 || off > -10 || off < -40 {
		t.Errorf("from_offset_min = %d, want about -20", off)
	}
}

// A sun cut outside the schema range of the sun offsets (-240..240 minutes)
// gives no fix, the range edge itself is kept. The offset is rounded to five
// minutes first.
func TestOverriddenSunFixDropsOffsetOutOfRange(t *testing.T) {
	site := &sun.Location{Lat: 48, Lon: 13.4}
	rule := Rule{ID: "heat"}
	for _, tc := range []struct {
		name string
		cut  float64
		ok   bool
	}{
		{"600 minutes before sunset", 600, false},
		{"243 minutes before sunset rounds to -245", 243, false},
		{"240 minutes before sunset is kept", 240, true},
	} {
		split := overrideSplit{feature: overriddenToSunset, cut: tc.cut, acceptedLeft: true}
		fix, _, ok := overriddenAddFix(rule, Base{}, split, nil, site)
		if ok != tc.ok {
			t.Errorf("%s: ok = %v, want %v (fix %+v)", tc.name, ok, tc.ok, fix)
		}
		if !ok {
			continue
		}
		cond, _ := fix.Ops[0].Value.(map[string]any)
		if off, _ := cond["from_offset_min"].(int); off != -240 {
			t.Errorf("%s: from_offset_min = %d, want -240", tc.name, off)
		}
	}
}
