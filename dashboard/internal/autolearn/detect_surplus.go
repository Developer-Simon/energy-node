package autolearn

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// SurplusDetector suggests running a load in the typical surplus window of the
// day (spec 15.6 "Ueberschussfenster"). The surplus of a quarter hour is grid
// export plus PV reserve plus battery charge above 200 W. The window is the
// longest run of quarter hours in which the surplus reaches the operating
// power plus the reserve on at least half of the days by the Wilson bound.
var SurplusDetector = Detector{
	Name:      "surplus_window",
	Group:     "energy",
	Light:     false,
	PerDevice: true,
	Run:       surWinWindowRun,
}

const (
	// surWinWindowDays is the number of recent days the window reads.
	surWinWindowDays = 14
	// surWinWindowLimit is the Wilson lower bound a quarter hour needs.
	surWinWindowLimit = 0.5
	// surWinWindowMinNEff is the minimum effective number of days.
	surWinWindowMinNEff = 10.0
	// surWinWindowMinReserveDays is the number of days the reserve must make
	// the window, from the last surWinWindowDays days.
	surWinWindowMinReserveDays = 5
	// surWinReserveMarginW is the reserve added to the start peak and the
	// operating power. The spec gives no number, the value is an assumption.
	surWinReserveMarginW = 200.0
	// surWinBatteryFloorW is the battery charge that counts as surplus.
	surWinBatteryFloorW = 200.0
	// surWinRoundW rounds the proposed threshold.
	surWinRoundW = 100.0
	// surWinHoldS is the hold of the on condition in seconds, as the templates set it.
	surWinHoldS = 120
	// surWinReserveKey is the learned key of the PV reserve.
	surWinReserveKey = "site/pv_reserve_w"
)

// surWinDay is one day the window reads: the surplus per quarter hour with
// and without the PV reserve, and the quarters the day has a measurement for.
type surWinDay struct {
	date    string
	weight  float64
	seen    [dayQuarters]bool
	with    [dayQuarters]float64
	without [dayQuarters]float64
}

// surWinWindowRun finds the surplus window of one device and proposes a rule
// that switches it on there. It needs the learned operating power and the
// cycle energy of the device, and it stays silent when a rule already switches
// the device on surplus.
func surWinWindowRun(ctx context.Context, in *Input, device string) []Candidate {
	if ctx.Err() != nil {
		return nil
	}
	p, ok := learnedOperatingPower(in.Learned, device)
	if !ok {
		return nil
	}
	cycle, ok := surWinLearnedNumber(in.Learned, "device/"+device+"/cycle_wh")
	if !ok || cycle <= 0 {
		return nil
	}
	if surWinHasRule(in, device) {
		return nil
	}
	days := surWinDays(in)
	if len(days) == 0 {
		return nil
	}
	weights := make([]float64, 0, len(days))
	for _, d := range days {
		weights = append(weights, d.weight)
	}
	neff := NEff(weights)
	if neff < surWinWindowMinNEff {
		return nil
	}
	need := int(math.Ceil(cycle / p * 4))
	if need < 1 {
		return nil
	}
	threshold := p + surWinReserveMarginW

	source := "export"
	start, n, lower, rate := surWinLongestWindow(days, threshold, func(d *surWinDay) *[dayQuarters]float64 { return &d.without })
	if n < need {
		start2, n2, lower2, rate2 := surWinLongestWindow(days, threshold, func(d *surWinDay) *[dayQuarters]float64 { return &d.with })
		if n2 < need || !surWinReserveHelps(days, threshold, need) {
			return nil
		}
		source = "reserve"
		start, n, lower, rate = start2, n2, lower2, rate2
	}
	return []Candidate{surWinCandidate(ctx, in, device, p, cycle, source, start, n, lower, rate, days, neff)}
}

// surWinCandidate builds the finding for the window and checks it by replay.
func surWinCandidate(ctx context.Context, in *Input, device string, p, cycle float64, source string, start, n int, lower, rate float64, days []surWinDay, neff float64) Candidate {
	peak := feedbackInrush(in.Days, device, p)
	onW := math.Round((peak+surWinReserveMarginW)/surWinRoundW) * surWinRoundW
	runS := int(cycle / p * 3600)
	code := "surplus_switch"
	if in.Devices[device].Role == energy.RoleWallbox {
		code = "surplus_wallbox"
	}
	id := "surplus:" + device
	clockFrom, clockTo := surWinClock(start), surWinClock(start+n)

	benefit := Benefit{Unit: "kwh_week", Value: cycle / 1000 * rate * 7}
	conf := "high"
	if len(days) < surWinWindowDays {
		conf = "estimate"
	}
	prefill := map[string]any{"device": device, "on_w": onW, "source": source, "min_run_s": runS}
	finding := Finding{
		ID:         id,
		Kind:       "surplus_window",
		Group:      "energy",
		Severity:   "info",
		Confidence: conf,
		Summary: Text{Code: "surplus.window", Params: map[string]any{
			"device": device, "on_w": onW, "from": clockFrom, "to": clockTo, "days": len(days)}},
		Evidence: Evidence{Shape: "curve", From: int64(start * 15), To: int64((start + n) * 15), Soll: []int64{int64(onW)}},
		Facts: []Fact{
			{Code: "window_min", Value: n * 15},
			{Code: "run_min", Value: runS / 60},
			{Code: "source", Value: source},
		},
		Apply:   []Fix{{Code: code, Template: code, Prefill: prefill}},
		Device:  device,
		Benefit: &benefit,
	}
	if source == "reserve" {
		finding.Summary.Code = "surplus.reserve"
	}
	cand := Candidate{
		Finding:     finding,
		Detector:    "surplus_window",
		NEff:        neff,
		MinNEff:     surWinWindowMinNEff,
		Lower:       lower,
		Limit:       surWinWindowLimit,
		HalvesAgree: true,
		Benefit:     benefit,
		Values:      map[string]float64{"window_quarters": float64(n), "on_w": onW},
	}
	if verdict, b, err := surWinReplay(ctx, in, device, id, onW, source, p, peak, cycle, runS); err == nil && verdict != nil {
		cand.Replay = verdict
		cand.Benefit = b
		finding.Benefit = &b
		cand.Finding = finding
	}
	return cand
}

// surWinReplay checks the proposed rule by replay. A rule that cannot be
// replayed, or a replay without minutes, leaves the candidate without a replay.
func surWinReplay(ctx context.Context, in *Input, device, id string, onW float64, source string, p, peak, cycle float64, runS int) (*ReplayVerdict, Benefit, error) {
	if in.Doc.Location == nil {
		return nil, Benefit{}, nil
	}
	rule := map[string]any{"id": id, "enabled": true}
	if source == "reserve" {
		rule["conditions"] = []map[string]any{{"type": "learned_value", "learned": surWinReserveKey,
			"comparison": "above", "threshold": onW, "fallback": 0, "hysteresis": 100,
			"max_age_s": 1800, "hold_seconds": surWinHoldS}}
	} else {
		rule["conditions"] = []map[string]any{{"type": "balance_threshold", "field": "grid_export",
			"comparison": "above", "threshold": onW, "hold_seconds": surWinHoldS}}
	}
	raw, err := json.Marshal(rule)
	if err != nil {
		return nil, Benefit{}, err
	}
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	chk := ReplayCheck{
		Rule:   json.RawMessage(raw),
		Target: ReplayTarget{RuleID: id, Device: device, OpW: p, PeakW: peak, CycleWh: cycle, MinRunS: float64(runS)},
		Benefit: func(alt, base ReplayResult) Benefit {
			// The energy the load takes from the export, per week.
			return Benefit{Unit: "kwh_week", Value: (base.ExportWh - alt.ExportWh) / 1000 * 7 / surWinWindowDays}
		},
	}
	verdict, b, err := EvaluateReplay(ctx, in, chk, surWinWindowDays, loc, *in.Doc.Location)
	if ctx.Err() != nil {
		return nil, Benefit{}, ctx.Err()
	}
	return verdict, b, err
}

// surWinDays returns the last surWinWindowDays day files with a measurement,
// oldest first. Days the house was away are left out.
func surWinDays(in *Input) []surWinDay {
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	files := make([]*DayFile, 0, len(in.Days))
	for i := range in.Days {
		files = append(files, &in.Days[i])
	}
	sort.SliceStable(files, func(a, b int) bool { return files[a].Day > files[b].Day })

	var out []surWinDay
	for _, f := range files {
		if len(out) == surWinWindowDays {
			break
		}
		if in.Base.Away[f.Day] {
			continue
		}
		t, err := time.ParseInLocation(dayLayout, f.Day, loc)
		if err != nil {
			continue
		}
		d := surWinDay{date: f.Day, weight: DayWeight(math.Max(0, in.Now.Sub(t).Hours()/24))}
		hasData := false
		d.with, d.without = surWinSeries(in, f, t)
		for q := 0; q < dayQuarters; q++ {
			d.seen[q] = f.Site.Seen[q] != 0
			hasData = hasData || d.seen[q]
		}
		if hasData {
			out = append(out, d)
		}
	}
	// The days are read newest first above, the window reads them oldest first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// surWinSeries returns the surplus per quarter hour of one day, with and
// without the PV reserve. The reserve is max(0, Best * k - PV) with the
// weather factor k of the quarter, as the rest surplus does it. Without a PV
// model or a site the reserve is 0.
func surWinSeries(in *Input, f *DayFile, midnight time.Time) (with, without [dayQuarters]float64) {
	var model PVModel
	var raster *Raster
	haveModel := false
	if in.PV != nil {
		model, raster, haveModel = in.PV()
	}
	site := in.Doc.Location
	haveModel = haveModel && site != nil
	for q := 0; q < dayQuarters; q++ {
		exp := float64(f.Site.GridExport[q])
		bat := math.Max(0, float64(f.Site.BatteryCharge[q])-surWinBatteryFloorW)
		without[q] = exp + bat
		res := 0.0
		if haveModel {
			az, el := sun.Position(shadeQuarterTime(midnight, q), site.Lat, site.Lon)
			k := 0.0
			if kv := f.Site.K[q]; kv != nil {
				k = float64(*kv)
			}
			res = math.Max(0, model.Best(az, el, raster)*k-float64(f.Site.PV[q]))
		}
		with[q] = without[q] + res
	}
	return with, without
}

// surWinLongestWindow returns the longest run of quarter hours whose Wilson
// lower bound of "surplus reaches the threshold" is at least
// surWinWindowLimit, the first quarter of the run, its length, the lowest
// bound in it and the mean share of days in it.
func surWinLongestWindow(days []surWinDay, threshold float64, pick func(*surWinDay) *[dayQuarters]float64) (start, n int, lower, rate float64) {
	var good [dayQuarters]bool
	var low, share [dayQuarters]float64
	for q := 0; q < dayQuarters; q++ {
		hits, seen := 0, 0
		for i := range days {
			if !days[i].seen[q] {
				continue
			}
			seen++
			if pick(&days[i])[q] >= threshold {
				hits++
			}
		}
		if seen == 0 {
			continue
		}
		low[q] = WilsonLower(hits, seen)
		share[q] = float64(hits) / float64(seen)
		good[q] = low[q] >= surWinWindowLimit
	}
	bestStart, bestN := 0, 0
	for q := 0; q < dayQuarters; {
		if !good[q] {
			q++
			continue
		}
		s := q
		for q < dayQuarters && good[q] {
			q++
		}
		if q-s > bestN {
			bestStart, bestN = s, q-s
		}
	}
	if bestN == 0 {
		return 0, 0, 0, 0
	}
	lower = 1
	for q := bestStart; q < bestStart+bestN; q++ {
		lower = math.Min(lower, low[q])
		rate += share[q]
	}
	return bestStart, bestN, lower, rate / float64(bestN)
}

// surWinReserveHelps reports whether the reserve makes the window on at least
// surWinWindowMinReserveDays days, where the surplus without the reserve is
// shorter than the run.
func surWinReserveHelps(days []surWinDay, threshold float64, need int) bool {
	count := 0
	for i := range days {
		with := longestRun(&days[i].with, threshold)
		without := longestRun(&days[i].without, threshold)
		if with >= need && without < need {
			count++
		}
	}
	return count >= surWinWindowMinReserveDays
}

// longestRun is the longest run of quarter hours at or above threshold.
func longestRun(xs *[dayQuarters]float64, threshold float64) int {
	best, cur := 0, 0
	for _, x := range xs {
		if x >= threshold {
			cur++
			best = max(best, cur)
		} else {
			cur = 0
		}
	}
	return best
}

// surWinHasRule reports whether an enabled rule already switches the device on
// surplus: a grid export or PV reserve condition and an action on one of the
// device entities.
func surWinHasRule(in *Input, device string) bool {
	entities := in.Devices[device].Entities
	if len(entities) == 0 {
		return false
	}
	own := make(map[string]bool, len(entities))
	for _, e := range entities {
		own[e] = true
	}
	for _, r := range in.Doc.Rules {
		if !r.Enabled || !surWinRuleOnSurplus(r) {
			continue
		}
		for _, a := range r.Actions {
			if own[a.EntityID] {
				return true
			}
		}
	}
	return false
}

// surWinRuleOnSurplus reports whether a rule has a surplus condition.
func surWinRuleOnSurplus(r Rule) bool {
	for _, c := range r.Conditions {
		if c.Type == "balance_threshold" && c.Field == "grid_export" {
			return true
		}
		if c.Type == "learned_value" {
			for _, ref := range c.Learned {
				if ref.Key == surWinReserveKey {
					return true
				}
			}
		}
	}
	return false
}

// surWinLearnedNumber reads a learned number, false when it is missing.
func surWinLearnedNumber(learned map[string]LearnedEntry, key string) (float64, bool) {
	e, ok := learned[key]
	if !ok {
		return 0, false
	}
	return asFloat(e.Value)
}

// surWinClock formats a quarter hour index as HH:MM.
func surWinClock(q int) string {
	m := q * 15
	return fmt.Sprintf("%02d:%02d", m/60%24, m%60)
}
