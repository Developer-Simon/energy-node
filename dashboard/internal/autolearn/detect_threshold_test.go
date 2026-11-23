package autolearn

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// thresholdDoc builds the rule document of one surplus rule switching the plug.
func thresholdDoc(t *testing.T, threshold float64, extra string) Doc {
	t.Helper()
	raw := fmt.Sprintf(`{"rules":[{"id":"heat","name":"Heizstab","enabled":true,
		"conditions":[{"type":"balance_threshold","field":"grid_export","comparison":"above","threshold":%v,"hold_seconds":120%s}],
		"actions":[{"type":"publish","topic":"plug/set","payload":"ON","entity_id":"switch.plug"}]}]}`, threshold, extra)
	doc, err := ParseDoc([]byte(raw))
	if err != nil {
		t.Fatalf("parse doc: %v", err)
	}
	return doc
}

// thresholdInput builds a 1500 W plug with a 3000 Wh cycle and 14 days.
func thresholdInput(t *testing.T, doc Doc, days []DayFile) *Input {
	t.Helper()
	return &Input{
		Now:  surplusWinNow(t),
		Loc:  berlinLoc(t),
		Days: days,
		Doc:  doc,
		Devices: map[string]DeviceInfo{
			"plug": {ID: "plug", Switchable: true, Entities: []string{"switch.plug"}},
		},
		Learned: map[string]LearnedEntry{
			"device/plug/op_power_w": {Key: "device/plug/op_power_w", Value: 1500.0},
			"device/plug/cycle_wh":   {Key: "device/plug/cycle_wh", Value: 3000.0},
		},
		Limits: learnrec.LimitsFor(learnrec.LevelBalanced, 1),
	}
}

// thresholdFixtureDays builds the 14 days before today. grid and imp return the grid
// export and grid import in W of a quarter of day i.
func thresholdFixtureDays(t *testing.T, fn func(i, q int) (exp, imp float32)) []DayFile {
	t.Helper()
	loc := berlinLoc(t)
	first := time.Date(2026, 11, 8, 0, 0, 0, 0, loc)
	var out []DayFile
	for i := 0; i < thresholdReplayDays; i++ {
		i := i
		f := DayFile{V: 1, Day: first.AddDate(0, 0, i).Format(dayLayout)}
		for q := 0; q < dayQuarters; q++ {
			exp, imp := fn(i, q)
			f.Site.Seen[q] = 1
			f.Site.LoadTotal[q] = 400
			f.Site.GridExport[q] = exp
			f.Site.GridImport[q] = imp
			k := float32(1)
			f.Site.K[q] = &k
		}
		out = append(out, f)
	}
	return out
}

// thresholdRunOn runs the rating on the plug.
func thresholdRunOn(t *testing.T, in *Input) []Candidate {
	t.Helper()
	return ThresholdDetector.Run(context.Background(), in, "")
}

// surplus2500 is 2500 W export from 11 to 15 o'clock.
func surplus2500(i, q int) (float32, float32) {
	if q >= 44 && q < 60 {
		return 2500, 0
	}
	return 0, 0
}

func TestThresholdTooHighLowersToPlateau(t *testing.T) {
	in := thresholdInput(t, thresholdDoc(t, 3000, ""), thresholdFixtureDays(t, surplus2500))
	got := thresholdRunOn(t, in)
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1", len(got))
	}
	f := got[0].Finding
	if f.Summary.Code != "threshold.too_high" {
		t.Fatalf("code = %s, want threshold.too_high", f.Summary.Code)
	}
	soll := f.Evidence.SollValue
	if soll == nil || *soll < 1500 || *soll > 1900 {
		t.Fatalf("soll = %v, want about 1700", soll)
	}
	if len(f.Apply) != 2 || f.Apply[0].Code != "set_threshold" || f.Apply[1].Code != "learned_threshold" {
		t.Fatalf("fixes = %+v", f.Apply)
	}
	if got[0].Replay == nil {
		t.Fatal("candidate without replay verdict")
	}
}

func TestThresholdTooLowRaisesWithGridImport(t *testing.T) {
	// 600 W export from 12 to 14 o'clock. The rule at 500 W takes it, and the
	// load draws the rest from the grid.
	day := func(i, q int) (float32, float32) {
		if q >= 48 && q < 56 {
			return 600, 0
		}
		return 0, 0
	}
	in := thresholdInput(t, thresholdDoc(t, 500, ""), thresholdFixtureDays(t, day))
	got := thresholdRunOn(t, in)
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1", len(got))
	}
	if code := got[0].Finding.Summary.Code; code != "threshold.too_low" {
		t.Fatalf("code = %s, want threshold.too_low", code)
	}
	if soll := got[0].Finding.Evidence.SollValue; soll == nil || *soll <= 500 {
		t.Fatalf("soll = %v, want above 500", soll)
	}
}

func TestThresholdHalvesDisagreeNoFinding(t *testing.T) {
	// Surplus only on the first seven days: the second half gains nothing.
	day := func(i, q int) (float32, float32) {
		if i < 7 {
			return surplus2500(i, q)
		}
		return 0, 0
	}
	in := thresholdInput(t, thresholdDoc(t, 3000, ""), thresholdFixtureDays(t, day))
	if got := thresholdRunOn(t, in); len(got) != 0 {
		t.Fatalf("candidates = %d, want 0", len(got))
	}
}

func TestThresholdNotReplayableRuleSilent(t *testing.T) {
	doc := thresholdDoc(t, 3000, `},{"type":"chain_value","comparison":"above","threshold":1,"hold_seconds":0`)
	in := thresholdInput(t, doc, thresholdFixtureDays(t, surplus2500))
	if got := thresholdRunOn(t, in); len(got) != 0 {
		t.Fatalf("candidates = %d, want 0", len(got))
	}
}

func TestThresholdWidestPlateauMiddle(t *testing.T) {
	var row []thresholdPoint
	for x := 1000.0; x <= 2400; x += 100 {
		j := 0.0
		switch {
		case x == 1000:
			j = 10
		case x >= 1600 && x <= 2000:
			j = 9.6
		}
		row = append(row, thresholdPoint{x: x, j: j})
	}
	if mid := thresholdWidest(row).x; mid != 1800 {
		t.Fatalf("mid = %v, want 1800", mid)
	}
}

func TestThresholdJointHoldAddsFactsOnly(t *testing.T) {
	in := thresholdInput(t, thresholdDoc(t, 3000, ""), thresholdFixtureDays(t, surplus2500))
	in.Limits = learnrec.LimitsFor(learnrec.LevelFull, 4)
	got := thresholdRunOn(t, in)
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1", len(got))
	}
	f := got[0].Finding
	if len(f.Facts) != 2 || f.Facts[0].Code != "hysteresis" || f.Facts[1].Code != "min_run_s" {
		t.Fatalf("facts = %+v, want hysteresis and min_run_s", f.Facts)
	}
	if len(f.Apply) != 2 || len(f.Apply[0].Ops) != 1 {
		t.Fatalf("joint hold must set only the threshold, fixes = %+v", f.Apply)
	}
}

func TestThresholdWidestNegative(t *testing.T) {
	// All J are negative. The maximum -1 sits at 1200, the points within 5 %
	// of it (1000 to 1400) form the plateau, its middle is 1200.
	var row []thresholdPoint
	for i := 0; i < 15; i++ {
		x := 600 + 100*float64(i)
		j := -3.0
		if math.Abs(x-1200) <= 200 {
			j = -1.0 - 0.02*math.Abs(x-1200)/200
		}
		row = append(row, thresholdPoint{x: x, j: j})
	}
	if mid := thresholdWidest(row).x; mid != 1200 {
		t.Fatalf("mid = %v, want 1200", mid)
	}
}

func TestThresholdBaseReplayOnce(t *testing.T) {
	// Count the base replays by the minutes they run on and their MinRunS.
	// Every pair must run once, however many grid points use it.
	calls := map[string]int{}
	orig := thresholdBaseReplay
	thresholdBaseReplay = func(ctx context.Context, rule any, target ReplayTarget, mins []ReplayMinute, real []bool, loc *time.Location, site sun.Location) (ReplayResult, error) {
		if r, ok := rule.(Rule); ok && r.ID == replayBaseRule.ID && len(mins) > 0 {
			calls[fmt.Sprintf("%v/%d/%v", mins[0].At, len(mins), target.MinRunS)]++
		}
		return orig(ctx, rule, target, mins, real, loc, site)
	}
	t.Cleanup(func() { thresholdBaseReplay = orig })

	in := thresholdInput(t, thresholdDoc(t, 3000, ""), thresholdFixtureDays(t, surplus2500))
	in.Limits = learnrec.LimitsFor(learnrec.LevelFull, 4)
	thresholdRunOn(t, in)
	if len(calls) == 0 {
		t.Fatal("no base replay counted")
	}
	for k, n := range calls {
		if n != 1 {
			t.Errorf("base replay %s ran %d times, want 1", k, n)
		}
	}
}
