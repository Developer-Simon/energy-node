package autolearn

import (
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	// frequentWindow is the period in which an hour is searched for frequent triggers.
	frequentWindow = 24 * time.Hour
	// seasonWindow is the period the seasonal maximum is taken from.
	seasonWindow = 365 * 24 * time.Hour
)

// Thresholds are the limits of the observed checks.
type Thresholds struct {
	// FrequentMinCount is the number of triggers in one hour that counts as frequent.
	FrequentMinCount int
	// NeverFiredWindow is the period without a trigger that makes a rule never fired.
	NeverFiredWindow time.Duration
	// NeverFiredMinAge is how long a rule must be known before it can be never fired.
	NeverFiredMinAge time.Duration
	// MinRangeDays is how many days of values a key needs before its range is trusted.
	MinRangeDays int
}

// DefaultThresholds are the limits for normal use.
func DefaultThresholds() Thresholds {
	return Thresholds{
		FrequentMinCount: 12,
		NeverFiredWindow: 14 * 24 * time.Hour,
		NeverFiredMinAge: 14 * 24 * time.Hour,
		MinRangeDays:     7,
	}
}

// ThresholdsFromEnv chooses the limits. ENERGY_NODE_AUTOLEARN_FAST=1 gives the
// demo limits, every other value the default limits.
func ThresholdsFromEnv(getenv func(string) string) Thresholds {
	if getenv == nil {
		getenv = os.Getenv
	}
	if strings.TrimSpace(getenv("ENERGY_NODE_AUTOLEARN_FAST")) == "1" {
		return Thresholds{FrequentMinCount: 3}
	}
	return DefaultThresholds()
}

// ObservedFindings checks the rules against what was observed with the default
// thresholds. See ObservedFindingsWith.
func ObservedFindings(doc Doc, st ObserverState, season SeasonalSource, now time.Time) []Finding {
	return ObservedFindingsWith(doc, st, season, now, DefaultThresholds())
}

// ObservedFindingsWith checks the rules against what was observed. A rule that
// fires at least th.FrequentMinCount times in one hour of the last 24 h is
// frequent. An enabled rule that did not fire within th.NeverFiredWindow,
// although it is at least th.NeverFiredMinAge known and its value has been seen
// for th.MinRangeDays, is never fired. A threshold the value never reached is
// reported as a seasonal rest when the season had a higher maximum. season may
// be nil, which skips the seasonal check.
func ObservedFindingsWith(doc Doc, st ObserverState, season SeasonalSource, now time.Time, th Thresholds) []Finding {
	var findings []Finding
	for _, rule := range doc.Rules {
		if !rule.Enabled {
			continue
		}
		stats, seen := st.Rules[rule.ID]
		if !seen {
			continue
		}
		if f, ok := frequentFinding(rule, stats, now, th.FrequentMinCount); ok {
			findings = append(findings, f)
		}
		if f, ok := neverFiredFinding(rule, stats, st, season, now, th); ok {
			findings = append(findings, f)
		}
	}
	sort.SliceStable(findings, func(i, j int) bool {
		ri, rj := severityRank[findings[i].Severity], severityRank[findings[j].Severity]
		if ri != rj {
			return ri < rj
		}
		return findings[i].ID < findings[j].ID
	})
	return findings
}

// frequentFinding reports the busiest hour of the last 24 h if it reached the
// frequent count. The evidence covers the 4 h around that hour.
func frequentFinding(rule Rule, stats RuleStats, now time.Time, minCount int) (Finding, bool) {
	first := now.Add(-frequentWindow).Unix() / 3600
	last := now.Unix() / 3600
	var peak int64
	peakCount := 0
	for h := first; h <= last; h++ {
		if n := stats.Hours[h]; n > peakCount {
			peak, peakCount = h, n
		}
	}
	if peakCount < minCount {
		return Finding{}, false
	}

	start := peak * 3600
	from, to := start-5400, start+9000 // 4 h centred on the middle of the hour
	var ist []int64
	for _, e := range stats.Events {
		if e >= from && e < to {
			ist = append(ist, e)
		}
	}
	ev := Evidence{Shape: "events", From: from, To: to, Ist: ist}
	if len(ist) > 0 {
		ev.SollSpans = [][2]int64{{ist[0], ist[len(ist)-1]}}
	}

	var fixes []Fix
	for i, c := range rule.Conditions {
		if c.Type == "balance_threshold" && c.Hysteresis == 0 {
			fixes = append(fixes, Fix{Code: "set_hysteresis", RuleID: rule.ID, Ops: []Op{
				{Path: []any{"conditions", i, "hysteresis"}, Value: hysteresisFor(c.Threshold)}}})
			break
		}
	}
	fixes = append(fixes, Fix{Code: "set_cooldown", RuleID: rule.ID, Ops: []Op{
		{Path: []any{"cooldown_seconds"}, Value: 600}}})

	return Finding{
		ID:         FindingID("frequent", rule.ID),
		Kind:       "frequent",
		Group:      "check",
		RuleIDs:    []string{rule.ID},
		Severity:   "warn",
		Confidence: "high",
		Summary: Text{Code: "check.frequent", Params: map[string]any{
			"rule": rule.Name, "count": peakCount}},
		Evidence: ev,
		Facts:    []Fact{{Code: "per_hour_max", Value: peakCount}},
		Apply:    fixes,
	}, true
}

// hysteresisFor is 10 % of the threshold, rounded to 50 W, at least 100 W.
func hysteresisFor(threshold float64) int {
	h := int(math.Round(threshold*0.1/50) * 50)
	if h < 100 {
		return 100
	}
	return h
}

// neverFiredFinding checks the first number condition of the rule whose value
// never reached the threshold in the last 30 days.
func neverFiredFinding(rule Rule, stats RuleStats, st ObserverState, season SeasonalSource, now time.Time, th Thresholds) (Finding, bool) {
	if now.Sub(stats.FirstSeen) < th.NeverFiredMinAge || firedSince(stats, now.Add(-th.NeverFiredWindow)) {
		return Finding{}, false
	}
	for i, c := range rule.Conditions {
		if c.Comparison != "above" && c.Comparison != "below" {
			continue
		}
		x, ok := conditionNumber(c)
		key := c.ValueKey()
		if !ok || key == "" {
			continue
		}
		ranges := st.Values[key]
		if len(ranges) == 0 || len(ranges) < th.MinRangeDays {
			continue
		}
		lo, hi := ranges[0].Min, ranges[0].Max
		for _, r := range ranges[1:] {
			lo, hi = min(lo, r.Min), max(hi, r.Max)
		}
		if c.Comparison == "above" && hi < x {
			return aboveFinding(rule, i, c, x, lo, hi, season, now)
		}
		if c.Comparison == "below" && lo > x {
			return belowFinding(rule, i, c, x, lo, hi)
		}
	}
	return Finding{}, false
}

func aboveFinding(rule Rule, i int, c Condition, x, lo, hi float64, season SeasonalSource, now time.Time) (Finding, bool) {
	var seasonMax *float64
	var months []int
	if c.Type == "balance_threshold" && season != nil {
		if maxW, m, _, ok := season.MaxW(c.Field, now.Add(-seasonWindow), now); ok {
			seasonMax, months = &maxW, m
		}
	}
	scale := Evidence{Shape: "scale", Seen: [2]float64{lo, hi}, IstValue: &x, SeasonMax: seasonMax, Unit: unitOf(c)}

	if seasonMax != nil && *seasonMax >= x {
		scale.Max = scaleMax(hi, x, *seasonMax)
		return Finding{
			ID:         FindingID("never_fired", rule.ID),
			Kind:       "never_fired",
			Group:      "check",
			RuleIDs:    []string{rule.ID},
			Severity:   "info",
			Confidence: "estimate",
			Summary: Text{Code: "check.seasonal_rest", Params: map[string]any{
				"rule": rule.Name, "threshold": x}},
			Evidence: scale,
			Facts:    []Fact{{Code: "season_months", Value: months}},
		}, true
	}

	s := math.Max(50, math.Floor(0.75*hi/50)*50)
	scale.SollValue = &s
	scale.Max = scaleMax(hi, x, s)
	return Finding{
		ID:         FindingID("never_fired", rule.ID),
		Kind:       "never_fired",
		Group:      "check",
		RuleIDs:    []string{rule.ID},
		Severity:   "warn",
		Confidence: "estimate",
		Summary: Text{Code: "check.never_fired", Params: map[string]any{
			"rule": rule.Name, "threshold": x}},
		Evidence: scale,
		Apply: []Fix{{Code: "set_threshold", RuleID: rule.ID, Ops: []Op{
			{Path: []any{"conditions", i, thresholdKey(c)}, Value: int(s)}}}},
	}, true
}

func belowFinding(rule Rule, i int, c Condition, y, lo, hi float64) (Finding, bool) {
	s := math.Ceil(1.25*lo/50) * 50
	scale := Evidence{Shape: "scale", Seen: [2]float64{lo, hi}, IstValue: &y, SollValue: &s,
		Unit: unitOf(c), Max: scaleMax(hi, y, s)}
	return Finding{
		ID:         FindingID("never_fired", rule.ID),
		Kind:       "never_fired",
		Group:      "check",
		RuleIDs:    []string{rule.ID},
		Severity:   "warn",
		Confidence: "estimate",
		Summary: Text{Code: "check.never_fired", Params: map[string]any{
			"rule": rule.Name, "threshold": y}},
		Evidence: scale,
		Apply: []Fix{{Code: "set_threshold", RuleID: rule.ID, Ops: []Op{
			{Path: []any{"conditions", i, thresholdKey(c)}, Value: int(s)}}}},
	}, true
}

// firedSince reports whether the rule triggered in an hour from since on.
func firedSince(stats RuleStats, since time.Time) bool {
	from := since.Unix() / 3600
	for h, n := range stats.Hours {
		if h >= from && n > 0 {
			return true
		}
	}
	return false
}

// thresholdKey is the field a condition keeps its number in.
func thresholdKey(c Condition) string {
	if c.Type == "balance_threshold" {
		return "threshold"
	}
	return "value"
}

// unitOf is W for balance fields. Entity values have no unit in the rule
// document, so they stay empty.
func unitOf(c Condition) string {
	if c.Type == "balance_threshold" {
		return "W"
	}
	return ""
}

// scaleMax is the largest of the values times 1.2, rounded to 100.
func scaleMax(values ...float64) float64 {
	largest := values[0]
	for _, v := range values[1:] {
		largest = max(largest, v)
	}
	return math.Round(largest*1.2/100) * 100
}
