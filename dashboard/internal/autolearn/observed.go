package autolearn

import (
	"context"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

const (
	// frequentWindow is the period in which an hour is searched for frequent triggers.
	frequentWindow = 24 * time.Hour
	// seasonWindow is the period the seasonal maximum is taken from.
	seasonWindow = 365 * 24 * time.Hour
	// frequentReplayDays is how many days the replay of the frequent check covers.
	frequentReplayDays = 14
	// hysteresisStepW is the step the minute hysteresis is rounded up to.
	hysteresisStepW = 50.0
	// cooldownFloorS is the shortest cooldown the frequent check proposes.
	cooldownFloorS = 300
)

// PVSource returns the stored roof model, the sky raster and whether a model
// exists, as Input.PV does.
type PVSource func() (PVModel, *Raster, bool)

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

// ObservedFindingsWith is ObservedFindingsWithPV without a PV source.
func ObservedFindingsWith(doc Doc, st ObserverState, season SeasonalSource, now time.Time, th Thresholds) []Finding {
	return ObservedFindingsWithPV(doc, st, season, nil, nil, now, th)
}

// ObservedFindingsWithPV checks the rules against what was observed. A rule that
// fires at least th.FrequentMinCount times in one hour of the last 24 h is
// frequent. An enabled rule that did not fire within th.NeverFiredWindow,
// although it is at least th.NeverFiredMinAge known and its value has been seen
// for th.MinRangeDays, is never fired. A threshold the value never reached is
// reported as a seasonal rest when the season had a higher maximum. season may
// be nil, which skips the seasonal check. pv may be nil. With a safe PV model
// and the site location, a PV or grid export threshold is checked against the
// clear-sky curve instead of the seasonal maximum. in may be nil, which keeps
// the hysteresis of the frequent check to its fallback and leaves out the
// replay evidence.
func ObservedFindingsWithPV(doc Doc, st ObserverState, season SeasonalSource, pv PVSource, in *Input, now time.Time, th Thresholds) []Finding {
	var findings []Finding
	for _, rule := range doc.Rules {
		if !rule.Enabled {
			continue
		}
		stats, seen := st.Rules[rule.ID]
		if !seen {
			continue
		}
		if f, ok := frequentFinding(rule, stats, in, now, th.FrequentMinCount); ok {
			findings = append(findings, f)
		}
		if f, ok := neverFiredFinding(rule, stats, st, season, pv, doc.Location, now, th); ok {
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
// frequent count. The evidence covers the 4 h around that hour. With in, the
// hysteresis comes from the minute noise and the replay of the rule with it
// gives the events that are left as the proposed times.
func frequentFinding(rule Rule, stats RuleStats, in *Input, now time.Time, minCount int) (Finding, bool) {
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
	confidence := "high"
	hystIdx := -1
	hyst := 0
	for i, c := range rule.Conditions {
		// A learned threshold has no fixed number to derive a hysteresis from.
		if _, learned := c.Learned["threshold"]; learned {
			continue
		}
		if c.Type == "balance_threshold" && c.Hysteresis == 0 {
			hystIdx, hyst = i, hysteresisFor(c.Threshold)
			if v, ok := minuteHysteresis(in, rule, c, now); ok {
				hyst = v
			}
			break
		}
	}
	if hystIdx >= 0 {
		fixes = append(fixes, Fix{Code: "set_hysteresis", RuleID: rule.ID, Ops: []Op{
			{Path: []any{"conditions", hystIdx, "hysteresis"}, Value: hyst}}})
		if in != nil {
			if soll, est, ok := frequentSoll(in, rule, hystIdx, hyst, from, to); ok {
				// The replayed triggers are the Soll. The span of the Ist triggers
				// would stand in for it, so it is dropped.
				ev.Soll = soll
				ev.SollSpans = nil
				if est {
					confidence = "estimate"
				}
			}
		}
	}
	fixes = append(fixes, Fix{Code: "set_cooldown", RuleID: rule.ID, Ops: []Op{
		{Path: []any{"cooldown_seconds"}, Value: frequentCooldown(ist)}}})

	return Finding{
		ID:         FindingID("frequent", rule.ID),
		Kind:       "frequent",
		Group:      "check",
		RuleIDs:    []string{rule.ID},
		Severity:   "warn",
		Confidence: confidence,
		Summary: Text{Code: "check.frequent", Params: map[string]any{
			"rule": rule.Name, "count": peakCount}},
		Evidence: ev,
		Facts:    []Fact{{Code: "per_hour_max", Value: peakCount}},
		Apply:    fixes,
	}, true
}

// minuteHysteresis is 2 times the minute noise of the condition signal plus
// the load step, the operating power of the device, rounded up to 50 W. It
// reports false without minute days near the threshold or without an operating
// power, and then the fallback of hysteresisFor applies.
func minuteHysteresis(in *Input, rule Rule, c Condition, now time.Time) (int, bool) {
	if in == nil || in.Minutes == nil {
		return 0, false
	}
	device, _, ok := thresholdDevice(in, rule)
	if !ok {
		return 0, false
	}
	p, ok := learnedOperatingPower(in.Learned, device)
	if !ok {
		return 0, false
	}
	noise, ok := MinuteNoise(in.Minutes, "b:"+c.Field, c.Threshold, now.AddDate(0, 0, -engineHistoryDays), now)
	if !ok {
		return 0, false
	}
	return int(math.Ceil((2*noise+p)/hysteresisStepW) * hysteresisStepW), true
}

// frequentSoll replays the rule with hysteresis h over the last frequentReplayDays
// days and returns the trigger times left in [from, to), in Unix seconds. est
// is set when stretched quarter hours took part. ok is false when the rule
// cannot be replayed or no minutes exist.
func frequentSoll(in *Input, rule Rule, idx, h int, from, to int64) (soll []int64, est, ok bool) {
	device, info, ok := thresholdDevice(in, rule)
	if !ok {
		return nil, false, false
	}
	p, ok := learnedOperatingPower(in.Learned, device)
	if !ok {
		return nil, false, false
	}
	raw, err := thresholdRuleJSON(rule, idx, rule.Conditions[idx].Threshold, float64(h))
	if err != nil {
		return nil, false, false
	}
	cycle, _ := surWinLearnedNumber(in.Learned, "device/"+device+"/cycle_wh")
	minutes, est := ReplayDays(in, frequentReplayDays)
	if len(minutes) == 0 {
		return nil, false, false
	}
	var site sun.Location
	if in.Doc.Location != nil {
		site = *in.Doc.Location
	}
	target := ReplayTarget{RuleID: rule.ID, Device: device, OpW: p,
		PeakW: feedbackInrush(in.Days, device, p), CycleWh: cycle, MinRunS: minRunForRole(info)}
	res, err := Replay(context.Background(), raw, target, minutes, RealState(in, device, minutes), replayLoc(in), site)
	if err != nil {
		return nil, false, false
	}
	first := minutes[0].At.Unix()
	soll = []int64{}
	for _, f := range res.Fires {
		if t := first + f; t >= from && t < to {
			soll = append(soll, t)
		}
	}
	return soll, est, true
}

// frequentCooldown is 3 times the mean distance of the triggers in the burst,
// at least cooldownFloorS, rounded up to whole seconds.
func frequentCooldown(ist []int64) int {
	if len(ist) < 2 {
		return cooldownFloorS
	}
	mean := float64(ist[len(ist)-1]-ist[0]) / float64(len(ist)-1)
	return max(cooldownFloorS, int(math.Ceil(3*mean)))
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
func neverFiredFinding(rule Rule, stats RuleStats, st ObserverState, season SeasonalSource, pv PVSource, loc *sun.Location, now time.Time, th Thresholds) (Finding, bool) {
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
			return aboveFinding(rule, i, c, x, lo, hi, season, pv, loc, now)
		}
		if c.Comparison == "below" && lo > x {
			return belowFinding(rule, i, c, x, lo, hi)
		}
	}
	return Finding{}, false
}

func aboveFinding(rule Rule, i int, c Condition, x, lo, hi float64, season SeasonalSource, pv PVSource, loc *sun.Location, now time.Time) (Finding, bool) {
	var seasonMax *float64
	var months []int
	// unsure is a PV threshold the curve does not reach while the raster does
	// not know the summer sun yet. It never becomes "can never be met".
	unsure := false
	if c.Type == "balance_threshold" && (c.Field == "pv" || c.Field == "grid_export") && pv != nil && loc != nil {
		if m, r, ok := pv(); ok && m.Safe {
			if f, ok := pvAboveFinding(rule, i, c, x, lo, hi, m, r, *loc, now); ok {
				return f, true
			}
			unsure = true
		}
	}
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
	if unsure {
		return Finding{}, false
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

// pvAboveFinding is a PV or grid export threshold above the clear-sky curve of
// a safe model. A threshold the curve reaches later in the year is a seasonal
// rest with the first week that reaches it. A threshold the curve never reaches
// is never fired with the threshold fix, but only when the raster knows the
// highest noon sun of the year. Otherwise ok is false and the caller falls back
// to the seasonal maximum.
func pvAboveFinding(rule Rule, i int, c Condition, x, lo, hi float64, m PVModel, r *Raster, loc sun.Location, now time.Time) (Finding, bool) {
	scale := Evidence{Shape: "scale", Seen: [2]float64{lo, hi}, IstValue: &x, Unit: unitOf(c)}
	if week, ok := SeasonWeekFor(x, m, r, loc, now); ok {
		scale.Max = scaleMax(hi, x)
		return Finding{
			ID:         FindingID("never_fired", rule.ID),
			Kind:       "never_fired",
			Group:      "check",
			RuleIDs:    []string{rule.ID},
			Severity:   "info",
			Confidence: "estimate",
			Summary: Text{Code: "check.never_fired_season", Params: map[string]any{
				"rule": rule.Name, "threshold": x, "week": week}},
			Evidence: scale,
			Facts:    []Fact{{Code: "season_week", Value: week}},
		}, true
	}
	if !rasterCoversYear(r, loc, now) {
		return Finding{}, false
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

// pvCoverMarginDeg is how far the highest cell of the raster may stay below
// the highest noon sun of the coming year.
const pvCoverMarginDeg = 5.0

// rasterCoversYear reports whether the raster holds values near the highest
// noon sun of the coming year. Without them the roof model only knows the low
// sun, and a threshold it does not reach may still be reached in summer. A nil
// raster covers nothing.
func rasterCoversYear(r *Raster, loc sun.Location, now time.Time) bool {
	if r == nil {
		return false
	}
	top := -90.0
	r.Each(func(_, el, _ float64) {
		top = math.Max(top, el)
	})
	need := -90.0
	for k := 0; k <= seasonWeeks; k++ {
		_, el := weekNoon(now.Add(time.Duration(k)*7*24*time.Hour), loc)
		need = math.Max(need, el)
	}
	return top >= need-pvCoverMarginDeg
}

// seasonWeeks is how many weeks ahead SeasonWeekFor looks.
const seasonWeeks = 53

// seasonStep is the sampling step of the sun elevation within a week.
const seasonStep = 15 * time.Minute

// SeasonWeekFor is the first ISO week, counted from now, in which the clear-sky
// curve reaches the threshold at the highest sun elevation of that week. ok is
// false when no week within seasonWeeks reaches it.
func SeasonWeekFor(threshold float64, m PVModel, r *Raster, loc sun.Location, now time.Time) (week int, ok bool) {
	for k := 0; k <= seasonWeeks; k++ {
		start := now.Add(time.Duration(k) * 7 * 24 * time.Hour)
		az, el := weekNoon(start, loc)
		if m.Best(az, el, r) >= threshold {
			_, w := start.ISOWeek()
			return w, true
		}
	}
	return 0, false
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

// weekNoon is the sun position az/el at the highest elevation of the 7 days
// from start, sampled every seasonStep. It returns the sky position used by
// PVModel.Best.
func weekNoon(start time.Time, loc sun.Location) (az, el float64) {
	el = -90
	for s := time.Duration(0); s < 7*24*time.Hour; s += seasonStep {
		a, e := sun.Position(start.Add(s), loc.Lat, loc.Lon)
		if e > el {
			az, el = a, e
		}
	}
	return az, el
}
