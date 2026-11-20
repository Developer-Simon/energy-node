package autolearn

import (
	"context"
	"testing"
	"time"
)

// runtimeRuns returns the ordinary runs on the first days at 18:00 with the given
// durations, followed by the long runs on the next days at 08:00. The runs end on
// their own day, so the test days need no extra day.
func runtimeRuns(normal []float64, long []float64, longEnd Origin) []runSpec {
	var runs []runSpec
	for i, m := range normal {
		runs = append(runs, runSpec{Day: i, Start: 18 * 60, Minutes: m})
	}
	for i, m := range long {
		runs = append(runs, runSpec{Day: len(normal) + i, Start: 8 * 60, Minutes: m, EndOrigin: longEnd})
	}
	return runs
}

// runtimeCands runs the runtime detector on the days of one device.
func runtimeCands(t *testing.T, days []DayFile, in *Input) []Candidate {
	t.Helper()
	if in == nil {
		in = switchTimeInput(t, days)
	}
	return RuntimeDetector.Run(context.Background(), in, "heater")
}

var runtimeNormal40 = []float64{35, 38, 40, 42, 45, 36, 44, 40, 39, 41, 37, 43}

func TestRuntimeLongRunsAfterUsualRunsSuggestsOff(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := runDays(t, start, 14, "heater", 1200, runtimeRuns(runtimeNormal40, []float64{360, 360}, ""))
	cands := runtimeCands(t, days, nil)
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1: %+v", len(cands), cands)
	}
	c := cands[0].Finding
	if c.ID != "runtime:heater" || c.Summary.Code != "runtime.forgotten" {
		t.Fatalf("finding = %s / %s", c.ID, c.Summary.Code)
	}
	if c.Evidence.Shape != "scale" || c.Evidence.IstValue == nil || *c.Evidence.IstValue != 360 {
		t.Errorf("evidence = %+v, want longest forgotten run 360", c.Evidence)
	}
	if c.Evidence.SollValue == nil || *c.Evidence.SollValue <= 45 || *c.Evidence.SollValue > 60 {
		t.Errorf("suggested switch-off = %v, want within (45, 60]", c.Evidence.SollValue)
	}
	if c.Apply[0].Template != "runtime_off" || c.Apply[0].Prefill["device"] != "heater" {
		t.Errorf("apply = %+v", c.Apply)
	}
	if c.Apply[0].Prefill["entity"] != "switch.heater" {
		t.Errorf("prefill entity = %v, want switch.heater", c.Apply[0].Prefill["entity"])
	}
	if c.Benefit == nil || c.Benefit.Unit != "kwh_week" || c.Benefit.Value <= 0 {
		t.Errorf("benefit = %+v", c.Benefit)
	}
}

func TestRuntimeTwoClustersWithoutOutliersIsNil(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	short := []float64{8, 9, 10, 10, 11, 12}
	long := []float64{170, 175, 180, 180, 185, 190}
	days := runDays(t, start, 12, "heater", 1000, runtimeRuns(short, long, ""))
	if cands := runtimeCands(t, days, nil); len(cands) != 0 {
		t.Fatalf("candidates = %+v, want none", cands)
	}
}

func TestRuntimeRuleEndedRunsAreNotForgotten(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := runDays(t, start, 14, "heater", 1200, runtimeRuns(runtimeNormal40, []float64{360, 360}, OriginRule))
	if cands := runtimeCands(t, days, nil); len(cands) != 0 {
		t.Fatalf("candidates = %+v, want none", cands)
	}
}

func TestRuntimeNoiseIsNil(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	noise := []float64{20, 25, 30, 33, 36, 40, 42, 45, 48, 52, 56, 60, 38, 44}
	days := runDays(t, start, 14, "heater", 1200, runtimeRuns(noise, nil, ""))
	if cands := runtimeCands(t, days, nil); len(cands) != 0 {
		t.Fatalf("candidates = %+v, want none", cands)
	}
}

func TestRuntimeNeedsEightOrdinaryRuns(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := runDays(t, start, 10, "heater", 1200, runtimeRuns(runtimeNormal40[:7], []float64{360, 360}, ""))
	if cands := runtimeCands(t, days, nil); len(cands) != 0 {
		t.Fatalf("candidates = %+v, want none with 7 ordinary runs", cands)
	}
}

func TestRuntimeNeedsHalfKWhExtraEnergy(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	// 40 W for two forgotten runs of 6 h is about 0.43 kWh of extra energy.
	days := runDays(t, start, 14, "heater", 40, runtimeRuns(runtimeNormal40, []float64{360, 360}, ""))
	if cands := runtimeCands(t, days, nil); len(cands) != 0 {
		t.Fatalf("candidates = %+v, want none below 0.5 kWh", cands)
	}
}

func TestRuntimeOvernightFactCountsRunsThroughNightRest(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	// Two long runs from 20:00 end after midnight, so the days need one more.
	var runs []runSpec
	for i, m := range runtimeNormal40 {
		runs = append(runs, runSpec{Day: i, Start: 18 * 60, Minutes: m})
	}
	runs = append(runs, runSpec{Day: 12, Start: 20 * 60, Minutes: 360}, runSpec{Day: 13, Start: 20 * 60, Minutes: 360})
	days := runDays(t, start, 15, "heater", 1200, runs)
	in := switchTimeInput(t, days)
	in.Base.NightRest = map[int]float64{0: 23 * 60}
	cands := runtimeCands(t, days, in)
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1", len(cands))
	}
	found := false
	for _, f := range cands[0].Finding.Facts {
		if f.Code == "overnight" {
			found = f.Value == 2
		}
	}
	if !found {
		t.Errorf("facts = %+v, want overnight 2", cands[0].Finding.Facts)
	}
}
