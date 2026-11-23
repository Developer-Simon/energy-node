package autolearn

import (
	"context"
	"math"
	"sort"
)

// FlappingDetector flags a device that two rules switch back and forth faster
// than it tolerates (spec 15.4 "Takten"). The fix lengthens the minimum hold of
// the switching-off rule.
var FlappingDetector = Detector{
	Name:      "flapping",
	Group:     "reliability",
	Light:     true,
	PerDevice: true,
	Run:       flappingRun,
}

const (
	// flappingMinChanges is the number of changes between the two rules of A, B, A, B.
	flappingMinChanges = 3
	// flappingMinSpanSec is the span of the events the finding needs: one day.
	flappingMinSpanSec = 86400
	// flappingMaxEvidence caps the event times kept as evidence.
	flappingMaxEvidence = 50
)

// flappingRun finds the device with exactly two rules alternating and a median
// period below its minimum runtime, over at least a day.
func flappingRun(ctx context.Context, in *Input, device string) []Candidate {
	if ctx.Err() != nil {
		return nil
	}
	info, ok := in.Devices[device]
	if !ok {
		return nil
	}
	minRun := reliabilityMinRuntime(info)
	if minRun <= 0 {
		return nil
	}
	events := ruleEventsOf(in.Days, device)
	if len(events) < flappingMinChanges+1 {
		return nil
	}
	distinct := map[string]bool{}
	for _, e := range events {
		distinct[e.rule] = true
	}
	if len(distinct) != 2 {
		return nil
	}
	var ids []string
	for id := range distinct {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rules := ruleByID(in.Doc)
	if _, ok := rules[ids[0]]; !ok {
		return nil
	}
	if _, ok := rules[ids[1]]; !ok {
		return nil
	}

	var gaps []float64
	offCount := map[string]int{}
	for i, e := range events {
		if !e.on {
			offCount[e.rule]++
		}
		if i > 0 && events[i-1].rule != e.rule {
			gaps = append(gaps, float64(e.at-events[i-1].at))
		}
	}
	if len(gaps) < flappingMinChanges {
		return nil
	}
	if events[len(events)-1].at-events[0].at < flappingMinSpanSec {
		return nil
	}
	period := Median(gaps)
	if period >= minRun {
		return nil
	}

	// The switching-off rule is the one with more off events.
	offRule := ids[0]
	if offCount[ids[1]] > offCount[ids[0]] {
		offRule = ids[1]
	}
	feedback := flappingFeedback(in, device, ids)
	return []Candidate{flappingCandidate(device, info, rules, ids, offRule, minRun, period, gaps, events, feedback)}
}

// flappingFeedback reports whether an unstable feedback finding of the device
// names the two rules of the flapping pair. Then the switch-ons follow the load
// jump, and the cause is the feedback (spec 15.4 "Takten").
func flappingFeedback(in *Input, device string, ids []string) bool {
	want := append([]string(nil), ids...)
	sort.Strings(want)
	for _, f := range FeedbackFindings(in) {
		if f.Device != device || len(f.RuleIDs) != len(want) {
			continue
		}
		got := append([]string(nil), f.RuleIDs...)
		sort.Strings(got)
		if got[0] == want[0] && got[1] == want[1] {
			return true
		}
	}
	return false
}

// flappingCandidate builds the finding of a flapping device.
func flappingCandidate(device string, info DeviceInfo, rules map[string]Rule, ids []string, offRule string,
	minRun, period float64, gaps []float64, events []ruleEvent, feedback bool) Candidate {
	hold := math.Ceil(math.Max(minRun, 3*period))
	apply := []Fix{{Code: "open_rule", RuleID: offRule}}
	if len(rules[offRule].Conditions) > 0 {
		apply = []Fix{{Code: "min_runtime", RuleID: offRule,
			Ops: []Op{{Path: []any{"conditions", 0, "hold_seconds"}, Value: int(hold)}}}}
	}

	var times []int64
	from := len(events) - flappingMaxEvidence
	if from < 0 {
		from = 0
	}
	for _, e := range events[from:] {
		times = append(times, e.at)
	}
	name := info.Name
	if name == "" {
		name = device
	}
	finding := Finding{
		ID:         FindingID("flapping", ids...) + ":" + device,
		Kind:       "flapping",
		Group:      "check",
		RuleIDs:    ids,
		Severity:   "warn",
		Confidence: "high",
		Summary: Text{Code: "check.flapping", Params: map[string]any{
			"rule":     ruleName(rules, ids[0]),
			"other":    ruleName(rules, ids[1]),
			"device":   name,
			"period_s": int(math.Round(period)),
		}},
		Evidence: Evidence{Shape: "events", From: events[0].at, To: events[len(events)-1].at, Ist: times},
		Facts:    []Fact{{Code: "min_runtime", Value: int(minRun)}},
		Apply:    apply,
		Device:   device,
	}
	if feedback {
		finding.Facts = append(finding.Facts, Fact{Code: "cause", Value: "feedback"})
	}
	return Candidate{
		Finding:  finding,
		Detector: "flapping",
		NEff:     float64(len(gaps)),
		MinNEff:  flappingMinChanges,
		// The pattern is a count, not a sample: its lower bound is the count itself.
		Lower:  1,
		Limit:  1,
		Values: map[string]float64{"period_s": period},
		Spread: map[string]float64{"period_s": MADScale(gaps)},
	}
}

// ruleName returns the name of a rule, or its ID when the name is empty.
func ruleName(rules map[string]Rule, id string) string {
	if r, ok := rules[id]; ok && r.Name != "" {
		return r.Name
	}
	return id
}
