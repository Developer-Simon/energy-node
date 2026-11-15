package performance

import (
	"math"
	"testing"
	"time"
)

// constantLoad reports pct percent total CPU on four cores at every Read.
func constantLoad(pct uint64) *scriptedSource {
	return &scriptedSource{step: func(i int) Raw {
		return Raw{CPUBusy: pct * uint64(i), CPUTotal: 100 * uint64(i), Cores: 4, Units: map[string]UnitRaw{}}
	}}
}

func TestOverBudgetAfterThirtyMinutes(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	const interval = 15 * time.Second
	m := NewMonitor(constantLoad(90), nil, Options{Interval: interval, Window: 6 * time.Hour, Warmup: 5 * time.Minute})
	m.SetBudget(func(Snapshot) Budget {
		return Budget{Target: 70, Deduction: Deduction{Pct: 10, Reasons: []string{"performance.deduction.hot"}}}
	})

	// Ticks 0..19 fall into the warmup, kept samples start at tick 20. The
	// 30 minutes are 120 kept readings, ticks 20..139.
	first := -1
	var at time.Time
	for i := 0; i <= 200; i++ {
		at = t0.Add(time.Duration(i) * interval)
		m.Tick(at)
		if _, ok := m.OverBudget(at); ok && first < 0 {
			first = i
		}
	}
	if first != 139 {
		t.Fatalf("finding first stands at tick %d, want 139", first)
	}

	info, ok := m.OverBudget(at)
	if !ok {
		t.Fatal("finding gone at the end")
	}
	if info.Minutes != 30+int((200-139)*15/60) {
		t.Errorf("Minutes = %d, want %d", info.Minutes, 30+int((200-139)*15/60))
	}
	if math.Abs(info.UsedPct-90) > 1e-9 || math.Abs(info.AvailablePct-60) > 1e-9 {
		t.Errorf("used/available = %v/%v, want 90/60", info.UsedPct, info.AvailablePct)
	}
	if info.TargetPct != 70 || info.DeductionPct != 10 {
		t.Errorf("target/deduction = %v/%v, want 70/10", info.TargetPct, info.DeductionPct)
	}
	if info.Reason != "performance.deduction.hot" {
		t.Errorf("Reason = %q", info.Reason)
	}
}

func TestOverBudgetIgnoresBusy(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	const interval = 15 * time.Second
	tick := 0
	m := NewMonitor(constantLoad(90), nil, Options{
		Interval: interval,
		Window:   6 * time.Hour,
		Warmup:   5 * time.Minute,
		Busy:     func() bool { return tick >= 120 && tick < 360 },
	})
	m.SetBudget(func(Snapshot) Budget { return Budget{Target: 70} })

	// 25 minutes over (ticks 20..119), 60 minutes busy (120..359), then over
	// again from 360. Only the 25 + 20 kept minutes of over count, so the finding
	// stands at tick 379 (120 readings), not earlier.
	first := -1
	for i := 0; i <= 400; i++ {
		tick = i
		at := t0.Add(time.Duration(i) * interval)
		m.Tick(at)
		if _, ok := m.OverBudget(at); ok && first < 0 {
			first = i
		}
	}
	if first != 379 {
		t.Fatalf("finding first stands at tick %d, want 379", first)
	}
}

func TestOverBudgetClearsWithHysteresis(t *testing.T) {
	const step = 15 * time.Second
	var tr OverBudgetTracker
	budget := Budget{Target: 70}
	feed := func(used float64, readings int) {
		for i := 0; i < readings; i++ {
			tr.Feed(used, budget, step)
		}
	}
	stands := func() bool {
		_, ok := tr.Info()
		return ok
	}

	feed(90, 120)
	if !stands() {
		t.Fatal("no finding after 30 min over the budget")
	}

	// Between available-5 and available: keeps the finding, however long.
	feed(68, 240)
	if !stands() {
		t.Fatal("a reading between the band edges cleared the finding")
	}

	// 9 min under, then a reading in between resets the under timer.
	feed(60, 36)
	feed(68, 1)
	feed(60, 36)
	if !stands() {
		t.Fatal("the under timer was not reset by the reading in between")
	}

	// 10 min continuously under available-5 clears it.
	feed(60, 4)
	if stands() {
		t.Fatal("finding still stands after 10 min under available-5")
	}
}
