package autolearn

import (
	"context"
	"strings"
	"testing"
	"time"
)

// cyclesDays puts the cycles of one device on consecutive days.
func cyclesDays(t *testing.T, device string, wh []float32, opW float32) []DayFile {
	t.Helper()
	loc := berlinLoc(t)
	start := time.Date(2026, 3, 2, 8, 0, 0, 0, loc)
	var out []DayFile
	for i, w := range wh {
		at := start.AddDate(0, 0, i)
		out = append(out, DayFile{V: dayFileVersion, Day: at.Format(dayLayout),
			Devices: map[string]*DeviceDay{}, Events: []EventRec{}, Actions: []ActionRec{},
			Cycles: []CycleRec{{Device: device, Start: at.Unix(), DurS: 5400, Wh: w, OpW: opW, PeakW: opW * 1.2}}})
	}
	return out
}

// cyclesInput wraps the days in an input with the Berlin clock.
func cyclesInput(t *testing.T, days []DayFile) *Input {
	t.Helper()
	return &Input{Now: time.Date(2026, 3, 20, 3, 30, 0, 0, berlinLoc(t)), Loc: berlinLoc(t), Days: days}
}

// repeatWh returns n copies of wh with a small spread, so the medians are stable.
func repeatWh(n int, wh float32) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = wh + float32(i%3) - 1
	}
	return out
}

func TestLearnCyclesTwoProgramsBiggestWins(t *testing.T) {
	wh := append(repeatWh(12, 900), repeatWh(4, 500)...)
	days := cyclesDays(t, "geschirr", wh, 2100)
	vals := learnedValues(LearnCyclesDetector.Run(context.Background(), cyclesInput(t, days), "geschirr"))

	cw, ok := learnedValueFor(vals, "device/geschirr/cycle_wh")
	if !ok {
		t.Fatalf("no cycle_wh in %+v", vals)
	}
	if cw.Num < 850 || cw.Num > 950 || cw.Unit != "Wh" || cw.Kind != ValueNumber || cw.Group != "energy" {
		t.Errorf("cycle_wh = %+v, want about 900 Wh, unit Wh, number, energy", cw)
	}
	if cw.N != 12 || cw.Device != "geschirr" {
		t.Errorf("cycle_wh N=%d device=%q, want 12 and geschirr", cw.N, cw.Device)
	}
	if len(cw.Programs) != 1 || cw.Programs[0] < 450 || cw.Programs[0] > 550 {
		t.Errorf("Programs = %v, want one program about 500 Wh", cw.Programs)
	}
	op, ok := learnedValueFor(vals, "device/geschirr/op_power_w")
	if !ok {
		t.Fatalf("no op_power_w in %+v", vals)
	}
	if op.Num != 2100 || op.Unit != "W" || op.N != 12 || op.Programs != nil {
		t.Errorf("op_power_w = %+v, want 2100 W over 12 cycles without programs", op)
	}
}

func TestLearnCyclesSingleProgramHasNoOthers(t *testing.T) {
	days := cyclesDays(t, "waschmaschine", repeatWh(10, 1200), 500)
	vals := learnedValues(LearnCyclesDetector.Run(context.Background(), cyclesInput(t, days), "waschmaschine"))
	cw, ok := learnedValueFor(vals, "device/waschmaschine/cycle_wh")
	if !ok {
		t.Fatalf("no cycle_wh in %+v", vals)
	}
	if cw.Num != 1200 || cw.Programs != nil {
		t.Errorf("cycle_wh = %+v, want 1200 Wh without programs", cw)
	}
}

func TestLearnCyclesFewerThanEightLearnNothing(t *testing.T) {
	days := cyclesDays(t, "geschirr", repeatWh(6, 900), 2100)
	if c := LearnCyclesDetector.Run(context.Background(), cyclesInput(t, days), "geschirr"); len(c) != 0 {
		t.Errorf("6 cycles gave candidates %+v, want none", c)
	}
}

func TestLearnCyclesScopedToDevice(t *testing.T) {
	days := cyclesDays(t, "geschirr", repeatWh(10, 900), 2100)
	other := cyclesDays(t, "trockner", repeatWh(10, 3000), 1800)
	for i := range days {
		days[i].Cycles = append(days[i].Cycles, other[i].Cycles...)
	}
	vals := learnedValues(LearnCyclesDetector.Run(context.Background(), cyclesInput(t, days), "geschirr"))
	for _, v := range vals {
		if v.Device != "geschirr" || !strings.HasPrefix(v.Key, "device/geschirr/") {
			t.Errorf("value %+v is not scoped to geschirr", v)
		}
	}
	if cw, ok := learnedValueFor(vals, "device/geschirr/cycle_wh"); !ok || cw.Num != 900 {
		t.Errorf("cycle_wh = %+v, want 900 for geschirr only", cw)
	}
	if none := LearnCyclesDetector.Run(context.Background(), cyclesInput(t, days), "herd"); len(none) != 0 {
		t.Errorf("unknown device gave candidates %+v, want none", none)
	}
}
