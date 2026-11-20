package autolearn

import (
	"encoding/json"
	"fmt"
	"testing"
)

// gateCand is a habit candidate with a benefit in kWh per week that passes
// the minimum data and the benefit gate.
func gateCand(id, detector string, kwh, lower, limit float64, halves bool) Candidate {
	return Candidate{
		Finding: Finding{ID: id, Kind: "test", Group: "habit", Severity: "info",
			RuleIDs: []string{"r-" + id}},
		Detector:    detector,
		NEff:        30,
		MinNEff:     10,
		Lower:       lower,
		Limit:       limit,
		HalvesAgree: halves,
		Benefit:     Benefit{Unit: "kwh_week", Value: kwh},
	}
}

func gateIDs(out []Finding) []string {
	ids := make([]string, 0, len(out))
	for _, f := range out {
		ids = append(ids, f.ID)
	}
	return ids
}

func TestGateKeepsBorderlineCandidateAsHigh(t *testing.T) {
	out, shown := Gate([]Candidate{gateCand("a", "x", 1, 2, 2, true)}, Doc{}, nil)
	if len(out) != 1 {
		t.Fatalf("got %d findings, want 1", len(out))
	}
	if out[0].Confidence != "high" {
		t.Errorf("Confidence = %q, want high", out[0].Confidence)
	}
	if _, ok := shown["a"]; !ok {
		t.Errorf("shown has no entry for a: %+v", shown)
	}
}

func TestGateDropsCandidateBelowLimitWithoutPrevious(t *testing.T) {
	out, _ := Gate([]Candidate{gateCand("a", "x", 1, 1.9, 2, true)}, Doc{}, nil)
	if len(out) != 0 {
		t.Errorf("got %v, want none", gateIDs(out))
	}
}

func TestGateKeepsShownCandidateAboveSeventyFivePercent(t *testing.T) {
	prev := map[string]Shown{"a": {Lower: 2}}

	out, _ := Gate([]Candidate{gateCand("a", "x", 1, 1.6, 2, false)}, Doc{}, prev)
	if len(out) != 1 {
		t.Fatalf("80 %%: got %v, want the candidate to stay", gateIDs(out))
	}
	if out[0].Confidence != "estimate" {
		t.Errorf("80 %%: Confidence = %q, want estimate", out[0].Confidence)
	}

	out, _ = Gate([]Candidate{gateCand("a", "x", 1, 1.4, 2, false)}, Doc{}, prev)
	if len(out) != 0 {
		t.Errorf("70 %%: got %v, want none", gateIDs(out))
	}
}

func TestGateQuietValuesChangeOnlyBeyondSpread(t *testing.T) {
	cand := gateCand("a", "x", 1, 2, 2, true)
	cand.Finding.Group = "habit"
	cand.Spread = map[string]float64{"time": 8}

	prev := map[string]Shown{"a": {Lower: 2, Values: map[string]float64{"time": 1350}, Since: 5}}

	cand.Values = map[string]float64{"time": 1355}
	_, shown := Gate([]Candidate{cand}, Doc{}, prev)
	if got := shown["a"].Values["time"]; got != 1350 {
		t.Errorf("22:35 within spread: shown time = %v, want 1350 (22:30)", got)
	}
	if shown["a"].Since != 5 {
		t.Errorf("Since = %d, want 5 kept", shown["a"].Since)
	}

	cand.Values = map[string]float64{"time": 1370}
	_, shown = Gate([]Candidate{cand}, Doc{}, prev)
	if got := shown["a"].Values["time"]; got != 1370 {
		t.Errorf("22:50 beyond spread: shown time = %v, want 1370", got)
	}
}

func TestGateDropsDuplicateOfExistingRule(t *testing.T) {
	prefillRules["test_tpl"] = func(map[string]any) (Rule, bool) {
		return Rule{Name: "neu", Conditions: []Condition{gateThreshold()},
			Actions: []Action{{Type: "publish", Topic: "t/x", Payload: "1"}}}, true
	}
	t.Cleanup(func() { delete(prefillRules, "test_tpl") })

	doc := Doc{Rules: []Rule{{ID: "old", Name: "alt", Enabled: true,
		Conditions: []Condition{gateThreshold()},
		Actions:    []Action{{Type: "publish", Topic: "t/x", Payload: "1"}}}}}

	cand := gateCand("a", "x", 1, 2, 2, true)
	cand.Finding.Apply = []Fix{{Code: "fix", Template: "test_tpl"}}
	if out, _ := Gate([]Candidate{cand}, doc, nil); len(out) != 0 {
		t.Errorf("duplicate kept: %v", gateIDs(out))
	}

	// Without a rule to build from, the candidate is kept.
	cand.Finding.Apply = []Fix{{Code: "fix", Template: "unknown_tpl"}}
	if out, _ := Gate([]Candidate{cand}, doc, nil); len(out) != 1 {
		t.Errorf("unbuildable template dropped: %v", gateIDs(out))
	}
}

func TestGateOrderLimitsTwoPerDetectorInTopThree(t *testing.T) {
	cands := []Candidate{
		gateCand("a1", "a", 9, 2, 2, true),
		gateCand("a2", "a", 8, 2, 2, true),
		gateCand("a3", "a", 7, 2, 2, true),
		gateCand("b1", "b", 1, 2, 2, true),
	}
	out, _ := Gate(cands, Doc{}, nil)
	if len(out) != 4 {
		t.Fatalf("got %v, want 4", gateIDs(out))
	}
	top := map[string]bool{}
	for _, f := range out[:3] {
		top[f.ID] = true
	}
	if top["a3"] {
		t.Errorf("top three %v has three of detector a", gateIDs(out[:3]))
	}
	if out[2].ID != "b1" {
		t.Errorf("third = %q, want b1", out[2].ID)
	}
}

func TestGateCapsAtTwentyAndPutsBadFirst(t *testing.T) {
	var cands []Candidate
	for i := 0; i < 25; i++ {
		cands = append(cands, gateCand(fmt.Sprintf("c%02d", i), fmt.Sprintf("d%02d", i), 1, 2, 2, true))
	}
	bad := Candidate{Finding: Finding{ID: "check:bad", Group: "check", Severity: "bad",
		Confidence: "high", RuleIDs: []string{"r"}}, Detector: "check", NEff: 30, MinNEff: 10}
	cands = append(cands, bad)

	out, _ := Gate(cands, Doc{}, nil)
	if len(out) != 20 {
		t.Fatalf("got %d findings, want 20", len(out))
	}
	if out[0].ID != "check:bad" {
		t.Errorf("first = %q, want the bad finding", out[0].ID)
	}
}

func TestGateCheckGroupOnlyNeedsMinimumData(t *testing.T) {
	low := Candidate{Finding: Finding{ID: "check:low", Group: "check", Severity: "warn",
		Confidence: "high", RuleIDs: []string{"r"}}, Detector: "check", NEff: 2, MinNEff: 10}
	ok := Candidate{Finding: Finding{ID: "check:ok", Group: "check", Severity: "warn",
		Confidence: "high", RuleIDs: []string{"r"}}, Detector: "check", NEff: 30, MinNEff: 10,
		HalvesAgree: false}

	out, _ := Gate([]Candidate{low, ok}, Doc{}, nil)
	if got := gateIDs(out); len(got) != 1 || got[0] != "check:ok" {
		t.Fatalf("got %v, want [check:ok]", got)
	}
	if out[0].Confidence != "high" {
		t.Errorf("check Confidence = %q, want high kept", out[0].Confidence)
	}
}

func TestGateRuntimePresetHighWithoutLimitStaysHigh(t *testing.T) {
	cand := gateCand("runtime:heater", "runtime", 1, 0, 0, false)
	cand.Finding.Confidence = "high"
	out, _ := Gate([]Candidate{cand}, Doc{}, nil)
	if len(out) != 1 || out[0].Confidence != "high" {
		t.Fatalf("got %+v, want one high finding", out)
	}
}

func TestGateEstimateRules(t *testing.T) {
	onlyLower := gateCand("a", "x", 1, 2, 2, false)
	out, _ := Gate([]Candidate{onlyLower}, Doc{}, nil)
	if len(out) != 1 || out[0].Confidence != "estimate" {
		t.Errorf("Lower >= Limit without halves: got %+v, want estimate", out)
	}

	preset := gateCand("b", "x", 1, 1, 2, false)
	preset.Finding.Confidence = "estimate"
	out, _ = Gate([]Candidate{preset}, Doc{}, nil)
	if len(out) != 1 || out[0].Confidence != "estimate" {
		t.Errorf("preset estimate: got %+v, want estimate", out)
	}
}

func TestGateDropsBelowBenefitMinimum(t *testing.T) {
	small := gateCand("a", "x", 0.4, 2, 2, true)
	out, _ := Gate([]Candidate{small}, Doc{}, nil)
	if len(out) != 0 {
		t.Errorf("0.4 kWh/week kept: %v", gateIDs(out))
	}
}

// gateThreshold is the condition shared by the duplicate test rules.
func gateThreshold() Condition {
	return Condition{Type: "balance_threshold", Threshold: 500,
		Raw: json.RawMessage(`{"type":"balance_threshold","threshold":500}`)}
}
