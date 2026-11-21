package autolearn

import (
	"math"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

var cycleBase = time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)

// washerPower is one 480 minute period of a washing machine: 2 W standby, a
// heating phase of 60 minutes at 2000 W, a 4 minute soak at standby, a spin of
// 20 minutes at 400 W and standby until the period ends.
func washerPower(offset int) float64 {
	switch {
	case offset >= 100 && offset < 160:
		return 2000
	case offset >= 164 && offset < 184:
		return 400
	}
	return 2
}

// washerFrame builds the frame of minute i, with grid import at the given
// share of the house load.
func washerFrame(i int, gridShare float64) learnrec.Frame {
	w := washerPower(i % 480)
	return learnrec.Frame{
		Minute:  cycleBase.Add(time.Duration(i) * time.Minute),
		Devices: []learnrec.DeviceMinute{{ID: "washer", Power: map[string]float64{"p": w}, Reachable: true}},
		Balance: map[string]float64{"load_total": w, "grid_import": w * gridShare},
	}
}

// fridgeFrame alternates 80 W and 120 W in blocks of 20 minutes.
func fridgeFrame(i int) learnrec.Frame {
	w := 80.0
	if (i/20)%2 == 1 {
		w = 120
	}
	return learnrec.Frame{
		Minute:  cycleBase.Add(time.Duration(i) * time.Minute),
		Devices: []learnrec.DeviceMinute{{ID: "fridge", Power: map[string]float64{"p": w}, Reachable: true}},
	}
}

func noOrigin(string, time.Time) string { return "" }

// feedWasher runs the minutes from to (exclusive) and returns the cycles.
func feedWasher(t *CycleTracker, from, to int, gridShare float64) []CycleRec {
	var out []CycleRec
	for i := from; i < to; i++ {
		out = append(out, t.ObserveMinute(washerFrame(i, gridShare), noOrigin)...)
	}
	return out
}

// warmUp is the 2880 minutes needed before a threshold is trusted.
const warmUp = 6 * 480

func TestCycleWasherOneCycle(t *testing.T) {
	tr := NewCycleTracker(nil)
	got := feedWasher(tr, 0, warmUp+480, 0)
	if len(got) != 1 {
		t.Fatalf("cycles = %d, want 1: %+v", len(got), got)
	}
	c := got[0]
	if c.Device != "washer" {
		t.Errorf("Device = %q, want washer", c.Device)
	}
	if math.Abs(float64(c.OpW)-2000) > 100 {
		t.Errorf("OpW = %v, want 2000 +- 100", c.OpW)
	}
	if c.PeakW < 2000 {
		t.Errorf("PeakW = %v, want >= 2000", c.PeakW)
	}
	if c.End != "self" {
		t.Errorf("End = %q, want self", c.End)
	}
	if c.Start != cycleBase.Add(time.Duration(warmUp+100)*time.Minute).Unix() {
		t.Errorf("Start = %d, want the minute the heating begins", c.Start)
	}
}

func TestCycleFridgeHasNoThreshold(t *testing.T) {
	tr := NewCycleTracker(nil)
	var got []CycleRec
	for i := 0; i < 3*1440; i++ {
		got = append(got, tr.ObserveMinute(fridgeFrame(i), noOrigin)...)
	}
	if len(got) != 0 {
		t.Fatalf("fridge cycles = %d, want 0", len(got))
	}
	if thr := tr.devices["fridge"].Thr; thr != 0 {
		t.Fatalf("fridge threshold = %v, want none", thr)
	}
}

func TestCycleStateMidCycleGivesOneCycle(t *testing.T) {
	tr := NewCycleTracker(nil)
	before := feedWasher(tr, 0, warmUp+130, 0)
	if len(before) != 0 {
		t.Fatalf("cycles before the cut = %d, want 0", len(before))
	}
	restored := NewCycleTracker(tr.State())
	got := feedWasher(restored, warmUp+130, warmUp+480, 0)
	if len(got) != 1 {
		t.Fatalf("cycles after restore = %d, want 1: %+v", len(got), got)
	}
}

func TestCycleRestartAfterPauseDiscardsOpenCycle(t *testing.T) {
	tr := NewCycleTracker(nil)
	feedWasher(tr, 0, warmUp+130, 0)
	if tr.devices["washer"].Open == nil {
		t.Fatal("cycle should be open before the cut")
	}
	restored := NewCycleTracker(tr.State())
	// The next minute comes 30 minutes after the last heating minute.
	got := restored.ObserveMinute(washerFrame(warmUp+160, 0), noOrigin)
	if len(got) != 0 {
		t.Fatalf("cycles = %d, want 0", len(got))
	}
	if restored.devices["washer"].Open != nil {
		t.Fatal("open cycle should be discarded after a restart with a 30 minute pause")
	}
}

func TestCycleGridShareHalf(t *testing.T) {
	tr := NewCycleTracker(nil)
	got := feedWasher(tr, 0, warmUp+480, 0.5)
	if len(got) != 1 {
		t.Fatalf("cycles = %d, want 1", len(got))
	}
	if math.Abs(float64(got[0].Grid)-0.5) > 1e-4 {
		t.Errorf("Grid = %v, want 0.5", got[0].Grid)
	}
	if math.Abs(float64(got[0].PV)-0.5) > 1e-4 {
		t.Errorf("PV = %v, want 0.5", got[0].PV)
	}
	if got[0].Bat != 0 {
		t.Errorf("Bat = %v, want 0", got[0].Bat)
	}
}
