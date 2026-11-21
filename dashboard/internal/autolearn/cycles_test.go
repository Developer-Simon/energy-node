package autolearn

import (
	"math"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

var cycleBase = time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)

// washerPower is one 480 minute period of a washing machine with a 4 minute
// soak: 2 W standby, heating for 60 minutes at 2000 W, a soak, a spin of 20
// minutes at 400 W and standby until the period ends.
func washerPower(offset int) float64 {
	return washerSoakPower(offset, 4)
}

// washerSoakPower is washerPower with a soak of the given length after the
// heating phase.
func washerSoakPower(offset, soak int) float64 {
	switch {
	case offset >= 100 && offset < 160:
		return 2000
	case offset >= 160+soak && offset < 160+soak+20:
		return 400
	}
	return 2
}

// balanceFunc gives the balance of a frame from the device power.
type balanceFunc func(w float64) map[string]float64

// gridBalance is a house where the grid supplies the given share of the load.
func gridBalance(share float64) balanceFunc {
	return func(w float64) map[string]float64 {
		return map[string]float64{"load_total": w, "grid_import": w * share}
	}
}

// noBalance is a frame without balance values.
func noBalance(float64) map[string]float64 { return nil }

// washerFrame builds the frame of minute i.
func washerFrame(i, soak int, bal balanceFunc) learnrec.Frame {
	w := washerSoakPower(i%480, soak)
	return powerFrame("washer", i, w, bal(w))
}

// powerFrame is a single device at w watts in minute i.
func powerFrame(device string, i int, w float64, bal map[string]float64) learnrec.Frame {
	return learnrec.Frame{
		Minute:  cycleBase.Add(time.Duration(i) * time.Minute),
		Devices: []learnrec.DeviceMinute{{ID: device, Power: map[string]float64{"p": w}, Reachable: true}},
		Balance: bal,
	}
}

// fridgeFrame alternates 80 W and 120 W in blocks of 20 minutes.
func fridgeFrame(i int) learnrec.Frame {
	w := 80.0
	if (i/20)%2 == 1 {
		w = 120
	}
	return powerFrame("fridge", i, w, nil)
}

func noOrigin(string, time.Time) string { return "" }

// feedWasher runs the minutes from to (exclusive) and returns the cycles.
func feedWasher(t *CycleTracker, from, to, soak int, bal balanceFunc) []CycleRec {
	var out []CycleRec
	for i := from; i < to; i++ {
		out = append(out, t.ObserveMinute(washerFrame(i, soak, bal), noOrigin)...)
	}
	return out
}

// warmUp is the 2880 minutes needed before a threshold is trusted.
const warmUp = 6 * 480

func TestCycleWasherOneCycle(t *testing.T) {
	tr := NewCycleTracker(nil)
	got := feedWasher(tr, 0, warmUp+480, 4, gridBalance(0))
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
	before := feedWasher(tr, 0, warmUp+130, 4, gridBalance(0))
	if len(before) != 0 {
		t.Fatalf("cycles before the cut = %d, want 0", len(before))
	}
	restored := NewCycleTracker(tr.State())
	got := feedWasher(restored, warmUp+130, warmUp+480, 4, gridBalance(0))
	if len(got) != 1 {
		t.Fatalf("cycles after restore = %d, want 1: %+v", len(got), got)
	}
}

func TestCycleRestartAfterPauseDiscardsOpenCycle(t *testing.T) {
	tr := NewCycleTracker(nil)
	feedWasher(tr, 0, warmUp+130, 4, gridBalance(0))
	if tr.devices["washer"].Open == nil {
		t.Fatal("cycle should be open before the cut")
	}
	restored := NewCycleTracker(tr.State())
	// The next minute comes 30 minutes after the last heating minute.
	got := restored.ObserveMinute(washerFrame(warmUp+160, 4, gridBalance(0)), noOrigin)
	if len(got) != 0 {
		t.Fatalf("cycles = %d, want 0", len(got))
	}
	if restored.devices["washer"].Open != nil {
		t.Fatal("open cycle should be discarded after a restart with a 30 minute pause")
	}
}

func TestCycleGridShareHalf(t *testing.T) {
	tr := NewCycleTracker(nil)
	got := feedWasher(tr, 0, warmUp+480, 4, gridBalance(0.5))
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

// A soak of 8 minutes in one cycle teaches a pause that keeps a later 10
// minute soak inside its cycle.
func TestCycleLongerSoakStaysInOneCycle(t *testing.T) {
	tr := NewCycleTracker(nil)
	feedWasher(tr, 0, warmUp+480, 8, gridBalance(0))
	got := feedWasher(tr, warmUp+480, warmUp+960, 10, gridBalance(0))
	if len(got) != 1 {
		t.Fatalf("cycles = %d, want 1: %+v", len(got), got)
	}
	if want := int(90 * 60); got[0].DurS != want {
		t.Errorf("DurS = %d, want %d (the cycle holds the 10 minute soak)", got[0].DurS, want)
	}
}

// A single above-threshold minute before a long gap is no run after a restart.
func TestCycleStaleRunDroppedAcrossRestart(t *testing.T) {
	tr := NewCycleTracker(nil)
	feedWasher(tr, 0, warmUp, 4, gridBalance(0))
	tr.ObserveMinute(powerFrame("washer", warmUp, 2000, nil), noOrigin)
	restored := NewCycleTracker(tr.State())
	got := restored.ObserveMinute(powerFrame("washer", warmUp+180, 2000, nil), noOrigin)
	if len(got) != 0 {
		t.Fatalf("cycles = %d, want 0", len(got))
	}
	st := restored.devices["washer"]
	if st.Open != nil {
		t.Fatal("a run from 3 hours before must not open a cycle")
	}
	if len(st.Run) != 1 {
		t.Fatalf("run length = %d, want 1 (only the new minute)", len(st.Run))
	}
}

// A gap of hours without a restart does not join two above-threshold minutes.
func TestCycleRunNeedsContiguousMinutes(t *testing.T) {
	tr := NewCycleTracker(nil)
	feedWasher(tr, 0, warmUp, 4, gridBalance(0))
	tr.ObserveMinute(powerFrame("washer", warmUp, 2000, nil), noOrigin)
	tr.ObserveMinute(powerFrame("washer", warmUp+180, 2000, nil), noOrigin)
	st := tr.devices["washer"]
	if st.Open != nil {
		t.Fatal("two minutes 3 hours apart must not open a cycle")
	}
	if len(st.Run) != 1 {
		t.Fatalf("run length = %d, want 1", len(st.Run))
	}
}

// A cycle that never ends is cut at 24 hours and ends as self.
func TestCycleCutAtMaxLength(t *testing.T) {
	tr := NewCycleTracker(nil)
	feedWasher(tr, 0, warmUp, 4, gridBalance(0))
	var got []CycleRec
	// 1442 minutes at 2000 W: the cut comes with minute 1440 of the cycle,
	// and minute 1441 opens the next cycle.
	for i := 0; i < cycleMaxMinutes+2; i++ {
		got = append(got, tr.ObserveMinute(powerFrame("washer", warmUp+i, 2000, nil), noOrigin)...)
	}
	if len(got) != 1 {
		t.Fatalf("cycles = %d, want 1", len(got))
	}
	if got[0].End != "self" {
		t.Errorf("End = %q, want self", got[0].End)
	}
	if want := cycleMaxMinutes * 60; got[0].DurS != want {
		t.Errorf("DurS = %d, want %d", got[0].DurS, want)
	}
	if tr.devices["washer"].Open == nil {
		t.Fatal("the minute after the cut should start a new cycle")
	}
}

// Without a balance the source shares are unknown: all three are 0.
func TestCycleSharesZeroWithoutBalance(t *testing.T) {
	tr := NewCycleTracker(nil)
	got := feedWasher(tr, 0, warmUp+480, 4, noBalance)
	if len(got) != 1 {
		t.Fatalf("cycles = %d, want 1", len(got))
	}
	if got[0].Grid != 0 || got[0].PV != 0 || got[0].Bat != 0 {
		t.Errorf("shares = %v/%v/%v, want 0/0/0 without load", got[0].Grid, got[0].PV, got[0].Bat)
	}
}
