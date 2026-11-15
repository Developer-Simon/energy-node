package performance

import (
	"math"
	"testing"
	"time"
)

// scriptedSource returns step(i) for the i-th Read.
type scriptedSource struct {
	step  func(i int) Raw
	calls int
}

func (s *scriptedSource) Read(_ []string) (Raw, error) {
	r := s.step(s.calls)
	s.calls++
	return r, nil
}

// steadySteps returns a source with 50 % total CPU on four cores, one step per Read.
func steadySteps(units func(i int) map[string]UnitRaw) *scriptedSource {
	return &scriptedSource{step: func(i int) Raw {
		raw := Raw{CPUBusy: uint64(50 * i), CPUTotal: uint64(100 * i), Cores: 4, Units: map[string]UnitRaw{}}
		if units != nil {
			raw.Units = units(i)
		}
		return raw
	}}
}

func TestMonitorSkipsWarmupAndBusy(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	const interval = 15 * time.Second
	busy := false
	m := NewMonitor(steadySteps(nil), nil, Options{
		Interval: interval,
		Window:   6 * time.Hour,
		Warmup:   5 * time.Minute,
		Busy:     func() bool { return busy },
	})

	var now time.Time
	for i := 0; i <= 40; i++ {
		now = t0.Add(time.Duration(i) * interval)
		busy = i >= 30 && i <= 33
		m.Tick(now)
	}

	snap := m.Snapshot(now)
	// Ticks 1..40 are 15 s apart. Warmup drops ticks 1..19 (before 5 min), busy drops 30..33.
	// That leaves 17 kept samples.
	if want := 17 * interval; snap.MeasuredFor != want {
		t.Errorf("MeasuredFor = %v, want %v", snap.MeasuredFor, want)
	}
	if math.Abs(snap.CPUP95-50) > 1e-9 {
		t.Errorf("CPUP95 = %v, want 50", snap.CPUP95)
	}
}

func TestMonitorDropsCounterReset(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	const interval = 15 * time.Second
	usec := []uint64{5000, 100, 100 + 1_500_000}
	src := steadySteps(func(i int) map[string]UnitRaw {
		return map[string]UnitRaw{"shelly-rpc.service": {CPUUsec: usec[i]}}
	})
	m := NewMonitor(src, nil, Options{
		Interval: interval,
		Window:   6 * time.Hour,
		Units:    map[string]string{"shelly": "shelly-rpc.service"},
	})

	var now time.Time
	for i := range usec {
		now = t0.Add(time.Duration(i) * interval)
		m.Tick(now)
	}

	// Only the step from 100 to 1.6 s of CPU counts: 1.5 s over 15 s on 4 cores = 2.5 %.
	ring := m.unitCPU["shelly-rpc.service"]
	if ring == nil {
		t.Fatal("no unit ring for shelly-rpc.service")
	}
	values := ring.Values()
	if len(values) != 1 {
		t.Fatalf("unit samples = %v, want exactly one", values)
	}
	if math.Abs(values[0]-2.5) > 1e-9 {
		t.Errorf("unit sample = %v, want 2.5", values[0])
	}

	snap := m.Snapshot(now)
	if len(snap.Units) != 1 || snap.Units[0].ID != "shelly" {
		t.Fatalf("Units = %+v, want one unit shelly", snap.Units)
	}
	if math.Abs(snap.Units[0].CPUP95-2.5) > 1e-9 {
		t.Errorf("unit CPUP95 = %v, want 2.5", snap.Units[0].CPUP95)
	}
}

func TestMonitorWindowIsSixHours(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	const interval = 15 * time.Second
	m := NewMonitor(steadySteps(nil), nil, Options{
		Interval: interval,
		Window:   6 * time.Hour,
	})

	// Ticks at 0 s, 15 s, ..., 7 h. The baseline is the tick at 0 s.
	var now time.Time
	for i := 0; i <= 7*3600/15; i++ {
		now = t0.Add(time.Duration(i) * interval)
		m.Tick(now)
	}

	// The window ends at now and reaches back 6 h. Both ends are on the 15 s
	// grid, so the oldest kept sample is exactly 6 h old: 6 h / 15 s + 1 samples.
	const wantSamples = 6*3600/15 + 1
	cutoff := now.Add(-6 * time.Hour)
	if got := len(m.SamplesSince(time.Time{})); got != wantSamples {
		t.Errorf("samples in window = %d, want %d", got, wantSamples)
	}
	if got := len(m.SamplesSince(cutoff)); got != wantSamples {
		t.Errorf("samples at or after 6 h = %d, want %d", got, wantSamples)
	}
	if got := len(m.SamplesSince(cutoff.Add(time.Nanosecond))); got != wantSamples-1 {
		t.Errorf("samples after 6 h = %d, want %d (oldest must be exactly 6 h old)", got, wantSamples-1)
	}
	if snap := m.Snapshot(now); snap.MeasuredFor != wantSamples*interval {
		t.Errorf("MeasuredFor = %v, want %v", snap.MeasuredFor, wantSamples*interval)
	}
}

func TestMonitorFillsRoundSFromCallback(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	const interval = 15 * time.Second
	src := steadySteps(func(i int) map[string]UnitRaw {
		return map[string]UnitRaw{
			"shelly-rpc.service": {CPUUsec: uint64(i) * 1_000_000},
			"trucki.service":     {CPUUsec: uint64(i) * 1_000_000},
		}
	})
	m := NewMonitor(src, nil, Options{
		Interval: interval,
		Window:   6 * time.Hour,
		Units:    map[string]string{"shelly": "shelly-rpc.service", "trucki": "trucki.service"},
		RoundS: func(id string) (float64, bool) {
			if id == "shelly" {
				return 0.4, true
			}
			return 0, false
		},
	})
	var now time.Time
	for i := 0; i < 3; i++ {
		now = t0.Add(time.Duration(i) * interval)
		m.Tick(now)
	}

	byID := map[string]UnitStats{}
	for _, u := range m.Snapshot(now).Units {
		byID[u.ID] = u
	}
	if u := byID["shelly"]; u.RoundS == nil || *u.RoundS != 0.4 {
		t.Errorf("shelly RoundS = %v, want 0.4", u.RoundS)
	}
	if u := byID["trucki"]; u.RoundS != nil {
		t.Errorf("trucki RoundS = %v, want nil when the callback returns false", *u.RoundS)
	}

	none := NewMonitor(src, nil, Options{Interval: interval, Units: map[string]string{"shelly": "shelly-rpc.service"}})
	none.Tick(t0)
	none.Tick(t0.Add(interval))
	for _, u := range none.Snapshot(t0.Add(interval)).Units {
		if u.RoundS != nil {
			t.Errorf("%s RoundS = %v, want nil without a callback", u.ID, *u.RoundS)
		}
	}
}
