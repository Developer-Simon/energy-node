package diagnostics

import "time"

// PerformanceStatus reports whether the node runs over its CPU budget. main
// adapts the performance Monitor to it.
type PerformanceStatus interface {
	OverBudget(now time.Time) (OverBudget, bool)
}

// OverBudget is the over-budget finding as the diagnostics see it.
type OverBudget struct {
	UsedPct      float64
	AvailablePct float64
	TargetPct    float64
	DeductionPct float64
	Minutes      int
	Reason       string
}

// PerformanceStatusFunc adapts a function to PerformanceStatus.
type PerformanceStatusFunc func(now time.Time) (OverBudget, bool)

// OverBudget calls f.
func (f PerformanceStatusFunc) OverBudget(now time.Time) (OverBudget, bool) {
	return f(now)
}
