package performance

import "time"

// The over-budget check compares the P95 of the last five minutes with the
// available budget. Hysteresis keeps one reading from raising or clearing the
// finding.
const (
	overBudgetWindow     = 5 * time.Minute
	overBudgetRaiseAfter = 30 * time.Minute
	overBudgetClearAfter = 10 * time.Minute
	// overBudgetMargin is the distance below the available budget that counts
	// as under. Readings between the margin and the budget keep the state.
	overBudgetMargin = 5.0
)

// Budget is the CPU budget at one point in time. The available budget is the
// target minus the deduction.
type Budget struct {
	Target    float64
	Deduction Deduction
}

// OverBudgetInfo describes the running over-budget finding.
type OverBudgetInfo struct {
	UsedPct      float64
	AvailablePct float64
	TargetPct    float64
	DeductionPct float64
	Minutes      int    // continuous minutes over the budget so far
	Reason       string // first deduction reason key, "" without deduction
}

// OverBudgetTracker turns the readings into the finding. The caller feeds it
// only readings that are kept, so warmup and busy time count neither way.
type OverBudgetTracker struct {
	over, under time.Duration // continuous time over; under time while flagged
	flagged     bool
	last        OverBudgetInfo
}

// Feed counts one reading of used percent that covers step.
//
// Over (used above available) adds to the over timer and resets the under
// timer. Under (used below available minus 5) resets the over timer while no
// finding stands; while one stands, it adds to the under timer instead. A
// reading in between keeps the state and resets the under timer.
func (t *OverBudgetTracker) Feed(used float64, b Budget, step time.Duration) {
	available := b.Target - b.Deduction.Pct
	t.last = OverBudgetInfo{
		UsedPct:      used,
		AvailablePct: available,
		TargetPct:    b.Target,
		DeductionPct: b.Deduction.Pct,
	}
	if len(b.Deduction.Reasons) > 0 {
		t.last.Reason = b.Deduction.Reasons[0]
	}

	switch {
	case used > available:
		t.under = 0
		t.over += step
	case used < available-overBudgetMargin:
		if t.flagged {
			t.under += step
		} else {
			t.over = 0
		}
	default:
		t.under = 0
	}

	if !t.flagged && t.over >= overBudgetRaiseAfter {
		t.flagged = true
		t.under = 0
	}
	if t.flagged && t.under >= overBudgetClearAfter {
		t.flagged = false
		t.over, t.under = 0, 0
	}
}

// Info returns the finding and whether it stands.
func (t *OverBudgetTracker) Info() (OverBudgetInfo, bool) {
	if !t.flagged {
		return OverBudgetInfo{}, false
	}
	info := t.last
	info.Minutes = int(t.over / time.Minute)
	return info, true
}
