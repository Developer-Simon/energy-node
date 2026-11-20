package autolearn

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// offlineDays writes n days from 2026-11-02 for one device. unreachable(d, q)
// says whether the device was unreachable in quarter q of day d. actionMinute
// is the minute of a daily action of rule "heat" on the device, -1 for none.
func offlineDays(t *testing.T, loc *time.Location, device string, n int, unreachable func(d, q int) bool, actionMinute int) []DayFile {
	t.Helper()
	var days []DayFile
	for d := 0; d < n; d++ {
		day := time.Date(2026, 11, 2+d, 0, 0, 0, 0, loc)
		f := relDay(day)
		dd := relReachable(&f, device)
		for q := 0; q < dayQuarters; q++ {
			if unreachable != nil && unreachable(d, q) {
				dd.Reach[q] = 0
			}
		}
		if actionMinute >= 0 {
			relRuleAction(&f, relAt(day, actionMinute/60, actionMinute%60, 0), device, "heat", false, false)
		}
		days = append(days, f)
	}
	return days
}

// offlineRule is an enabled rule whose time window starts at start.
func offlineRule(id, start string) Rule {
	raw := json.RawMessage(fmt.Sprintf(`{"type":"time_window","start":%q,"end":"04:00"}`, start))
	return Rule{ID: id, Name: "Heizstab", Enabled: true,
		Conditions: []Condition{{Type: "time_window", Raw: raw}}}
}

// offlineNightOutage is unreachable from 03:00 to 03:45 every day.
func offlineNightOutage(d, q int) bool {
	return q >= 12 && q <= 14
}

func TestOfflineFlagsRuleStartingInOfflineWindow(t *testing.T) {
	loc := berlinLoc(t)
	days := offlineDays(t, loc, "plug.heater", 20, offlineNightOutage, 195)
	in := relInput(loc, days, map[string]DeviceInfo{"plug.heater": relDevice("")}, offlineRule("heat", "03:15"))

	got := relRun(OfflineDetector.Run, in, "plug.heater")
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1: %+v", len(got), got)
	}
	f := got[0].Finding
	if f.Kind != "offline_window" || f.Group != "check" || f.Severity != "warn" {
		t.Fatalf("finding = kind %q group %q severity %q", f.Kind, f.Group, f.Severity)
	}
	if f.Summary.Code != "check.offline_window" {
		t.Errorf("summary code = %q", f.Summary.Code)
	}
	if f.Summary.Params["window_from"] != 180 || f.Summary.Params["window_to"] != 225 {
		t.Errorf("window params = %v", f.Summary.Params)
	}
	if len(f.RuleIDs) != 1 || f.RuleIDs[0] != "heat" {
		t.Errorf("RuleIDs = %v", f.RuleIDs)
	}
	if len(f.Apply) != 1 || f.Apply[0].Code != "move_before" || f.Apply[0].RuleID != "heat" {
		t.Fatalf("apply = %+v, want move_before on heat", f.Apply)
	}
	op := f.Apply[0].Ops[0]
	if len(op.Path) != 3 || op.Path[0] != "conditions" || op.Path[1] != 0 || op.Path[2] != "start" {
		t.Errorf("op path = %v", op.Path)
	}
	if op.Value != "02:55" {
		t.Errorf("op value = %v, want 02:55", op.Value)
	}
	if f.Evidence.Shape != "day" || len(f.Evidence.IstSpans) != 1 || f.Evidence.IstSpans[0] != [2]int64{180, 225} {
		t.Errorf("evidence = %+v, want day shape with span 180-225", f.Evidence)
	}
	if len(f.Evidence.Ist) != 1 || f.Evidence.Ist[0] != 195 || len(f.Evidence.Soll) != 1 || f.Evidence.Soll[0] != 175 {
		t.Errorf("evidence ist/soll = %v / %v, want 195 / 175", f.Evidence.Ist, f.Evidence.Soll)
	}
}

func TestOfflineReachableDeviceGivesNothing(t *testing.T) {
	loc := berlinLoc(t)
	days := offlineDays(t, loc, "plug.heater", 20, nil, 195)
	in := relInput(loc, days, map[string]DeviceInfo{"plug.heater": relDevice("")}, offlineRule("heat", "03:15"))

	if got := relRun(OfflineDetector.Run, in, "plug.heater"); len(got) != 0 {
		t.Fatalf("candidates = %d, want 0", len(got))
	}
}

func TestOfflineNeedsFourteenDays(t *testing.T) {
	loc := berlinLoc(t)
	days := offlineDays(t, loc, "plug.heater", 13, offlineNightOutage, 195)
	in := relInput(loc, days, map[string]DeviceInfo{"plug.heater": relDevice("")}, offlineRule("heat", "03:15"))

	if got := relRun(OfflineDetector.Run, in, "plug.heater"); len(got) != 0 {
		t.Fatalf("candidates = %d, want 0 with 13 days", len(got))
	}
}

func TestOfflineNoiseGivesNothing(t *testing.T) {
	loc := berlinLoc(t)
	// About one day in ten is unreachable in each quarter, at scattered days.
	noise := func(d, q int) bool { return (d*7+q*13)%10 == 0 }
	days := offlineDays(t, loc, "plug.heater", 20, noise, 195)
	in := relInput(loc, days, map[string]DeviceInfo{"plug.heater": relDevice("")}, offlineRule("heat", "03:15"))

	if got := relRun(OfflineDetector.Run, in, "plug.heater"); len(got) != 0 {
		t.Fatalf("candidates = %d, want 0 for noise", len(got))
	}
}

func TestOfflineRuleOutsideWindowGivesNothing(t *testing.T) {
	loc := berlinLoc(t)
	days := offlineDays(t, loc, "plug.heater", 20, offlineNightOutage, 600)
	in := relInput(loc, days, map[string]DeviceInfo{"plug.heater": relDevice("")}, offlineRule("heat", "10:00"))

	if got := relRun(OfflineDetector.Run, in, "plug.heater"); len(got) != 0 {
		t.Fatalf("candidates = %d, want 0 for a rule at 10:00", len(got))
	}
}

// offlineMidnightOutage is unreachable from 23:30 to 00:30 every day.
func offlineMidnightOutage(d, q int) bool {
	return q == 94 || q == 95 || q == 0 || q == 1
}

func TestOfflineJoinsWindowAcrossMidnight(t *testing.T) {
	loc := berlinLoc(t)
	days := offlineDays(t, loc, "plug.heater", 20, offlineMidnightOutage, 10)
	in := relInput(loc, days, map[string]DeviceInfo{"plug.heater": relDevice("")}, offlineRule("heat", "00:10"))

	got := relRun(OfflineDetector.Run, in, "plug.heater")
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1: %+v", len(got), got)
	}
	f := got[0].Finding
	if f.Summary.Params["window_from"] != 1410 || f.Summary.Params["window_to"] != 30 {
		t.Errorf("window params = %v, want 1410 to 30", f.Summary.Params)
	}
	if len(f.Apply) != 1 || f.Apply[0].Code != "move_before" {
		t.Fatalf("apply = %+v, want move_before", f.Apply)
	}
	if v := f.Apply[0].Ops[0].Value; v != "23:25" {
		t.Fatalf("op value = %v, want 23:25", v)
	}
	// 23:25 must lie outside the window 23:30 to 00:30.
	if proposed := 23*60 + 25; proposed >= 1410 || proposed < 30 {
		t.Errorf("proposed %d lies in the window", proposed)
	}
	if len(f.Evidence.IstSpans) != 2 || f.Evidence.IstSpans[0] != [2]int64{1410, 1440} || f.Evidence.IstSpans[1] != [2]int64{0, 30} {
		t.Errorf("IstSpans = %v, want [1410 1440] and [0 30]", f.Evidence.IstSpans)
	}
}
