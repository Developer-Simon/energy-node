package autolearn

import (
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

// flapDays writes two days, each with n alternating rule events step seconds
// apart from 08:00. Rule "on" switches on, rule "off" switches off.
func flapDays(t *testing.T, loc *time.Location, device string, n, step int) []DayFile {
	t.Helper()
	var days []DayFile
	for d := 0; d < 2; d++ {
		day := time.Date(2026, 11, 10+d, 0, 0, 0, 0, loc)
		f := relDay(day)
		relReachable(&f, device)
		start := relAt(day, 8, 0, 0)
		for k := 0; k < n; k++ {
			on := k%2 == 0
			rule := "off"
			if on {
				rule = "on"
			}
			relRuleEvent(&f, start+int64(k*step), device, rule, on)
		}
		days = append(days, f)
	}
	return days
}

func TestFlappingFlagsAlternatingRulesBelowMinimumRuntime(t *testing.T) {
	loc := berlinLoc(t)
	days := flapDays(t, loc, "plug.kettle", 10, 60)
	in := relInput(loc, days, map[string]DeviceInfo{"plug.kettle": relDevice("")},
		relRule("on", "Ein"), relRule("off", "Aus"))

	got := relRun(FlappingDetector.Run, in, "plug.kettle")
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1: %+v", len(got), got)
	}
	f := got[0].Finding
	if f.Kind != "flapping" || f.Group != "check" || f.Severity != "warn" {
		t.Fatalf("finding = kind %q group %q severity %q", f.Kind, f.Group, f.Severity)
	}
	if f.ID != FindingID("flapping", "on", "off")+":plug.kettle" {
		t.Errorf("ID = %q", f.ID)
	}
	if len(f.RuleIDs) != 2 || f.RuleIDs[0] != "off" || f.RuleIDs[1] != "on" {
		t.Errorf("RuleIDs = %v", f.RuleIDs)
	}
	if f.Summary.Code != "check.flapping" {
		t.Errorf("summary code = %q", f.Summary.Code)
	}
	if len(f.Apply) != 1 || f.Apply[0].Code != "min_runtime" || f.Apply[0].RuleID != "off" {
		t.Fatalf("apply = %+v, want min_runtime on the off rule", f.Apply)
	}
	op := f.Apply[0].Ops[0]
	if len(op.Path) != 3 || op.Path[0] != "conditions" || op.Path[1] != 0 || op.Path[2] != "hold_seconds" {
		t.Errorf("op path = %v", op.Path)
	}
	// The hold is the minimum run of other devices, 300 s.
	if op.Value != 300 {
		t.Errorf("hold_seconds = %v, want 300", op.Value)
	}
}

func TestFlappingMinimumRuntimeByRole(t *testing.T) {
	loc := berlinLoc(t)
	cases := []struct {
		name    string
		role    energy.Role
		step    int
		flagged bool
	}{
		{"other device, period 60 s", "", 60, true},
		{"other device, period 300 s", "", 300, false},
		{"heat pump, period 300 s", energy.RoleHeatPump, 300, true},
		{"heat pump, period 900 s", energy.RoleHeatPump, 900, false},
		{"wallbox, period 120 s", energy.RoleWallbox, 120, true},
		{"wallbox, period 240 s", energy.RoleWallbox, 240, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			days := flapDays(t, loc, "dev.x", 10, c.step)
			in := relInput(loc, days, map[string]DeviceInfo{"dev.x": relDevice(c.role)},
				relRule("on", "Ein"), relRule("off", "Aus"))
			got := relRun(FlappingDetector.Run, in, "dev.x")
			if (len(got) == 1) != c.flagged {
				t.Fatalf("flagged = %t, want %t: %+v", len(got) == 1, c.flagged, got)
			}
		})
	}
}

func TestFlappingLightGivesNothing(t *testing.T) {
	loc := berlinLoc(t)
	days := flapDays(t, loc, "light.kitchen", 10, 60)
	info := relDevice("")
	info.Light = true
	in := relInput(loc, days, map[string]DeviceInfo{"light.kitchen": info},
		relRule("on", "Ein"), relRule("off", "Aus"))
	if got := relRun(FlappingDetector.Run, in, "light.kitchen"); len(got) != 0 {
		t.Fatalf("light gave %d candidates, want 0", len(got))
	}
}

func TestFlappingNeedsADay(t *testing.T) {
	loc := berlinLoc(t)
	day := time.Date(2026, 11, 10, 0, 0, 0, 0, loc)
	f := relDay(day)
	relReachable(&f, "plug.kettle")
	for k := 0; k < 10; k++ {
		on := k%2 == 0
		rule := "off"
		if on {
			rule = "on"
		}
		relRuleEvent(&f, relAt(day, 8, 0, 0)+int64(k*60), "plug.kettle", rule, on)
	}
	in := relInput(loc, []DayFile{f}, map[string]DeviceInfo{"plug.kettle": relDevice("")},
		relRule("on", "Ein"), relRule("off", "Aus"))
	if got := relRun(FlappingDetector.Run, in, "plug.kettle"); len(got) != 0 {
		t.Fatalf("ten minutes of data gave %d candidates, want 0", len(got))
	}
}

func TestFlappingSparseRulesGiveNothing(t *testing.T) {
	loc := berlinLoc(t)
	var days []DayFile
	for d := 0; d < 3; d++ {
		day := time.Date(2026, 11, 10+d, 0, 0, 0, 0, loc)
		f := relDay(day)
		relReachable(&f, "plug.kettle")
		relRuleEvent(&f, relAt(day, 6, 0, 0), "plug.kettle", "on", true)
		relRuleEvent(&f, relAt(day, 18, 0, 0), "plug.kettle", "off", false)
		days = append(days, f)
	}
	in := relInput(loc, days, map[string]DeviceInfo{"plug.kettle": relDevice("")},
		relRule("on", "Ein"), relRule("off", "Aus"))
	if got := relRun(FlappingDetector.Run, in, "plug.kettle"); len(got) != 0 {
		t.Fatalf("daily switching gave %d candidates, want 0", len(got))
	}
}

func TestFlappingCausedByFeedbackNamesTheCause(t *testing.T) {
	loc := berlinLoc(t)
	days := flapDays(t, loc, "plug.kettle", 10, 60)
	devices := map[string]DeviceInfo{"plug.kettle": {ID: "plug.kettle", Name: "Kessel", Switchable: true,
		Entities: []string{"switch.plug.kettle"}}}
	causeOf := func(in *Input) (string, bool) {
		got := relRun(FlappingDetector.Run, in, "plug.kettle")
		if len(got) != 1 {
			t.Fatalf("candidates = %d, want 1", len(got))
		}
		for _, f := range got[0].Finding.Facts {
			if f.Code == "cause" {
				return f.Value.(string), true
			}
		}
		return "", false
	}

	// Without a learned operating power the switch-ons cannot be tied to the load.
	plain := relInput(loc, days, devices, fbRule("on", "above", 1500), fbRule("off", "below", 500))
	if cause, ok := causeOf(plain); ok {
		t.Errorf("cause without feedback = %q, want none", cause)
	}

	// The kettle moves the export by 2000 W, so the thresholds 1500 and 500 feed back.
	fed := relInput(loc, days, devices, fbRule("on", "above", 1500), fbRule("off", "below", 500))
	fed.Learned = fbLearned(2000)
	if cause, ok := causeOf(fed); !ok || cause != "feedback" {
		t.Errorf("cause = %q (found %v), want feedback", cause, ok)
	}
}
