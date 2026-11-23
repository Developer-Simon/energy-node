package autolearn

import (
	"context"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

// shiftTDay builds a day with every quarter seen. The grid export is surW in the
// quarters from fromQ up to toQ, zero elsewhere.
func shiftTDay(day string, fromQ, toQ int, surW float32, cycles []CycleRec) DayFile {
	f := DayFile{V: 1, Day: day, Cycles: cycles}
	for q := 0; q < dayQuarters; q++ {
		f.Site.Seen[q] = 1
		if q >= fromQ && q < toQ {
			f.Site.GridExport[q] = surW
		}
	}
	return f
}

// shiftTCycle is a finished cycle that started at minute of the day of the file.
func shiftTCycle(t *testing.T, loc *time.Location, day string, minute int, device string, durMin int, wh, opW, grid float32) CycleRec {
	t.Helper()
	d, err := time.ParseInLocation(dayLayout, day, loc)
	if err != nil {
		t.Fatal(err)
	}
	at := d.Add(time.Duration(minute) * time.Minute)
	return CycleRec{Device: device, Start: at.Unix(), DurS: durMin * 60, Wh: wh, OpW: opW, PeakW: opW * 1.2, Grid: grid}
}

// shiftTDays returns n days from 2026-11-08 on, built by fn(i, day).
func shiftTDays(n int, loc *time.Location, fn func(i int, day string) DayFile) []DayFile {
	first := time.Date(2026, 11, 8, 0, 0, 0, 0, loc)
	var out []DayFile
	for i := 0; i < n; i++ {
		out = append(out, fn(i, first.AddDate(0, 0, i).Format(dayLayout)))
	}
	return out
}

// shiftTInput builds the input with the given days and switchable devices.
func shiftTInput(t *testing.T, days []DayFile, devices ...string) *Input {
	t.Helper()
	loc := berlinLoc(t)
	devs := map[string]DeviceInfo{}
	for _, id := range devices {
		devs[id] = DeviceInfo{ID: id, Role: energy.RoleSwitch, Switchable: true, Entities: []string{"switch." + id}}
	}
	return &Input{
		Now:     time.Date(2026, 11, 22, 8, 0, 0, 0, loc),
		Loc:     loc,
		Days:    days,
		Doc:     Doc{Location: shadeSite},
		Devices: devs,
	}
}

// shiftTWash gives a washing machine that runs in the evening from the grid,
// with its start spread over about four hours. The surplus of 3000 W lies at
// noon for four hours.
func shiftTWash(t *testing.T, loc *time.Location, n int) []DayFile {
	return shiftTDays(n, loc, func(i int, day string) DayFile {
		c := shiftTCycle(t, loc, day, 1020+24*(i%10), "wash", 120, 1000, 2000, 0.9)
		return shiftTDay(day, 44, 60, 3000, []CycleRec{c})
	})
}

// TestShiftSuggestsGridCyclesMovableToSurplus: the washing machine runs in the
// evening from the grid while the surplus is there at noon.
func TestShiftSuggestsGridCyclesMovableToSurplus(t *testing.T) {
	loc := berlinLoc(t)
	in := shiftTInput(t, shiftTWash(t, loc, 10), "wash")
	cands := ShiftDetector.Run(context.Background(), in, "wash")
	if len(cands) != 1 {
		t.Fatalf("want one suggestion, got %d", len(cands))
	}
	c := cands[0]
	if c.Finding.ID != "shift:wash" {
		t.Fatalf("ID = %q, want shift:wash", c.Finding.ID)
	}
	if len(c.Finding.Apply) != 1 || c.Finding.Apply[0].Template != "shift_start" {
		t.Fatalf("apply = %+v, want the shift_start template", c.Finding.Apply)
	}
	p := c.Finding.Apply[0].Prefill
	if p["device"] != "wash" {
		t.Errorf("prefill device = %v, want wash", p["device"])
	}
	if _, ok := p["entity"]; ok {
		t.Errorf("prefill carries an entity %v, want none: the first entity is not a safe switch", p["entity"])
	}
	if p["on_w"] != 2000.0 {
		t.Errorf("prefill on_w = %v, want 2000", p["on_w"])
	}
	if p["latest"] != "19:45" {
		t.Errorf("prefill latest = %v, want 19:45", p["latest"])
	}
	if c.Finding.Benefit == nil || c.Finding.Benefit.Unit != "kwh_week" || c.Finding.Benefit.Value <= 0 {
		t.Errorf("benefit = %+v, want a positive kwh_week", c.Finding.Benefit)
	}
	if c.Replay == nil {
		t.Errorf("replay = nil, want a replay verdict")
	}
}

// TestShiftFixedStartNoSuggestion: a device that always starts at 07:00 has no
// spread of start times, so it is a habit and not a shiftable load.
func TestShiftFixedStartNoSuggestion(t *testing.T) {
	loc := berlinLoc(t)
	days := shiftTDays(10, loc, func(i int, day string) DayFile {
		c := shiftTCycle(t, loc, day, 420, "wash", 120, 1000, 2000, 0.9)
		return shiftTDay(day, 44, 60, 3000, []CycleRec{c})
	})
	in := shiftTInput(t, days, "wash")
	if cands := ShiftDetector.Run(context.Background(), in, "wash"); len(cands) != 0 {
		t.Fatalf("fixed start: want no suggestion, got %d", len(cands))
	}
}

// TestShiftCompetitionGoesToMoreGridEnergy: one surplus window of one hour fits
// only one of two devices. The dryer takes more grid energy and gets it.
func TestShiftCompetitionGoesToMoreGridEnergy(t *testing.T) {
	loc := berlinLoc(t)
	days := shiftTDays(10, loc, func(i int, day string) DayFile {
		start := 1020 + 24*(i%10)
		dryer := shiftTCycle(t, loc, day, start, "dryer", 60, 2000, 2000, 1.0)
		wash := shiftTCycle(t, loc, day, start, "wash", 30, 1000, 2000, 0.9)
		return shiftTDay(day, 48, 52, 3000, []CycleRec{dryer, wash})
	})
	in := shiftTInput(t, days, "dryer", "wash")
	if cands := ShiftDetector.Run(context.Background(), in, "wash"); len(cands) != 0 {
		t.Fatalf("wash: want no suggestion, got %d", len(cands))
	}
	cands := ShiftDetector.Run(context.Background(), in, "dryer")
	if len(cands) != 1 || cands[0].Finding.ID != "shift:dryer" {
		t.Fatalf("dryer: want shift:dryer, got %d candidates", len(cands))
	}
}

// TestShiftNeedsEightRuns: six runs are not enough for a safe suggestion.
func TestShiftNeedsEightRuns(t *testing.T) {
	loc := berlinLoc(t)
	in := shiftTInput(t, shiftTWash(t, loc, 6), "wash")
	if cands := ShiftDetector.Run(context.Background(), in, "wash"); len(cands) != 0 {
		t.Fatalf("6 runs: want no suggestion, got %d", len(cands))
	}
}

// TestShiftNoSurplusNoSuggestion: without surplus on the day the runs cannot move.
func TestShiftNoSurplusNoSuggestion(t *testing.T) {
	loc := berlinLoc(t)
	days := shiftTDays(10, loc, func(i int, day string) DayFile {
		c := shiftTCycle(t, loc, day, 1020+24*(i%10), "wash", 120, 1000, 2000, 0.9)
		return shiftTDay(day, 0, 0, 0, []CycleRec{c})
	})
	in := shiftTInput(t, days, "wash")
	if cands := ShiftDetector.Run(context.Background(), in, "wash"); len(cands) != 0 {
		t.Fatalf("no surplus: want no suggestion, got %d", len(cands))
	}
}
