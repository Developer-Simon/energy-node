package autolearn

import (
	"math"
	"sort"
)

// Shown is what the suggestion band showed for a finding last time. Lower is
// the lower confidence bound, Values the numbers on screen, Since the time the
// finding was first shown.
type Shown struct {
	Lower  float64
	Values map[string]float64
	Since  int64
}

const (
	// calmKeep is the share of the limit under which a shown finding goes away.
	calmKeep = 0.75
	// gateMaxFindings caps the number of suggestions.
	gateMaxFindings = 20
	// gateTopSlots is the number of leading places checked for detector variety.
	gateTopSlots = 3
	// gatePerDetector is the most suggestions of one detector in the top slots.
	gatePerDetector = 2
	// probeRuleID names the prepared rule in the compatibility check.
	probeRuleID = "candidate-probe"
)

// prefillRules builds the rule of a template from its prefill. A template
// without a builder, or a prefill the builder rejects, leaves the candidate
// unchecked for compatibility.
var prefillRules = map[string]func(prefill map[string]any) (Rule, bool){}

// Gate runs the candidates through the gates of spec 15.8: minimum data,
// benefit, confidence, compatibility with the rule document and calm. The
// result is ordered and capped. prev holds what was shown last time, keyed by
// finding ID, and shown is what is shown now.
func Gate(cands []Candidate, doc Doc, prev map[string]Shown) (out []Finding, shown map[string]Shown) {
	type scored struct {
		finding Finding
		cand    Candidate
		score   float64
	}
	var kept []scored
	for _, c := range cands {
		f := c.Finding
		if c.NEff < c.MinNEff {
			continue
		}
		if f.AppliedIn(doc) {
			continue
		}
		_, seen := prev[f.ID]
		if seen && c.Lower < calmKeep*c.Limit {
			continue
		}

		conf := f.Confidence
		var score float64
		if f.Group != "check" {
			kwh, ok := benefitKWh(c.Benefit)
			if !ok {
				continue
			}
			var pass bool
			conf, pass = gateConfidence(c)
			if !pass && seen {
				conf, pass = "estimate", true
			}
			if !pass || !compatible(f, doc) {
				continue
			}
			score = kwh * confidenceWeight(conf)
		}
		f.Confidence = conf
		kept = append(kept, scored{finding: f, cand: c, score: score})
	}

	sort.SliceStable(kept, func(i, j int) bool {
		bi, bj := kept[i].finding.Severity == "bad", kept[j].finding.Severity == "bad"
		if bi != bj {
			return bi
		}
		if kept[i].score != kept[j].score {
			return kept[i].score > kept[j].score
		}
		return kept[i].finding.ID < kept[j].finding.ID
	})

	// The first slots take at most gatePerDetector of each detector. The rest
	// keep their order behind them.
	used := make([]bool, len(kept))
	perDetector := map[string]int{}
	picked := 0
	for i, s := range kept {
		if picked == gateTopSlots {
			break
		}
		if perDetector[s.cand.Detector] >= gatePerDetector {
			continue
		}
		perDetector[s.cand.Detector]++
		used[i] = true
		picked++
	}
	var ordered []scored
	for i, s := range kept {
		if used[i] {
			ordered = append(ordered, s)
		}
	}
	for i, s := range kept {
		if !used[i] {
			ordered = append(ordered, s)
		}
	}
	if len(ordered) > gateMaxFindings {
		ordered = ordered[:gateMaxFindings]
	}

	shown = make(map[string]Shown, len(ordered))
	for _, s := range ordered {
		last, seen := prev[s.finding.ID]
		entry := Shown{Lower: s.cand.Lower, Values: copyValues(s.cand.Values)}
		if seen {
			entry.Since = last.Since
			if !valuesMoved(s.cand.Values, last.Values, s.cand.Spread) {
				entry.Values = copyValues(last.Values)
			}
		}
		shown[s.finding.ID] = entry
		out = append(out, displayFinding(s.finding, entry.Values))
	}
	return out, shown
}

// displayFinding returns f with the stabilised values in its summary params
// and in the prefills of its fixes. Only keys that exist in those maps change,
// the key "time" is shown as HH:MM. f and its maps are not modified.
func displayFinding(f Finding, values map[string]float64) Finding {
	if len(values) == 0 {
		return f
	}
	params := make(map[string]any, len(f.Summary.Params))
	for k, v := range f.Summary.Params {
		params[k] = v
	}
	for k, v := range values {
		if _, ok := params[k]; ok {
			params[k] = displayValue(k, v)
		}
	}
	f.Summary = Text{Code: f.Summary.Code, Params: params}

	apply := make([]Fix, len(f.Apply))
	for i, fix := range f.Apply {
		apply[i] = fix
		if fix.Prefill == nil {
			continue
		}
		prefill := make(map[string]any, len(fix.Prefill))
		for k, v := range fix.Prefill {
			prefill[k] = v
		}
		for k, v := range values {
			if _, ok := prefill[k]; ok {
				prefill[k] = displayValue(k, v)
			}
		}
		apply[i].Prefill = prefill
	}
	f.Apply = apply
	return f
}

// displayValue formats a stabilised value for display. Times are minutes of
// the day and shown as HH:MM, like the detectors do.
func displayValue(key string, v float64) any {
	if key == "time" {
		return switchTimeClock(v)
	}
	return v
}

// gateConfidence returns the confidence of a candidate and whether it passes
// the security gate at all.
func gateConfidence(c Candidate) (string, bool) {
	switch {
	case c.Finding.Confidence == "high" && c.Limit == 0:
		// A runtime detector has no statistical limit and sets its own level.
		return "high", true
	case c.Lower >= c.Limit && c.HalvesAgree:
		return "high", true
	case c.Lower >= c.Limit || c.Finding.Confidence == "estimate":
		return "estimate", true
	}
	return "", false
}

func confidenceWeight(conf string) float64 {
	if conf == "high" {
		return 1
	}
	return 0.5
}

// benefitMinimum is the smallest benefit per unit that is worth a suggestion.
var benefitMinimum = map[string]float64{
	"kwh_week":     0.5,
	"actions_week": 3,
	"switches_day": 10,
}

// benefitKWh converts a benefit to kWh per week with the fixed rates of spec
// 15.8: 1 kWh = 5 handgriffe = 20 switches. It reports false for an unknown
// unit or a benefit below its minimum.
func benefitKWh(b Benefit) (float64, bool) {
	min, known := benefitMinimum[b.Unit]
	// NaN fails the comparison, so it is dropped with the unknown units.
	if !known || !(b.Value >= min) {
		return 0, false
	}
	switch b.Unit {
	case "actions_week":
		return b.Value / 5, true
	case "switches_day":
		return b.Value / 20, true
	}
	return b.Value, true
}

// compatible runs the prepared rule of the first template fix through the
// static rule check together with the document. A new duplicate or
// contradiction makes the candidate incompatible.
func compatible(f Finding, doc Doc) bool {
	for _, fix := range f.Apply {
		if fix.Template == "" {
			continue
		}
		build := prefillRules[fix.Template]
		if build == nil {
			return true
		}
		rule, ok := build(fix.Prefill)
		if !ok {
			return true
		}
		rule.ID = probeRuleID
		rule.Enabled = true
		probe := Doc{
			Rules:     append(append([]Rule(nil), doc.Rules...), rule),
			Scenarios: doc.Scenarios,
		}
		for _, pf := range StaticFindings(probe, nil) {
			if (pf.Kind == "duplicate" || pf.Kind == "contradiction") && containsID(pf.RuleIDs, probeRuleID) {
				return false
			}
		}
		return true
	}
	return true
}

func containsID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// valuesMoved reports whether a value wandered further than its spread from
// the value on screen, or whether the sets of keys differ.
func valuesMoved(now, shown, spread map[string]float64) bool {
	if len(now) != len(shown) {
		return true
	}
	for k, v := range now {
		old, ok := shown[k]
		if !ok || math.Abs(v-old) > spread[k] {
			return true
		}
	}
	return false
}

func copyValues(in map[string]float64) map[string]float64 {
	if in == nil {
		return nil
	}
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
