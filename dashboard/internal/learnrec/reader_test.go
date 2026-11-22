package learnrec

import (
	"math"
	"testing"
	"time"
)

func rdFrame(at time.Time, watts float64) Frame {
	return Frame{
		Minute:  at,
		Devices: []DeviceMinute{{ID: "dev", Power: map[string]float64{"uid1": watts}}},
	}
}

func TestMinuteStoreRangeAcrossMidnight(t *testing.T) {
	env := newMsEnv(t, LocRAM)
	s := newMsStore(env)
	base := time.Date(2026, 11, 20, 23, 58, 0, 0, time.UTC)
	for i, w := range []float64{1, 2, 3, 4} {
		s.ObserveMinute(rdFrame(base.Add(time.Duration(i)*time.Minute), w))
	}

	got := s.Range("e:uid1", base, base.Add(4*time.Minute))
	if !got.From.Equal(base) {
		t.Fatalf("From = %v, want %v", got.From, base)
	}
	want := []float32{1, 2, 3, 4}
	if len(got.Values) != len(want) {
		t.Fatalf("len = %d, want %d", len(got.Values), len(want))
	}
	for i := range want {
		if got.Values[i] != want[i] {
			t.Errorf("Values[%d] = %v, want %v", i, got.Values[i], want[i])
		}
	}

	gap := s.Range("e:uid1", base.Add(4*time.Minute), base.Add(6*time.Minute))
	if len(gap.Values) != 2 || !math.IsNaN(float64(gap.Values[0])) {
		t.Errorf("missing minutes must be NaN, got %v", gap.Values)
	}
}

func TestMinuteStoreCoverageNeedsEightyPercentOfDay(t *testing.T) {
	env := newMsEnv(t, LocRAM)
	s := newMsStore(env)
	start := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	for m := 0; m < 720; m++ {
		s.ObserveMinute(Frame{
			Minute:  start.Add(time.Duration(m) * time.Minute),
			Balance: map[string]float64{"load_total": 10},
		})
	}
	if got := s.Coverage(start, start.Add(24*time.Hour)); got != 0 {
		t.Fatalf("half a day: Coverage = %d, want 0", got)
	}

	for m := 720; m < 1440; m++ {
		s.ObserveMinute(Frame{
			Minute:  start.Add(time.Duration(m) * time.Minute),
			Balance: map[string]float64{"load_total": 10},
		})
	}
	if got := s.Coverage(start, start.Add(24*time.Hour)); got != 1 {
		t.Fatalf("full day: Coverage = %d, want 1", got)
	}
}
