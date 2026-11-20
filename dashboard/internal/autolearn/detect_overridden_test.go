package autolearn

import (
	"context"
	"testing"
	"time"
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
