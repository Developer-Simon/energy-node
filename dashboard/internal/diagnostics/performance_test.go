package diagnostics

import (
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func overBudgetStatus(ok bool) PerformanceStatus {
	return PerformanceStatusFunc(func(time.Time) (OverBudget, bool) {
		return OverBudget{UsedPct: 88, AvailablePct: 60, TargetPct: 70, DeductionPct: 10, Minutes: 31, Reason: "performance.deduction.hot"}, ok
	})
}

func TestPerformanceWarningCarriesDetails(t *testing.T) {
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	engine := NewEngine(registry.New(), nil)
	engine.SetPerformance(overBudgetStatus(true), "node")

	warnings := engine.Device("node", now)
	var got *Warning
	for i := range warnings {
		if warnings[i].RuleID == "performance_over_budget" {
			got = &warnings[i]
		}
	}
	if got == nil {
		t.Fatalf("no performance_over_budget warning in %+v", warnings)
	}
	if got.Severity != SeverityWarning || got.DeviceID != "node" || !got.Operational {
		t.Fatalf("got %+v", got)
	}
	if got.Message != ruleTexts["performance_over_budget"].Message || got.Hint != ruleTexts["performance_over_budget"].Hint {
		t.Fatalf("texts not taken from ruleTexts: %+v", got)
	}
	want := map[string]any{
		"used_pct": 88.0, "available_pct": 60.0, "target_pct": 70.0, "deduction_pct": 10.0,
		"minutes": 31, "reason": "performance.deduction.hot", "settings_page": "performance",
	}
	for key, value := range want {
		if got.Details[key] != value {
			t.Errorf("Details[%q] = %v (%T), want %v", key, got.Details[key], got.Details[key], value)
		}
	}
}

func TestPerformanceWarningAbsentWithoutFinding(t *testing.T) {
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	engine := NewEngine(registry.New(), nil)
	engine.SetPerformance(overBudgetStatus(false), "node")
	for _, w := range engine.Evaluate(now) {
		if w.RuleID == "performance_over_budget" {
			t.Fatalf("warning without finding: %+v", w)
		}
	}
	engine.SetPerformance(nil, "")
	for _, w := range engine.Evaluate(now) {
		if w.RuleID == "performance_over_budget" {
			t.Fatalf("warning without status: %+v", w)
		}
	}
}

func TestPerformanceWarningInEvaluate(t *testing.T) {
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	engine := NewEngine(registry.New(), nil)
	engine.SetPerformance(overBudgetStatus(true), "node")
	found := false
	for _, w := range engine.Evaluate(now) {
		if w.RuleID == "performance_over_budget" && w.DeviceID == "node" && w.Details["settings_page"] == "performance" {
			found = true
		}
	}
	if !found {
		t.Fatal("Evaluate lacks the over-budget warning")
	}
}
