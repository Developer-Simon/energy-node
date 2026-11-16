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

// busySteps returns a source with busyPerTick busy and 100 total jiffies per Read on four cores.
// The CPU load in percent is therefore busyPerTick.
func busySteps(busyPerTick uint64) *scriptedSource {
	return &scriptedSource{step: func(i int) Raw {
		return Raw{CPUBusy: busyPerTick * uint64(i), CPUTotal: 100 * uint64(i), Cores: 4, Units: map[string]UnitRaw{}}
	}}
}

func TestTickEvaluatesRunningChange(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	const interval = 15 * time.Second
	cases := []struct {
		name     string
		busy     uint64
		want     string
		measured float64
	}{
		{"ok", 50, "ok", 50},
		{"bad", 80, "bad", 80},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			m := NewMonitor(busySteps(tc.busy), nil, Options{Interval: interval, Window: 6 * time.Hour})
			if err := m.AttachChange(dir, t0); err != nil {
				t.Fatalf("AttachChange: %v", err)
			}
			if err := m.RecordChange(Change{At: t0, Available: 70, State: "running"}); err != nil {
				t.Fatalf("RecordChange: %v", err)
			}

			// Ticks 0..80 are 20 min. The change is 15 min old at tick 60.
			for i := 0; i <= 80; i++ {
				now := t0.Add(time.Duration(i) * interval)
				m.Tick(now)
				if i == 59 {
					if c := m.CurrentChange(); c == nil || c.State != "running" {
						t.Fatalf("before 15 min: change = %+v, want running", c)
					}
				}
			}

			c := m.CurrentChange()
			if c == nil || c.State != tc.want {
				t.Fatalf("change = %+v, want state %s", c, tc.want)
			}
			if c.Measured == nil || math.Abs(*c.Measured-tc.measured) > 1e-9 {
				t.Fatalf("Measured = %v, want %v", c.Measured, tc.measured)
			}
			stored, err := LoadChange(dir)
			if err != nil || stored == nil || stored.State != tc.want {
				t.Fatalf("stored change = %+v, %v, want state %s", stored, err, tc.want)
			}
		})
	}
}

func TestAttachChangeMarksRestartedAndResetsAt(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	restart := t0.Add(time.Hour)

	t.Run("running change restarts its check", func(t *testing.T) {
		dir := t.TempDir()
		if err := SaveChange(dir, Change{At: t0, State: "running"}); err != nil {
			t.Fatalf("SaveChange: %v", err)
		}
		m := NewMonitor(busySteps(50), nil, Options{})
		if err := m.AttachChange(dir, restart); err != nil {
			t.Fatalf("AttachChange: %v", err)
		}
		c := m.CurrentChange()
		if c == nil || c.State != "running" || !c.Restarted || !c.At.Equal(restart) {
			t.Fatalf("change = %+v, want running, Restarted, At %v", c, restart)
		}
		stored, err := LoadChange(dir)
		if err != nil || stored == nil || !stored.Restarted || !stored.At.Equal(restart) {
			t.Fatalf("stored change = %+v, %v, want Restarted and At %v", stored, err, restart)
		}
	})

	t.Run("decided change is kept as loaded", func(t *testing.T) {
		dir := t.TempDir()
		if err := SaveChange(dir, Change{At: t0, State: "ok"}); err != nil {
			t.Fatalf("SaveChange: %v", err)
		}
		m := NewMonitor(busySteps(50), nil, Options{})
		if err := m.AttachChange(dir, restart); err != nil {
			t.Fatalf("AttachChange: %v", err)
		}
		c := m.CurrentChange()
		if c == nil || c.State != "ok" || c.Restarted || !c.At.Equal(t0) {
			t.Fatalf("change = %+v, want ok, not Restarted, At %v", c, t0)
		}
	})

	t.Run("missing file holds no change", func(t *testing.T) {
		m := NewMonitor(busySteps(50), nil, Options{})
		if err := m.AttachChange(t.TempDir(), restart); err != nil {
			t.Fatalf("AttachChange: %v", err)
		}
		if c := m.CurrentChange(); c != nil {
			t.Fatalf("change = %+v, want nil", c)
		}
	})
}

func TestCurrentChangeIsACopy(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	m := NewMonitor(busySteps(50), nil, Options{})
	if c := m.CurrentChange(); c != nil {
		t.Fatalf("CurrentChange without a change = %+v, want nil", c)
	}

	measured := 42.0
	if err := m.AttachChange(t.TempDir(), t0); err != nil {
		t.Fatalf("AttachChange: %v", err)
	}
	if err := m.RecordChange(Change{
		At:       t0,
		Before:   map[RowID]float64{RowShelly: 10},
		After:    map[RowID]float64{RowShelly: 5},
		State:    "running",
		Measured: &measured,
	}); err != nil {
		t.Fatalf("RecordChange: %v", err)
	}

	c := m.CurrentChange()
	c.Before[RowShelly] = 99
	c.After[RowTick] = 7
	*c.Measured = 1
	c.State = "undone"

	again := m.CurrentChange()
	if again.Before[RowShelly] != 10 {
		t.Errorf("Before = %v, want shelly 10", again.Before)
	}
	if _, ok := again.After[RowTick]; ok {
		t.Errorf("After = %v, want no tick row", again.After)
	}
	if again.Measured == nil || *again.Measured != 42 {
		t.Errorf("Measured = %v, want 42", again.Measured)
	}
	if again.State != "running" {
		t.Errorf("State = %q, want running", again.State)
	}
}

func TestMarkUndoneSurvivesReload(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	dir := t.TempDir()

	empty := NewMonitor(busySteps(50), nil, Options{})
	if err := empty.MarkUndone(); err != nil {
		t.Fatalf("MarkUndone without a change: %v", err)
	}

	first := NewMonitor(busySteps(50), nil, Options{})
	if err := first.AttachChange(dir, t0); err != nil {
		t.Fatalf("AttachChange: %v", err)
	}
	if err := first.RecordChange(Change{At: t0, State: "running"}); err != nil {
		t.Fatalf("RecordChange: %v", err)
	}
	if err := first.MarkUndone(); err != nil {
		t.Fatalf("MarkUndone: %v", err)
	}

	second := NewMonitor(busySteps(50), nil, Options{})
	if err := second.AttachChange(dir, t0.Add(time.Hour)); err != nil {
		t.Fatalf("AttachChange after reload: %v", err)
	}
	c := second.CurrentChange()
	if c == nil || c.State != "undone" || c.Restarted {
		t.Fatalf("change after reload = %+v, want undone and not Restarted", c)
	}
}

func TestMonitorWindowsDashboardShares(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	const interval = 15 * time.Second
	timing := NewTiming()
	var gc, user float64
	m := NewMonitor(steadySteps(nil), nil, Options{
		Interval:   interval,
		Window:     6 * time.Hour,
		Timing:     timing,
		RuntimeCPU: func() (float64, float64) { return gc, user },
	})

	// Baseline tick, then two kept ticks with push 3 s and mqtt 1 s in total.
	m.Tick(t0)
	timing.Add(PartPush, 1500*time.Millisecond)
	timing.Add(PartMQTT, 500*time.Millisecond)
	gc, user = 0.25, 0.75
	m.Tick(t0.Add(interval))
	timing.Add(PartPush, 1500*time.Millisecond)
	timing.Add(PartMQTT, 500*time.Millisecond)
	gc, user = 0.5, 1.5
	now := t0.Add(2 * interval)
	m.Tick(now)

	snap := m.Snapshot(now)
	if snap.Dashboard == nil {
		t.Fatal("Dashboard = nil, want a split")
	}
	if got := snap.Dashboard.Shares[PartPush]; math.Abs(got-0.75) > 1e-9 {
		t.Errorf("push share = %v, want 0.75", got)
	}
	if got := snap.Dashboard.Shares[PartMQTT]; math.Abs(got-0.25) > 1e-9 {
		t.Errorf("mqtt share = %v, want 0.25", got)
	}
	// gc 0.5 s against user 1.5 s in the window.
	if got := snap.Dashboard.GCShare; math.Abs(got-0.25) > 1e-9 {
		t.Errorf("GCShare = %v, want 0.25", got)
	}
}

func TestMonitorWithoutTimingHasNoSplit(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	const interval = 15 * time.Second
	var calls float64
	m := NewMonitor(steadySteps(nil), nil, Options{
		Interval:   interval,
		Window:     6 * time.Hour,
		RuntimeCPU: func() (float64, float64) { calls++; return calls, calls },
	})

	var now time.Time
	for i := 0; i <= 10; i++ {
		now = t0.Add(time.Duration(i) * interval)
		m.Tick(now)
	}
	if snap := m.Snapshot(now); snap.Dashboard != nil {
		t.Errorf("Dashboard = %+v, want nil without timing", snap.Dashboard)
	}
}

func TestMonitorDiscardedTickAdvancesBaseline(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	const interval = 15 * time.Second
	timing := NewTiming()
	busy := false
	m := NewMonitor(steadySteps(nil), nil, Options{
		Interval: interval,
		Window:   6 * time.Hour,
		Timing:   timing,
		Busy:     func() bool { return busy },
	})

	m.Tick(t0)
	timing.Add(PartPush, time.Second)
	m.Tick(t0.Add(interval)) // kept

	// Busy tick: mqtt work happens here but the tick is discarded.
	busy = true
	timing.Add(PartMQTT, 3*time.Second)
	m.Tick(t0.Add(2 * interval))
	busy = false

	timing.Add(PartPush, time.Second)
	now := t0.Add(3 * interval)
	m.Tick(now) // kept, its delta is push 1 s only

	snap := m.Snapshot(now)
	if snap.Dashboard == nil {
		t.Fatal("Dashboard = nil, want a split")
	}
	if got := snap.Dashboard.Shares[PartPush]; math.Abs(got-1) > 1e-9 {
		t.Errorf("push share = %v, want 1", got)
	}
	if got := snap.Dashboard.Shares[PartMQTT]; got != 0 {
		t.Errorf("mqtt share = %v, want 0: the discarded tick must not count", got)
	}
}

func TestMonitorEmptyTimingHasNoSplit(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	const interval = 15 * time.Second
	m := NewMonitor(steadySteps(nil), nil, Options{
		Interval: interval,
		Window:   6 * time.Hour,
		Timing:   NewTiming(),
	})

	var now time.Time
	for i := 0; i <= 5; i++ {
		now = t0.Add(time.Duration(i) * interval)
		m.Tick(now)
	}
	if snap := m.Snapshot(now); snap.Dashboard != nil {
		t.Errorf("Dashboard = %+v, want nil when every part is 0", snap.Dashboard)
	}
}

func TestMonitorZeroRuntimeCPUGivesZeroGCShare(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	const interval = 15 * time.Second
	timing := NewTiming()
	m := NewMonitor(steadySteps(nil), nil, Options{
		Interval:   interval,
		Window:     6 * time.Hour,
		Timing:     timing,
		RuntimeCPU: func() (float64, float64) { return 0, 0 },
	})

	m.Tick(t0)
	timing.Add(PartPush, time.Second)
	now := t0.Add(interval)
	m.Tick(now)

	snap := m.Snapshot(now)
	if snap.Dashboard == nil {
		t.Fatal("Dashboard = nil, want a split")
	}
	if math.IsNaN(snap.Dashboard.GCShare) || snap.Dashboard.GCShare != 0 {
		t.Errorf("GCShare = %v, want 0 without runtime CPU", snap.Dashboard.GCShare)
	}
}
