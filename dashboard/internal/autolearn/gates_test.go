package autolearn

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"
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
	out, shown := Gate([]Candidate{gateCand("a", "x", 1, 2, 2, true)}, Doc{}, nil, GateOptions{})
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
	out, _ := Gate([]Candidate{gateCand("a", "x", 1, 1.9, 2, true)}, Doc{}, nil, GateOptions{})
	if len(out) != 0 {
		t.Errorf("got %v, want none", gateIDs(out))
	}
}

func TestGateKeepsShownCandidateAboveSeventyFivePercent(t *testing.T) {
	prev := map[string]Shown{"a": {Lower: 2}}

	out, _ := Gate([]Candidate{gateCand("a", "x", 1, 1.6, 2, false)}, Doc{}, prev, GateOptions{})
	if len(out) != 1 {
		t.Fatalf("80 %%: got %v, want the candidate to stay", gateIDs(out))
	}
	if out[0].Confidence != "estimate" {
		t.Errorf("80 %%: Confidence = %q, want estimate", out[0].Confidence)
	}

	out, _ = Gate([]Candidate{gateCand("a", "x", 1, 1.4, 2, false)}, Doc{}, prev, GateOptions{})
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
	_, shown := Gate([]Candidate{cand}, Doc{}, prev, GateOptions{})
	if got := shown["a"].Values["time"]; got != 1350 {
		t.Errorf("22:35 within spread: shown time = %v, want 1350 (22:30)", got)
	}
	if shown["a"].Since != 5 {
		t.Errorf("Since = %d, want 5 kept", shown["a"].Since)
	}

	cand.Values = map[string]float64{"time": 1370}
	_, shown = Gate([]Candidate{cand}, Doc{}, prev, GateOptions{})
	if got := shown["a"].Values["time"]; got != 1370 {
		t.Errorf("22:50 beyond spread: shown time = %v, want 1370", got)
	}
}

// gateCoverDoc is a rule that switches lamp off at 22:30 by a time window.
func gateCoverDoc(entity string, on bool) (Doc, func(string) []Target) {
	off := !on
	rule := Rule{ID: "lamp-night", Name: "Lampe", Enabled: true,
		Conditions: []Condition{{Type: "time_window", Raw: json.RawMessage(`{"type":"time_window","start":"22:30","end":"22:31"}`)}},
		Actions:    []Action{{Type: "publish", EntityID: entity, Payload: "x"}}}
	doc := Doc{Rules: []Rule{rule}}
	targets := func(id string) []Target {
		if id != rule.ID {
			return nil
		}
		return []Target{{Entity: entity, On: &off}}
	}
	return doc, targets
}

// gateTimeCand is a time_point suggestion for entity at 22:30.
func gateTimeCand(id, entity string, on bool) Candidate {
	cand := gateCand(id, "switch_time", 1, 2, 2, true)
	cand.Finding.Apply = []Fix{{Code: "time_point", Template: "time_point",
		Prefill: map[string]any{"entity": entity, "on": on, "time": "22:30"}}}
	return cand
}

func TestGateDropsCoveredSuggestion(t *testing.T) {
	doc, targets := gateCoverDoc("switch.lamp", false)
	cand := gateTimeCand("a", "switch.lamp", false)
	if out, _ := Gate([]Candidate{cand}, doc, nil, GateOptions{Targets: targets}); len(out) != 0 {
		t.Fatalf("kept %v, want the covered suggestion dropped", gateIDs(out))
	}
}

func TestGateDropsContradiction(t *testing.T) {
	doc, targets := gateCoverDoc("switch.lamp", true)
	cand := gateTimeCand("a", "switch.lamp", false)
	if out, _ := Gate([]Candidate{cand}, doc, nil, GateOptions{Targets: targets}); len(out) != 0 {
		t.Fatalf("kept %v, want the contradicting suggestion dropped", gateIDs(out))
	}
}

func TestGateKeepsOtherEntity(t *testing.T) {
	doc, targets := gateCoverDoc("switch.lamp", false)
	cand := gateTimeCand("a", "switch.other", false)
	if out, _ := Gate([]Candidate{cand}, doc, nil, GateOptions{Targets: targets}); len(out) != 1 {
		t.Fatalf("kept %v, want one suggestion for another entity", gateIDs(out))
	}
}

func TestGateKeepsOtherTrigger(t *testing.T) {
	doc, targets := gateCoverDoc("switch.lamp", false)
	cand := gateCand("a", "runtime", 1, 2, 2, true)
	cand.Finding.Apply = []Fix{{Code: "runtime_off", Template: "runtime_off",
		Prefill: map[string]any{"entity": "switch.lamp"}}}
	if out, _ := Gate([]Candidate{cand}, doc, nil, GateOptions{Targets: targets}); len(out) != 1 {
		t.Fatalf("kept %v, want one suggestion with another trigger", gateIDs(out))
	}
}

func TestGateDisabledRuleCoversNothing(t *testing.T) {
	doc, targets := gateCoverDoc("switch.lamp", false)
	doc.Rules[0].Enabled = false
	cand := gateTimeCand("a", "switch.lamp", false)
	if out, _ := Gate([]Candidate{cand}, doc, nil, GateOptions{Targets: targets}); len(out) != 1 {
		t.Fatalf("kept %v, want one suggestion, the rule is off", gateIDs(out))
	}
}

func TestGateDropsUnreplayedEnergy(t *testing.T) {
	verdict := &ReplayVerdict{}
	for _, det := range []string{"surplus_window", "load_shift", "threshold_rating"} {
		if out, _ := Gate([]Candidate{gateCand("e", det, 1, 2, 2, true)}, Doc{}, nil, GateOptions{}); len(out) != 0 {
			t.Errorf("%s without replay kept", det)
		}
		c := gateCand("e", det, 1, 2, 2, true)
		c.Replay = verdict
		if out, _ := Gate([]Candidate{c}, Doc{}, nil, GateOptions{}); len(out) != 1 {
			t.Errorf("%s with replay verdict dropped", det)
		}
	}

	battery := gateCand("b", "battery", 1, 2, 2, true)
	battery.Finding.Apply = []Fix{{Code: "after_battery_full", Template: "after_battery_full",
		Prefill: map[string]any{"entity": "switch.boiler"}}}
	if out, _ := Gate([]Candidate{battery}, Doc{}, nil, GateOptions{}); len(out) != 0 {
		t.Errorf("after_battery_full without replay kept")
	}
	battery.Replay = verdict
	if out, _ := Gate([]Candidate{battery}, Doc{}, nil, GateOptions{}); len(out) != 1 {
		t.Errorf("after_battery_full with replay verdict dropped")
	}
}

func TestGateOrderLimitsTwoPerDetectorInTopThree(t *testing.T) {
	cands := []Candidate{
		gateCand("a1", "a", 9, 2, 2, true),
		gateCand("a2", "a", 8, 2, 2, true),
		gateCand("a3", "a", 7, 2, 2, true),
		gateCand("b1", "b", 1, 2, 2, true),
	}
	out, _ := Gate(cands, Doc{}, nil, GateOptions{})
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

	out, _ := Gate(cands, Doc{}, nil, GateOptions{})
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

	out, _ := Gate([]Candidate{low, ok}, Doc{}, nil, GateOptions{})
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
	out, _ := Gate([]Candidate{cand}, Doc{}, nil, GateOptions{})
	if len(out) != 1 || out[0].Confidence != "high" {
		t.Fatalf("got %+v, want one high finding", out)
	}
}

func TestGateEstimateRules(t *testing.T) {
	onlyLower := gateCand("a", "x", 1, 2, 2, false)
	out, _ := Gate([]Candidate{onlyLower}, Doc{}, nil, GateOptions{})
	if len(out) != 1 || out[0].Confidence != "estimate" {
		t.Errorf("Lower >= Limit without halves: got %+v, want estimate", out)
	}

	preset := gateCand("b", "x", 1, 1, 2, false)
	preset.Finding.Confidence = "estimate"
	out, _ = Gate([]Candidate{preset}, Doc{}, nil, GateOptions{})
	if len(out) != 1 || out[0].Confidence != "estimate" {
		t.Errorf("preset estimate: got %+v, want estimate", out)
	}
}

func TestGateDropsBelowBenefitMinimum(t *testing.T) {
	small := gateCand("a", "x", 0.4, 2, 2, true)
	out, _ := Gate([]Candidate{small}, Doc{}, nil, GateOptions{})
	if len(out) != 0 {
		t.Errorf("0.4 kWh/week kept: %v", gateIDs(out))
	}
}

// gateThreshold is the condition shared by the duplicate test rules.
func gateThreshold() Condition {
	return Condition{Type: "balance_threshold", Threshold: 500,
		Raw: json.RawMessage(`{"type":"balance_threshold","threshold":500}`)}
}

// gateClockCand is a switch time candidate whose summary and prefill show 22:35.
func gateClockCand(minute float64) Candidate {
	cand := gateCand("switch_time:lamp:off:0:2230", "switch_time", 1, 2, 2, true)
	cand.Finding.Summary = Text{Code: "switch_time.off", Params: map[string]any{"time": "22:35"}}
	cand.Finding.Apply = []Fix{{Code: "time_point", Template: "time_point",
		Prefill: map[string]any{"device": "lamp", "time": "22:35"}}}
	cand.Values = map[string]float64{"time": minute}
	cand.Spread = map[string]float64{"time": 8}
	return cand
}

func TestGateShowsStabilisedTime(t *testing.T) {
	prev := map[string]Shown{"switch_time:lamp:off:0:2230": {Lower: 2, Values: map[string]float64{"time": 1350}}}

	out, _ := Gate([]Candidate{gateClockCand(1355)}, Doc{}, prev, GateOptions{})
	if len(out) != 1 {
		t.Fatalf("got %v, want one finding", gateIDs(out))
	}
	if got := out[0].Summary.Params["time"]; got != "22:30" {
		t.Errorf("summary time = %v, want 22:30", got)
	}
	if got := out[0].Apply[0].Prefill["time"]; got != "22:30" {
		t.Errorf("prefill time = %v, want 22:30", got)
	}
	if out[0].ID != "switch_time:lamp:off:0:2230" {
		t.Errorf("ID = %q, want unchanged", out[0].ID)
	}

	out, _ = Gate([]Candidate{gateClockCand(1370)}, Doc{}, prev, GateOptions{})
	if len(out) != 1 {
		t.Fatalf("got %v, want one finding", gateIDs(out))
	}
	if got := out[0].Summary.Params["time"]; got != "22:50" {
		t.Errorf("beyond spread: summary time = %v, want 22:50", got)
	}
	if got := out[0].Apply[0].Prefill["time"]; got != "22:50" {
		t.Errorf("beyond spread: prefill time = %v, want 22:50", got)
	}
}

func TestBenefitKWhDropsNaN(t *testing.T) {
	if _, ok := benefitKWh(Benefit{Unit: "kwh_week", Value: math.NaN()}); ok {
		t.Error("NaN kWh benefit passed the minimum")
	}
	if _, ok := benefitKWh(Benefit{Unit: "switches_day", Value: math.NaN()}); ok {
		t.Error("NaN switches benefit passed the minimum")
	}
	if _, ok := benefitKWh(Benefit{Unit: "kwh_week", Value: 0.5}); !ok {
		t.Error("benefit at the minimum was dropped")
	}
}

// TestGateShowsStabilisedLatestAsClock checks the shift suggestion: its latest
// start is a minute of the day in Values, and the summary and the prefill show
// it as HH:MM, not as a number the assistant cannot read.
func TestGateShowsStabilisedLatestAsClock(t *testing.T) {
	cand := gateCand("shift:spuelmaschine", "shift", 1, 2, 2, true)
	cand.Finding.Summary = Text{Code: "shift.load", Params: map[string]any{"device": "spuelmaschine", "latest": "20:45"}}
	cand.Finding.Apply = []Fix{{Code: "shift_start", Template: "shift_start",
		Prefill: map[string]any{"device": "spuelmaschine", "latest": "20:45", "on_w": 500.0}}}
	cand.Values = map[string]float64{"latest": 1245}
	cand.Spread = map[string]float64{"latest": 8}
	prev := map[string]Shown{"shift:spuelmaschine": {Lower: 2, Values: map[string]float64{"latest": 1245}}}

	out, _ := Gate([]Candidate{cand}, Doc{}, prev, GateOptions{})
	if len(out) != 1 {
		t.Fatalf("got %v, want one finding", gateIDs(out))
	}
	if got := out[0].Summary.Params["latest"]; got != "20:45" {
		t.Errorf("summary latest = %v, want 20:45", got)
	}
	if got := out[0].Apply[0].Prefill["latest"]; got != "20:45" {
		t.Errorf("prefill latest = %v, want 20:45", got)
	}
}

func TestGateHarmFirstAfterVariety(t *testing.T) {
	bad := func(id string, kwh float64) Candidate {
		c := gateCand(id, "x", kwh, 2, 2, true)
		c.Finding.Severity = "bad"
		return c
	}
	cands := []Candidate{
		bad("x1", 9),
		bad("x2", 8),
		bad("x3", 7),
		gateCand("y1", "y", 1, 2, 2, true),
	}
	out, _ := Gate(cands, Doc{}, nil, GateOptions{})
	want := []string{"x1", "x2", "x3", "y1"}
	if got := gateIDs(out); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestGateCapCountsOpen(t *testing.T) {
	var cands []Candidate
	for i := 0; i < 21; i++ {
		cands = append(cands, gateCand(fmt.Sprintf("c%02d", i), fmt.Sprintf("d%02d", i), float64(30-i), 2, 2, true))
	}
	prev := map[string]Shown{"c00": {Lower: 2, Since: 42}}
	opt := GateOptions{Hidden: func(id string) bool { return id == "c00" }}
	out, shown := Gate(cands, Doc{}, prev, opt)
	if len(out) != 20 {
		t.Fatalf("got %d findings, want 20", len(out))
	}
	for _, f := range out {
		if f.ID == "c00" {
			t.Errorf("hidden finding c00 is in the result %v", gateIDs(out))
		}
	}
	if got := shown["c00"].Since; got != 42 {
		t.Errorf("hidden finding Since = %d, want 42 kept in shown", got)
	}
}

// spreadCovers reports whether every key of the values has a spread.
func spreadCovers(t *testing.T, c Candidate) {
	t.Helper()
	for k := range c.Values {
		s, ok := c.Spread[k]
		if !ok {
			t.Errorf("%s: value %q has no spread", c.Detector, k)
			continue
		}
		if !(s > 0) {
			t.Errorf("%s: spread of %q = %v, want > 0", c.Detector, k, s)
		}
	}
}

func TestFlappingCandidateSpreadOfGaps(t *testing.T) {
	gaps := []float64{300, 330, 270, 310, 290}
	rules := map[string]Rule{}
	events := []ruleEvent{{at: 1}, {at: 2}}
	c := flappingCandidate("dev", DeviceInfo{}, rules, []string{"r1", "r2"}, "r1", 60, Median(gaps), gaps, events, false)
	spreadCovers(t, c)
	if want := MADScale(gaps); c.Spread["period_s"] != want {
		t.Errorf("period spread = %v, want %v", c.Spread["period_s"], want)
	}
}

func TestNoEffectCandidateSpreadIsOneCase(t *testing.T) {
	tally := &noEffectTally{considered: 20, failures: 5, offline: 5, failAt: []int64{1, 2, 3, 4, 5}}
	c := noEffectCandidate("dev", DeviceInfo{}, Rule{ID: "r1"}, tally, 0.3)
	spreadCovers(t, c)
	if want := 1.0 / 20; c.Spread["share"] != want {
		t.Errorf("share spread = %v, want %v", c.Spread["share"], want)
	}
}

func TestSurplusCandidateSpreadOfWindowStart(t *testing.T) {
	const p = 1000.0
	var days []surWinDay
	for i := 0; i < 4; i++ {
		d := surWinDay{weight: 1}
		for q := 0; q < dayQuarters; q++ {
			d.seen[q] = true
		}
		for q := 40 + i; q < 48; q++ {
			d.without[q] = p + surWinReserveMarginW + 500
		}
		days = append(days, d)
	}
	pick := func(d *surWinDay) *[dayQuarters]float64 { return &d.without }
	c := surWinCandidate(context.Background(), &Input{}, "dev", p, 1500, "export", 40, 8, 2, 1, days, 30, pick)
	spreadCovers(t, c)
	if got := c.Spread["start"]; !(got > 0) {
		t.Errorf("start spread = %v, want > 0", got)
	}
}

func TestThresholdSpreadOfDailyPeaks(t *testing.T) {
	loc := time.UTC
	var minutes []ReplayMinute
	for day := 0; day < 4; day++ {
		start := time.Date(2026, 11, 10+day, 0, 0, 0, 0, loc)
		for m := 0; m < 1440; m += 60 {
			v := 100.0
			if m == 720 {
				v = float64(1000 + 200*day)
			}
			minutes = append(minutes, ReplayMinute{At: start.Add(time.Duration(m) * time.Minute), Balance: map[string]float64{"grid_export": v}})
		}
	}
	spread := thresholdSpread(minutes, loc, "grid_export")
	spreadCovers(t, Candidate{Detector: "threshold_rating", Values: map[string]float64{"today": 0, "soll": 0, "gain_week": 0}, Spread: spread})
	if want := thresholdMinGainKWhWeek; spread["gain_week"] != want {
		t.Errorf("gain_week spread = %v, want %v", spread["gain_week"], want)
	}
}
