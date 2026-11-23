package autolearn

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// ThresholdDetector rates the threshold of a surplus rule by replaying it with
// other thresholds (spec 15.6 "Schwellenbewertung"). Every replayable surplus
// rule is tried with Limits.ThresholdSteps values between 0.3 and 2 times its
// threshold. The value J = E_pv - 1.5 * E_netz - 0.02 kWh * N is maximised
// over the grid, and the middle of the widest run near the maximum is taken.
// Rules that are not replayable are skipped silently.
var ThresholdDetector = Detector{
	Name:      "threshold_rating",
	Group:     "energy",
	Light:     false,
	PerDevice: false,
	Run:       thresholdRun,
}

const (
	// thresholdReplayDays is the number of days the rating replays.
	thresholdReplayDays = 14
	// thresholdLowFactor and thresholdHighFactor bound the grid.
	thresholdLowFactor  = 0.3
	thresholdHighFactor = 2.0
	// thresholdGridPenalty is the weight of grid import in J.
	thresholdGridPenalty = 1.5
	// thresholdSwitchKWh is the cost of one switch in J.
	thresholdSwitchKWh = 0.02
	// thresholdPlateau is the share of the maximum a point needs to count as near it.
	thresholdPlateau = 0.95
	// thresholdMinGainKWhWeek is the gain per week a rating must exceed.
	thresholdMinGainKWhWeek = 0.5
	// thresholdRoundW rounds the proposed threshold.
	thresholdRoundW = 10.0
)

// thresholdJointHysteresisW and thresholdJointMinRunS are the grid of the
// joint variation (ReplayJointHold, full level only).
var (
	thresholdJointHysteresisW = []float64{0, 100, 200}
	thresholdJointMinRunS     = []float64{0, 300, 600}
)

// thresholdPoint is one replayed point of the grid.
type thresholdPoint struct {
	x, h, m float64
	j       float64
}

// thresholdRun rates the surplus rules of the document.
func thresholdRun(ctx context.Context, in *Input, _ string) []Candidate {
	if ctx.Err() != nil || in == nil || in.Limits.ThresholdSteps < 2 {
		return nil
	}
	minutes, estimate := ReplayDays(in, thresholdReplayDays)
	loc := replayLoc(in)
	days := thresholdDays(minutes, loc)
	if len(days) < 2 {
		return nil
	}
	var site sun.Location
	if in.Doc.Location != nil {
		site = *in.Doc.Location
	}
	var out []Candidate
	for _, r := range in.Doc.Rules {
		if ctx.Err() != nil {
			return nil
		}
		if !r.Enabled {
			continue
		}
		cand, ok := thresholdRule(ctx, in, r, minutes, days, estimate, loc, site)
		if ok {
			out = append(out, cand)
		}
	}
	return out
}

// thresholdRule rates one rule. It returns false when the rule is not a
// replayable surplus rule, has no operating power, or gives no finding.
func thresholdRule(ctx context.Context, in *Input, r Rule, minutes []ReplayMinute, days []string, estimate bool, loc *time.Location, site sun.Location) (Candidate, bool) {
	idx, ok := thresholdCondition(r)
	if !ok || Replayable(r) != nil {
		return Candidate{}, false
	}
	device, info, ok := thresholdDevice(in, r)
	if !ok {
		return Candidate{}, false
	}
	p, ok := learnedOperatingPower(in.Learned, device)
	if !ok {
		return Candidate{}, false
	}
	cycle := 0.0
	if v, ok := surWinLearnedNumber(in.Learned, "device/"+device+"/cycle_wh"); ok {
		cycle = v
	}
	today := r.Conditions[idx].Threshold
	if today <= 0 {
		return Candidate{}, false
	}
	hyst0 := r.Conditions[idx].Hysteresis
	run0 := minRunForRole(info)
	base := thresholdTarget{rule: r.ID, device: device, opW: p, peakW: feedbackInrush(in.Days, device, p), cycleWh: cycle}
	real := RealState(in, device, minutes)

	hysts := []float64{hyst0}
	runs := []float64{run0}
	if in.Limits.ReplayJointHold {
		hysts = thresholdJointHysteresisW
		runs = thresholdJointMinRunS
	}
	grid := thresholdGrid(today, in.Limits.ThresholdSteps)

	// Rate every point of the grid and keep the (hysteresis, minimum run) pair
	// with the best point. The pair is fixed before the plateau is read.
	var rows [][]thresholdPoint
	bestRow, bestJ := -1, math.Inf(-1)
	for _, h := range hysts {
		for _, m := range runs {
			row, err := thresholdRow(ctx, in, r, idx, base, m, h, grid, minutes, real, loc, site)
			if err != nil {
				return Candidate{}, false
			}
			rows = append(rows, row)
			for _, pt := range row {
				if pt.j > bestJ {
					bestJ = pt.j
					bestRow = len(rows) - 1
				}
			}
		}
	}
	if bestRow < 0 {
		return Candidate{}, false
	}
	row := rows[bestRow]
	chosen := thresholdWidest(row)
	soll := math.Round(chosen.x/thresholdRoundW) * thresholdRoundW
	if soll == today || soll <= 0 {
		return Candidate{}, false
	}

	// Gain against today's rule, per week, and the halves of the days.
	nDays := float64(len(days))
	todayPt := thresholdEval(ctx, in, r, idx, base, run0, hyst0, today, minutes, real, loc, site)
	if todayPt.err != nil {
		return Candidate{}, false
	}
	chosenEval := thresholdEval(ctx, in, r, idx, base, chosen.m, chosen.h, soll, minutes, real, loc, site)
	if chosenEval.err != nil {
		return Candidate{}, false
	}
	gainWeek := (chosenEval.j - todayPt.j) * 7 / nDays
	if !(gainWeek > thresholdMinGainKWhWeek) {
		return Candidate{}, false
	}
	if !thresholdHalvesAgree(ctx, in, r, idx, base, minutes, real, loc, site, days, today, hyst0, run0, soll, chosen.h, chosen.m) {
		return Candidate{}, false
	}

	code := "threshold.too_low"
	kwh := (todayPt.eNetz - chosenEval.eNetz) * 7 / nDays
	if soll < today {
		code = "threshold.too_high"
		kwh = (chosenEval.ePV - todayPt.ePV) * 7 / nDays
	}
	joint := in.Limits.ReplayJointHold
	cand := thresholdCandidate(in, r, device, p, code, kwh, gainWeek, today, soll, chosen, joint, estimate, nDays, chosenEval)
	cand.Spread = thresholdSpread(minutes, loc, r.Conditions[idx].Field)
	return cand, true
}

// thresholdTarget holds what does not change over the grid of one rule.
type thresholdTarget struct {
	rule    string
	device  string
	opW     float64
	peakW   float64
	cycleWh float64
}

// thresholdEvalResult is the replay of one point with its terms.
type thresholdEvalResult struct {
	alt, base ReplayResult
	j         float64
	ePV       float64
	eNetz     float64
	err       error
}

// thresholdEval replays one point of the grid on the given minutes: the rule
// with threshold x, hysteresis h and minimum run m. It returns J with its terms.
func thresholdEval(ctx context.Context, in *Input, r Rule, idx int, t thresholdTarget, m, h, x float64, minutes []ReplayMinute, real []bool, loc *time.Location, site sun.Location) thresholdEvalResult {
	target := ReplayTarget{RuleID: t.rule, Device: t.device, OpW: t.opW, PeakW: t.peakW, CycleWh: t.cycleWh, MinRunS: m}
	raw, err := thresholdRuleJSON(r, idx, x, h)
	if err != nil {
		return thresholdEvalResult{err: err}
	}
	base, err := Replay(ctx, replayBaseRule, target, minutes, real, loc, site)
	if err != nil {
		return thresholdEvalResult{err: err}
	}
	alt, err := Replay(ctx, raw, target, minutes, real, loc, site)
	if err != nil {
		return thresholdEvalResult{err: err}
	}
	j, ePV, eNetz := thresholdJ(alt, base)
	return thresholdEvalResult{alt: alt, base: base, j: j, ePV: ePV, eNetz: eNetz}
}

// thresholdRow rates the whole grid for one hysteresis and minimum run.
func thresholdRow(ctx context.Context, in *Input, r Rule, idx int, t thresholdTarget, m, h float64, grid []float64, minutes []ReplayMinute, real []bool, loc *time.Location, site sun.Location) ([]thresholdPoint, error) {
	row := make([]thresholdPoint, 0, len(grid))
	for _, x := range grid {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ev := thresholdEval(ctx, in, r, idx, t, m, h, x, minutes, real, loc, site)
		if ev.err != nil {
			return nil, ev.err
		}
		row = append(row, thresholdPoint{x: x, h: h, m: m, j: ev.j})
	}
	return row, nil
}

// thresholdWidest returns the middle of the widest run of points whose J is
// at least thresholdPlateau of the maximum of the row. Ties keep the first run.
func thresholdWidest(row []thresholdPoint) thresholdPoint {
	maxJ := math.Inf(-1)
	for _, pt := range row {
		maxJ = math.Max(maxJ, pt.j)
	}
	bestStart, bestEnd := -1, -1
	for i := 0; i < len(row); {
		if row[i].j < thresholdPlateau*maxJ {
			i++
			continue
		}
		s := i
		for i < len(row) && row[i].j >= thresholdPlateau*maxJ {
			i++
		}
		e := i - 1
		if bestStart < 0 || row[e].x-row[s].x > row[bestEnd].x-row[bestStart].x {
			bestStart, bestEnd = s, e
		}
	}
	if bestStart < 0 {
		bestStart, bestEnd = 0, 0
	}
	mid := (row[bestStart].x + row[bestEnd].x) / 2
	out := row[bestStart]
	out.x = mid
	return out
}

// thresholdHalvesAgree reports whether the proposal beats today's rule on the
// first and on the second half of the days.
func thresholdHalvesAgree(ctx context.Context, in *Input, r Rule, idx int, t thresholdTarget, minutes []ReplayMinute, real []bool, loc *time.Location, site sun.Location, days []string, today, hyst0, run0, soll, h, m float64) bool {
	split := thresholdSplit(minutes, loc, days)
	parts := [][2]int{{0, split}, {split, len(minutes)}}
	for _, part := range parts {
		lo, hi := part[0], part[1]
		if hi <= lo {
			return false
		}
		mins := minutes[lo:hi]
		rl := real[lo:hi]
		prop := thresholdEval(ctx, in, r, idx, t, m, h, soll, mins, rl, loc, site)
		now := thresholdEval(ctx, in, r, idx, t, run0, hyst0, today, mins, rl, loc, site)
		if prop.err != nil || now.err != nil {
			return false
		}
		if !(prop.j-now.j > 0) {
			return false
		}
	}
	return true
}

// thresholdCandidate builds the finding and the candidate of a rated rule.
func thresholdCandidate(in *Input, r Rule, device string, p float64, code string, kwh, gainWeek, today, soll float64, chosen thresholdPoint, joint, estimate bool, nDays float64, chosenEval thresholdEvalResult) Candidate {
	rules := ruleByID(in.Doc)
	conf := "high"
	if estimate {
		conf = "estimate"
	}
	name := ruleName(rules, r.ID)
	idx, _ := thresholdCondition(r)
	fixes := []Fix{{Code: "set_threshold", RuleID: r.ID, Ops: []Op{{
		Path: []any{"conditions", idx, "threshold"}, Value: soll}}}}
	fixes = append(fixes, Fix{Code: "learned_threshold", RuleID: r.ID, Ops: []Op{{
		Path:  []any{"conditions", idx, "threshold"},
		Value: map[string]any{"learned": "device/" + device + "/op_power_w", "offset": soll - p}}}})
	var facts []Fact
	if joint {
		facts = []Fact{{Code: "hysteresis", Value: chosen.h}, {Code: "min_run_s", Value: chosen.m}}
	}
	benefit := Benefit{Unit: "kwh_week", Value: gainWeek}
	finding := Finding{
		ID:         "threshold:" + r.ID,
		Kind:       "threshold",
		Group:      "check",
		RuleIDs:    []string{r.ID},
		Severity:   "info",
		Confidence: conf,
		Summary: Text{Code: code, Params: map[string]any{
			"rule": name, "kwh": kwh, "threshold": today, "soll": soll}},
		Evidence: Evidence{Shape: "scale", Unit: "W", IstValue: &today, SollValue: &soll},
		Facts:    facts,
		Apply:    fixes,
		Device:   device,
		Benefit:  &benefit,
	}
	replay := &ReplayVerdict{Base: chosenEval.base, Alt: chosenEval.alt, Estimate: estimate}
	return Candidate{
		Finding:     finding,
		Detector:    "threshold_rating",
		NEff:        nDays,
		MinNEff:     0,
		Lower:       1,
		Limit:       1,
		HalvesAgree: true,
		Benefit:     benefit,
		Values:      map[string]float64{"today": today, "soll": soll, "gain_week": gainWeek},
		Replay:      replay,
	}
}

// thresholdCondition returns the index of the surplus condition of a rule: a
// grid export or PV reserve condition with a numeric threshold, above it.
func thresholdCondition(r Rule) (int, bool) {
	for i, c := range r.Conditions {
		if _, ref := c.Learned["threshold"]; ref {
			continue
		}
		if c.Comparison != "above" {
			continue
		}
		if c.Type == "balance_threshold" && c.Field == "grid_export" {
			return i, true
		}
		if c.Type == "learned_value" {
			if ref, ok := c.Learned["learned"]; ok && ref.Key == surWinReserveKey {
				return i, true
			}
		}
	}
	return 0, false
}

// thresholdDevice returns the device the rule switches, by its entity.
func thresholdDevice(in *Input, r Rule) (string, DeviceInfo, bool) {
	ids := make([]string, 0, len(in.Devices))
	for id := range in.Devices {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		info := in.Devices[id]
		if actsOnDevice(r, info) {
			return id, info, true
		}
	}
	return "", DeviceInfo{}, false
}

// thresholdGrid returns steps values between 0.3 and 2 times the threshold.
func thresholdGrid(today float64, steps int) []float64 {
	lo, hi := thresholdLowFactor*today, thresholdHighFactor*today
	out := make([]float64, steps)
	for i := range out {
		out[i] = lo + (hi-lo)*float64(i)/float64(steps-1)
	}
	return out
}

// thresholdJ is J = E_pv - 1.5 * E_netz - 0.02 kWh * N in kWh. E_pv is the
// extra surplus the proposal uses, E_netz the extra grid import it causes.
func thresholdJ(alt, base ReplayResult) (j, ePV, eNetz float64) {
	ePV = math.Max(0, base.ExportWh-alt.ExportWh) / 1000
	eNetz = math.Max(0, alt.GridWh-base.GridWh) / 1000
	j = ePV - thresholdGridPenalty*eNetz - thresholdSwitchKWh*float64(alt.Switches)
	return j, ePV, eNetz
}

// thresholdRuleJSON returns the rule with the threshold x and the hysteresis
// h on condition idx, the other conditions unchanged.
func thresholdRuleJSON(r Rule, idx int, x, h float64) (json.RawMessage, error) {
	conds := make([]json.RawMessage, len(r.Conditions))
	for i, c := range r.Conditions {
		if i != idx {
			conds[i] = c.Raw
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(c.Raw, &m); err != nil {
			return nil, err
		}
		m["threshold"] = x
		m["hysteresis"] = h
		b, err := json.Marshal(m)
		if err != nil {
			return nil, err
		}
		conds[i] = b
	}
	return json.Marshal(map[string]any{
		"id": r.ID, "enabled": true,
		"cooldown_seconds": r.CooldownSeconds, "repeat_seconds": r.RepeatSeconds,
		"conditions": conds,
	})
}

// thresholdDays returns the distinct days of the minutes, oldest first.
func thresholdDays(minutes []ReplayMinute, loc *time.Location) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range minutes {
		d := m.At.In(loc).Format(dayLayout)
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

// thresholdSplit returns the index of the first minute of the second half of the days.
func thresholdSplit(minutes []ReplayMinute, loc *time.Location, days []string) int {
	boundary := days[len(days)/2]
	for i, m := range minutes {
		if m.At.In(loc).Format(dayLayout) >= boundary {
			return i
		}
	}
	return len(minutes)
}

// thresholdSpread is the spread of the values of a threshold proposal. The
// threshold and its proposal move with the daily peak of the signal the rule
// reads, so their spread is 1,4826 · MAD of those daily peaks. The gain is
// judged against the smallest gain that counts.
func thresholdSpread(minutes []ReplayMinute, loc *time.Location, field string) map[string]float64 {
	peaks := map[string]float64{}
	for _, m := range minutes {
		d := m.At.In(loc).Format(dayLayout)
		v := m.Balance[field]
		if p, ok := peaks[d]; !ok || v > p {
			peaks[d] = v
		}
	}
	xs := make([]float64, 0, len(peaks))
	for _, v := range peaks {
		xs = append(xs, v)
	}
	dailySpread := MADScale(xs)
	return map[string]float64{"today": dailySpread, "soll": dailySpread, "gain_week": thresholdMinGainKWhWeek}
}
