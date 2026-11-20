package autolearn

import (
	"testing"
	"time"
)

// noEffectDays writes n days with the actions of rule "on" on plug.lamp. Each
// day has a failing action at every minute of the day in fails, and as many
// working actions at 12:00. The quarters [reach0[0], reach0[1]) are unreachable.
func noEffectDays(t *testing.T, loc *time.Location, n int, fails []int, reach0 [2]int) []DayFile {
	t.Helper()
	var days []DayFile
	for d := 0; d < n; d++ {
		day := time.Date(2026, 11, 10+d, 0, 0, 0, 0, loc)
		f := relDay(day)
		dd := relReachable(&f, "plug.lamp")
		for q := reach0[0]; q < reach0[1]; q++ {
			dd.Reach[q] = 0
		}
		for _, m := range fails {
			relRuleAction(&f, relAt(day, m/60, m%60, 0), "plug.lamp", "on", false, false)
		}
		for k := 0; k < len(fails); k++ {
			relRuleAction(&f, relAt(day, 12, k, 0), "plug.lamp", "on", false, true)
		}
		days = append(days, f)
	}
	return days
}

// minutes of the day
func hm(h, m int) int { return h*60 + m }

func TestNoEffectScatteredFailuresGetRepeat(t *testing.T) {
	loc := berlinLoc(t)
	fails := []int{hm(3, 10), hm(9, 40), hm(14, 5), hm(18, 25), hm(22, 50)}
	days := noEffectDays(t, loc, 3, fails, [2]int{0, 0})
	in := relInput(loc, days, map[string]DeviceInfo{"plug.lamp": relDevice("")}, relRule("on", "Licht"))

	got := relRun(NoEffectDetector.Run, in, "plug.lamp")
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1: %+v", len(got), got)
	}
	f := got[0].Finding
	if f.Kind != "no_effect" || f.Group != "check" || f.Severity != "warn" {
		t.Fatalf("finding = kind %q group %q severity %q", f.Kind, f.Group, f.Severity)
	}
	if f.Summary.Code != "check.no_effect" || f.Summary.Params["feature"] != "scattered" {
		t.Errorf("summary = %+v", f.Summary)
	}
	if len(f.Apply) != 1 || f.Apply[0].Code != "repeat" || f.Apply[0].RuleID != "on" {
		t.Fatalf("apply = %+v, want repeat on rule on", f.Apply)
	}
	op := f.Apply[0].Ops[0]
	if len(op.Path) != 1 || op.Path[0] != "repeat_seconds" || op.Value != 60 {
		t.Errorf("op = %+v, want repeat_seconds 60", op)
	}
}

func TestNoEffectClusteredFailuresGetOpenRule(t *testing.T) {
	loc := berlinLoc(t)
	fails := []int{hm(7, 0), hm(7, 3), hm(7, 5), hm(7, 8), hm(7, 10)}
	days := noEffectDays(t, loc, 3, fails, [2]int{0, 0})
	in := relInput(loc, days, map[string]DeviceInfo{"plug.lamp": relDevice("")}, relRule("on", "Licht"))

	got := relRun(NoEffectDetector.Run, in, "plug.lamp")
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1: %+v", len(got), got)
	}
	f := got[0].Finding
	if f.Summary.Params["feature"] != "clustered" {
		t.Errorf("feature = %v, want clustered", f.Summary.Params["feature"])
	}
	if len(f.Apply) != 1 || f.Apply[0].Code != "open_rule" || f.Apply[0].RuleID != "on" {
		t.Fatalf("apply = %+v, want open_rule", f.Apply)
	}
}

func TestNoEffectFailuresInOfflineQuarterPointToOfflineWindow(t *testing.T) {
	loc := berlinLoc(t)
	// The device is unreachable from 07:00 to 08:00, the failures fall there.
	fails := []int{hm(7, 0), hm(7, 15), hm(7, 30), hm(7, 45)}
	days := noEffectDays(t, loc, 3, fails, [2]int{28, 32})
	in := relInput(loc, days, map[string]DeviceInfo{"plug.lamp": relDevice("")}, relRule("on", "Licht"))

	got := relRun(NoEffectDetector.Run, in, "plug.lamp")
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1: %+v", len(got), got)
	}
	f := got[0].Finding
	if f.Summary.Params["feature"] != "offline_window" {
		t.Errorf("feature = %v, want offline_window", f.Summary.Params["feature"])
	}
	for _, fix := range f.Apply {
		if fix.Code == "repeat" {
			t.Fatalf("offline failures got a repeat fix: %+v", f.Apply)
		}
	}
}

func TestNoEffectNoiseGivesNothing(t *testing.T) {
	loc := berlinLoc(t)
	// Three failures among many working actions: the lower Wilson bound stays below 0.2.
	days := noEffectDays(t, loc, 1, []int{hm(3, 10), hm(9, 40), hm(14, 5)}, [2]int{0, 0})
	for k := 0; k < 97; k++ {
		relRuleAction(&days[0], relAt(time.Date(2026, 11, 10, 0, 0, 0, 0, loc), 15, 0, 0)+int64(k), "plug.lamp", "on", false, true)
	}
	in := relInput(loc, days, map[string]DeviceInfo{"plug.lamp": relDevice("")}, relRule("on", "Licht"))
	if got := relRun(NoEffectDetector.Run, in, "plug.lamp"); len(got) != 0 {
		t.Fatalf("noise gave %d candidates, want 0", len(got))
	}
}

func TestNoEffectActionsAlreadyThereAreNotCounted(t *testing.T) {
	loc := berlinLoc(t)
	fails := []int{hm(3, 10), hm(9, 40), hm(14, 5), hm(18, 25), hm(22, 50)}
	days := noEffectDays(t, loc, 3, fails, [2]int{0, 0})
	for d := range days {
		for k := range days[d].Actions {
			days[d].Actions[k].There = true
		}
	}
	in := relInput(loc, days, map[string]DeviceInfo{"plug.lamp": relDevice("")}, relRule("on", "Licht"))
	if got := relRun(NoEffectDetector.Run, in, "plug.lamp"); len(got) != 0 {
		t.Fatalf("already-there actions gave %d candidates, want 0", len(got))
	}
}
