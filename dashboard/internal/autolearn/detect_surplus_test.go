package autolearn

import (
	"context"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// surplusWinNow is the clock of the surplus window tests.
func surplusWinNow(t *testing.T) time.Time {
	t.Helper()
	return time.Date(2026, 11, 22, 8, 0, 0, 0, berlinLoc(t))
}

// surplusWinDay builds a day with every quarter seen. grid returns the grid
// export in W of a quarter, pv the measured PV in W and k the weather factor.
func surplusWinDay(day string, grid func(q int) float32, pv func(q int) float32) DayFile {
	f := DayFile{V: 1, Day: day}
	for q := 0; q < dayQuarters; q++ {
		f.Site.Seen[q] = 1
		f.Site.LoadTotal[q] = 400
		f.Site.GridExport[q] = grid(q)
		f.Site.PV[q] = pv(q)
		k := float32(1)
		f.Site.K[q] = &k
	}
	return f
}

// surplusWinDays returns the 14 days before 2026-11-22. Day i is built by fn.
func surplusWinDays(loc *time.Location, fn func(i int, q int) (grid, pv float32)) []DayFile {
	first := time.Date(2026, 11, 8, 0, 0, 0, 0, loc)
	var out []DayFile
	for i := 0; i < surWinWindowDays; i++ {
		i := i
		day := first.AddDate(0, 0, i).Format(dayLayout)
		out = append(out, surplusWinDay(day,
			func(q int) float32 { g, _ := fn(i, q); return g },
			func(q int) float32 { _, p := fn(i, q); return p }))
	}
	return out
}

// surplusWinInput builds the input of a 1500 W device with a 2 h run.
func surplusWinInput(t *testing.T, days []DayFile) *Input {
	t.Helper()
	loc := berlinLoc(t)
	return &Input{
		Now:  surplusWinNow(t),
		Loc:  loc,
		Days: days,
		Doc:  Doc{Location: shadeSite},
		Devices: map[string]DeviceInfo{
			"plug": {ID: "plug", Switchable: true, Entities: []string{"switch.plug"}},
		},
		Learned: map[string]LearnedEntry{
			"device/plug/op_power_w": {Key: "device/plug/op_power_w", Value: 1500.0},
			"device/plug/cycle_wh":   {Key: "device/plug/cycle_wh", Value: 3000.0},
		},
	}
}

// surplusWinExport is 2500 W of export from 11 to 15 o'clock on the first twelve days.
func surplusWinExport(i, q int) (float32, float32) {
	if i < 12 && q >= 44 && q < 60 {
		return 2500, 0
	}
	return 0, 0
}

func surplusWinRun(t *testing.T, in *Input) []Candidate {
	t.Helper()
	return SurplusDetector.Run(context.Background(), in, "plug")
}

func TestSurplusWindowSuggestsSwitchOnWindow(t *testing.T) {
	loc := berlinLoc(t)
	in := surplusWinInput(t, surplusWinDays(loc, surplusWinExport))
	got := surplusWinRun(t, in)
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1", len(got))
	}
	c := got[0]
	if c.Finding.Summary.Code != "surplus.window" {
		t.Errorf("summary = %q, want surplus.window", c.Finding.Summary.Code)
	}
	if c.Finding.ID != "surplus:plug" {
		t.Errorf("id = %q, want surplus:plug", c.Finding.ID)
	}
	if len(c.Finding.Apply) != 1 {
		t.Fatalf("apply = %d fixes, want 1", len(c.Finding.Apply))
	}
	fix := c.Finding.Apply[0]
	if fix.Template != "surplus_switch" {
		t.Errorf("template = %q, want surplus_switch", fix.Template)
	}
	if on, _ := fix.Prefill["on_w"].(float64); on != 1700 {
		t.Errorf("on_w = %v, want 1700", fix.Prefill["on_w"])
	}
	if src := fix.Prefill["source"]; src != "export" {
		t.Errorf("source = %v, want export", src)
	}
	if c.Finding.Benefit == nil || c.Finding.Benefit.Unit != "kwh_week" || c.Finding.Benefit.Value <= 0 {
		t.Errorf("benefit = %+v, want a positive kwh_week", c.Finding.Benefit)
	}
	if c.Finding.Evidence.Shape != "curve" {
		t.Errorf("evidence shape = %q, want curve", c.Finding.Evidence.Shape)
	}
}

func TestSurplusWindowReserveSource(t *testing.T) {
	loc := berlinLoc(t)
	// No export, the feed-in is throttled to 2000 W. The reserve is the PV that
	// the roof would make beyond the throttle.
	model := PVModel{Surfaces: []Surface{{PMax: 20000, TiltDeg: 30, AzimuthDeg: 180}}, CapW: 20000, Safe: true}
	days := surplusWinDays(loc, func(i, q int) (float32, float32) {
		if i >= 12 {
			return 0, 0
		}
		day := time.Date(2026, 11, 8, 0, 0, 0, 0, loc).AddDate(0, 0, i)
		az, el := sunPosForTest(day, q)
		best := model.Best(az, el, nil)
		return 0, float32(min(best, 2000))
	})
	in := surplusWinInput(t, days)
	in.PV = func() (PVModel, *Raster, bool) { return model, nil, true }
	got := surplusWinRun(t, in)
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1", len(got))
	}
	c := got[0]
	if c.Finding.Summary.Code != "surplus.reserve" {
		t.Errorf("summary = %q, want surplus.reserve", c.Finding.Summary.Code)
	}
	if src := c.Finding.Apply[0].Prefill["source"]; src != "reserve" {
		t.Errorf("source = %v, want reserve", src)
	}
}

func TestSurplusWindowShortWindowNoSuggestion(t *testing.T) {
	loc := berlinLoc(t)
	short := func(i, q int) (float32, float32) {
		if i < 12 && q >= 44 && q < 48 { // 1 h of export, the run needs 2 h
			return 2500, 0
		}
		return 0, 0
	}
	if got := surplusWinRun(t, surplusWinInput(t, surplusWinDays(loc, short))); len(got) != 0 {
		t.Errorf("candidates = %d, want 0 for a 1 h window", len(got))
	}
}

func TestSurplusWindowExistingSurplusRuleNoSuggestion(t *testing.T) {
	loc := berlinLoc(t)
	in := surplusWinInput(t, surplusWinDays(loc, surplusWinExport))
	in.Doc.Rules = []Rule{{
		ID:      "zero-feed",
		Enabled: true,
		Conditions: []Condition{{Type: "balance_threshold", Field: "grid_export",
			Comparison: "above", Threshold: 1000}},
		Actions: []Action{{Type: "switch", EntityID: "switch.plug"}},
	}}
	if got := surplusWinRun(t, in); len(got) != 0 {
		t.Errorf("candidates = %d, want 0 with an existing surplus rule", len(got))
	}
}

func TestSurplusWindowNoisePatternNoSuggestion(t *testing.T) {
	loc := berlinLoc(t)
	noise := func(i, q int) (float32, float32) {
		return float32(300 + (q*37+i*11)%500), 0
	}
	if got := surplusWinRun(t, surplusWinInput(t, surplusWinDays(loc, noise))); len(got) != 0 {
		t.Errorf("candidates = %d, want 0 without a pattern", len(got))
	}
}

func TestSurplusWindowNeedsLearnedPower(t *testing.T) {
	loc := berlinLoc(t)
	in := surplusWinInput(t, surplusWinDays(loc, surplusWinExport))
	in.Learned = nil
	if got := surplusWinRun(t, in); len(got) != 0 {
		t.Errorf("candidates = %d, want 0 without learned operating power", len(got))
	}
}

// sunPosForTest is the sun position of quarter q of the day, as the surplus detector reads it.
func sunPosForTest(day time.Time, q int) (float64, float64) {
	return sun.Position(shadeQuarterTime(day, q), shadeSite.Lat, shadeSite.Lon)
}
