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
)

// GateOptions are the inputs of Gate that the caller knows and the candidates
// do not. The zero value hides nothing and has no targets.
type GateOptions struct {
	// Hidden reports a finding that was dismissed or postponed. Nil hides nothing.
	Hidden func(id string) bool
	// Targets returns the targets of a rule or scenario ID. Nil means no targets,
	// so the rules cover no suggestion.
	Targets func(ruleID string) []Target
}

// scored is a candidate that passed the gates, with its finding as shown and
// its sort score.
type scored struct {
	finding Finding
	cand    Candidate
	score   float64
}

// Gate runs the candidates through the gates of spec 15.8: minimum data,
// benefit, confidence, compatibility with the rule document and calm. The
// result is ordered and capped. prev holds what was shown last time, keyed by
// finding ID, and shown is what is shown now. Hidden findings do not count
// against the cap.
func Gate(cands []Candidate, doc Doc, prev map[string]Shown, opt GateOptions) (out []Finding, shown map[string]Shown) {
	active := activeIntents(doc, opt.Targets)
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

		if c.Replay != nil && replayAddsGrid(*c.Replay) {
			continue
		}
		if replayRequired(c) && c.Replay == nil {
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
			if !pass || coveredBy(f, active) {
				continue
			}
			if c.Replay != nil && c.Replay.Estimate && conf == "high" {
				conf = "estimate"
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

	// A hidden finding leaves the open list before the cap, but keeps its
	// entry in shown, so the calm rule and Since do not jump.
	shown = make(map[string]Shown, len(kept))
	var open []scored
	for _, s := range kept {
		id := s.finding.ID
		if opt.Hidden != nil && opt.Hidden(id) {
			if last, ok := prev[id]; ok {
				shown[id] = last
			}
			continue
		}
		open = append(open, s)
	}

	ordered := gateOrder(open)
	if len(ordered) > gateMaxFindings {
		ordered = ordered[:gateMaxFindings]
	}

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

// gateOrder puts the harmful findings ("bad") before all others. The detector
// variety is chosen inside each severity, so a benefit never pushes out a harm
// from the top slots. Both lists keep the order of kept, which is by score.
func gateOrder(kept []scored) []scored {
	var harm, rest []scored
	for _, s := range kept {
		if s.finding.Severity == "bad" {
			harm = append(harm, s)
		} else {
			rest = append(rest, s)
		}
	}
	return append(gateVariety(harm), gateVariety(rest)...)
}

// gateVariety lets at most gatePerDetector of each detector into the first
// gateTopSlots places. The rest keep their order behind them.
func gateVariety(list []scored) []scored {
	used := make([]bool, len(list))
	perDetector := map[string]int{}
	picked := 0
	for i, s := range list {
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
	ordered := make([]scored, 0, len(list))
	for i, s := range list {
		if used[i] {
			ordered = append(ordered, s)
		}
	}
	for i, s := range list {
		if !used[i] {
			ordered = append(ordered, s)
		}
	}
	return ordered
}

// displayFinding returns f with the stabilised values in its summary params
// and in the prefills of its fixes. Only keys that exist in those maps change,
// the keys "time" and "latest" are shown as HH:MM. f and its maps are not modified.
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
	if key == "time" || key == "latest" {
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

// replayAddsGrid reports whether the suggestion draws more extra grid import
// than the export it uses. Such a suggestion is dropped.
func replayAddsGrid(v ReplayVerdict) bool {
	extraGrid := v.Alt.GridWh - v.Base.GridWh
	usedExport := v.Base.ExportWh - v.Alt.ExportWh
	return extraGrid > usedExport
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

// replayRequired reports the energy suggestions that are shown only with a
// replay verdict (spec 15.6): surplus windows, load shifts, threshold ratings
// and the after_battery_full suggestion of the battery detector.
func replayRequired(c Candidate) bool {
	switch c.Detector {
	case "surplus_window", "load_shift", "threshold_rating":
		return true
	case "battery":
		for _, fix := range c.Finding.Apply {
			if fix.Template == "after_battery_full" {
				return true
			}
		}
	}
	return false
}

// activeIntents returns the intents of the enabled rules of doc and of its
// enabled scenarios. A scenario switches its regulated entity as a surplus
// trigger. Without targets there are no intents.
func activeIntents(doc Doc, targets func(ruleID string) []Target) []Intent {
	if targets == nil {
		return nil
	}
	var out []Intent
	for _, r := range doc.Rules {
		if r.Enabled {
			out = append(out, RuleIntents(r, targets(r.ID))...)
		}
	}
	for _, s := range doc.Scenarios {
		if !s.Enabled {
			continue
		}
		for _, t := range targets(s.ID) {
			if t.Entity != "" {
				out = append(out, Intent{Entity: t.Entity, On: t.On != nil && *t.On, Trigger: "surplus"})
			}
		}
	}
	return out
}

// coveredBy reports whether an active rule switches the entity of the first
// fix of f with the same trigger. The same direction is a duplicate, the
// other direction a contradiction, and both are dropped. A finding without a
// known intent is never covered.
func coveredBy(f Finding, active []Intent) bool {
	in, ok := findingIntent(f)
	if !ok || in.Entity == "" {
		return false
	}
	for _, a := range active {
		if a.Entity == in.Entity && a.Trigger == in.Trigger {
			return true
		}
	}
	return false
}

// findingIntent returns the intent of the first fix with a known template.
func findingIntent(f Finding) (Intent, bool) {
	for _, fix := range f.Apply {
		if in, ok := TemplateIntent(fix); ok {
			return in, true
		}
	}
	return Intent{}, false
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
