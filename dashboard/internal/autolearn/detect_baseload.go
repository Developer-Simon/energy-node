package autolearn

import (
	"context"
	"math"
	"sort"
	"time"
)

// BaseLoadDetector watches the night load of the house. Each night is the 10th
// percentile of the load between the night rest and the day start of its day
// type. A sustained rise above the expectation is a finding, with the device
// that accounts for at least half of the rise when one is measured. A fall is
// no finding. The learned site/base_w follows the level of the last 14 nights.
// It is daily and not per device.
var BaseLoadDetector = Detector{
	Name:      "baseload",
	Group:     "energy",
	Light:     false,
	PerDevice: false,
	Run:       baseLoadRun,
}

const (
	// baseloadRefNights is the reference of the drift without an earlier year:
	// the median of the first nights of the window. A reference that followed
	// the last nights would absorb a lasting rise after two weeks.
	baseloadRefNights = 28
	// baseloadWindow is the number of nights the drift is looked for in.
	baseloadWindow = 90
	// baseloadMinNights is the number of nights for a drift: the reference and
	// seven more. The spec gives no number, the value is an assumption.
	baseloadMinNights = 35
	// baseloadSeasonMinNights is the number of nights for a drift when every
	// night has its expectation from the earlier years. The reference of the
	// window is not needed then.
	baseloadSeasonMinNights = 14
	// baseloadMinYears is the number of earlier weeks that make the expectation
	// of a week, the earlier years of the season store.
	baseloadMinYears = 3
	// baseloadLearnNights is the number of nights of the learned site/base_w.
	baseloadLearnNights = 14
	// baseloadLearnMin is the number of nights a learned site/base_w needs.
	baseloadLearnMin = 7
	// baseloadMinSigmaW is the smallest spread in W, so that a flat series does
	// not turn every small step into a drift.
	baseloadMinSigmaW = 10.0
	// baseloadRoundW rounds the base load and the rise in W.
	baseloadRoundW = 10.0
	// baseloadHours is the hours of a week and of a month for the benefit.
	baseloadWeekH  = 168.0
	baseloadMonthH = 720.0
)

// baseloadNight is one night of a day file that has all its quarters seen.
type baseloadNight struct {
	date  string
	week  int
	group int
	night float64
	// dev is the average power of each device with a record in the night, W.
	dev map[string]float64
}

// baseLoadRun returns the drift finding and the learned site/base_w.
func baseLoadRun(ctx context.Context, in *Input, _ string) []Candidate {
	if ctx.Err() != nil {
		return nil
	}
	ns := baseloadNights(in)
	var out []Candidate
	if g, ok := learnTodayGroup(in); ok {
		var same []float64
		for _, n := range ns {
			if n.group == g {
				same = append(same, n.night)
			}
		}
		if len(same) > baseloadLearnNights {
			same = same[len(same)-baseloadLearnNights:]
		}
		if len(same) >= baseloadLearnMin {
			out = append(out, Candidate{
				Detector: "baseload",
				NEff:     float64(len(same)),
				MinNEff:  baseloadLearnMin,
				Value: &Value{Key: "site/base_w", Kind: ValueNumber, Unit: "W", Group: "energy",
					Num: baseloadRound(Median(same), baseloadRoundW), N: len(same)},
			})
		}
	}
	if len(ns) >= baseloadMinNights || (len(ns) >= baseloadSeasonMinNights && baseloadSeasonKnown(in, ns)) {
		if len(ns) > baseloadWindow {
			ns = ns[len(ns)-baseloadWindow:]
		}
		out = append(out, baseLoadDrift(in, ns)...)
	}
	return out
}

// baseloadNights reads the nights of the day files in date order. Today, days
// without a day type, bounds or a night quarter that was not seen are left out.
func baseloadNights(in *Input) []baseloadNight {
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	today := in.Now.In(loc).Format(dayLayout)
	days := make([]*DayFile, 0, len(in.Days))
	for i := range in.Days {
		days = append(days, &in.Days[i])
	}
	sort.SliceStable(days, func(a, b int) bool { return days[a].Day < days[b].Day })
	var out []baseloadNight
	for _, f := range days {
		if f.Day == today {
			continue
		}
		g, ok := in.Base.DayType[f.Day]
		if !ok {
			continue
		}
		ds, okS := in.Base.DayStart[g]
		nr, okN := in.Base.NightRest[g]
		if !okS || !okN {
			continue
		}
		t, err := time.ParseInLocation(dayLayout, f.Day, loc)
		if err != nil {
			continue
		}
		var quarters []int
		var load []float64
		seen := true
		for q := 0; q < dayQuarters; q++ {
			if !baseloadNightQuarter(q, ds, nr) {
				continue
			}
			quarters = append(quarters, q)
			if f.Site.Seen[q] == 0 {
				seen = false
			}
			load = append(load, float64(f.Site.LoadTotal[q]))
		}
		if !seen || len(load) == 0 {
			continue
		}
		_, week := t.ISOWeek()
		dev := map[string]float64{}
		for id, dd := range f.Devices {
			if dd == nil {
				continue
			}
			sum, n := 0.0, 0
			for _, q := range quarters {
				if dd.Seen[q] != 0 {
					sum += float64(dd.AvgW[q])
					n++
				}
			}
			if n > 0 {
				dev[id] = sum / float64(n)
			}
		}
		out = append(out, baseloadNight{date: f.Day, week: week, group: g, night: basePercentile10(load), dev: dev})
	}
	return out
}

// baseloadNightQuarter reports whether quarter q lies in the night: from the
// night rest to the end of the day, and from the start of the day to the day start.
func baseloadNightQuarter(q int, dayStart, nightRest float64) bool {
	m := float64(q) * baseQuarterMin
	return m < dayStart || m >= nightRest
}

// baseLoadDrift looks for a sustained rise of the nights above their
// expectation and names the device behind it. The expectation is the median
// of the same weeks of the earlier years when the season store has them, else
// the reference of the window. The spread is the MAD of the nights.
func baseLoadDrift(in *Input, ns []baseloadNight) []Candidate {
	exp := baseloadExpect(in, ns)
	loads := make([]float64, len(ns))
	for i, n := range ns {
		loads[i] = n.night
	}
	sigma := math.Max(MADScale(loads), baseloadMinSigmaW)
	z := make([]float64, len(ns))
	for i, n := range ns {
		z[i] = (n.night - exp[i]) / sigma
	}
	at, rising, ok := CUSUM(z, 0, 1)
	if !ok || at < 1 {
		return nil
	}
	var before, after []float64
	for i, n := range ns {
		if i < at {
			before = append(before, n.night)
		} else {
			after = append(after, n.night)
		}
	}
	shift := Median(after) - Median(before)
	if shift == 0 || rising != (shift > 0) {
		return nil
	}
	if !rising {
		return baseloadFall(ns, at, before, after, shift)
	}
	device := baseloadDevice(in, ns, at, shift)
	date := ns[at].date
	w := baseloadRound(shift, baseloadRoundW)
	benefit := Benefit{Unit: "kwh_week", Value: shift * baseloadWeekH / 1000}
	candidate := ""
	if device != "" {
		candidate = deviceName(in.Devices[device], device)
	}
	after0, before0 := Median(after), Median(before)
	finding := Finding{
		ID:         FindingID("baseload", date),
		Kind:       "baseload",
		Group:      "energy",
		Severity:   "info",
		Confidence: "high",
		Summary: Text{Code: "baseload.drift", Params: map[string]any{
			"w": w, "kwh_month": round1(shift * baseloadMonthH / 1000), "candidate": candidate,
		}},
		Evidence: Evidence{Shape: "scale", Unit: "W", IstValue: &after0, SollValue: &before0},
		Device:   device,
		Benefit:  &benefit,
	}
	if device != "" && in.Devices[device].Switchable {
		finding.Apply = []Fix{{Code: "night_off", Template: "night_off",
			Prefill: withEntity(map[string]any{"device": device}, "entity", switchEntityOf(in, device))}}
	}
	// Without a switchable culprit there is no rule to suggest: the finding goes
	// to the rule check and from there to Monitoring as a notice.
	if len(finding.Apply) == 0 {
		finding.Group = "check"
	}
	return []Candidate{{
		Finding:     finding,
		Detector:    "baseload",
		NEff:        float64(len(after)),
		MinNEff:     baseloadLearnMin,
		Lower:       1,
		Limit:       1,
		HalvesAgree: true,
		Benefit:     benefit,
	}}
}

// baseloadFall reports a sustained fall of the nights from before to after. It
// names no device and suggests no rule: a lower base load is a notice for the
// monitoring, with no saving to win back, so the benefit stays empty.
func baseloadFall(ns []baseloadNight, at int, before, after []float64, shift float64) []Candidate {
	w := baseloadRound(-shift, baseloadRoundW)
	after0, before0 := Median(after), Median(before)
	finding := Finding{
		ID:         FindingID("baseload", ns[at].date),
		Kind:       "baseload",
		Group:      "check",
		Severity:   "info",
		Confidence: "high",
		Summary: Text{Code: "baseload.drift.down", Params: map[string]any{
			"w": w, "kwh_month": round1(-shift * baseloadMonthH / 1000),
		}},
		Evidence: Evidence{Shape: "scale", Unit: "W", IstValue: &after0, SollValue: &before0},
	}
	return []Candidate{{
		Finding:     finding,
		Detector:    "baseload",
		NEff:        float64(len(after)),
		MinNEff:     baseloadLearnMin,
		Lower:       1,
		Limit:       1,
		HalvesAgree: true,
	}}
}

// baseloadExpect returns the expected night load of each night. With the same
// weeks of baseloadMinYears earlier years in the season store that is their
// median, else the reference of the window.
func baseloadExpect(in *Input, ns []baseloadNight) []float64 {
	loads := make([]float64, 0, baseloadRefNights)
	for i := 0; i < len(ns) && i < baseloadRefNights; i++ {
		loads = append(loads, ns[i].night)
	}
	ref := Median(loads)
	exp := make([]float64, len(ns))
	for i, n := range ns {
		exp[i] = ref
		if in.Season == nil {
			continue
		}
		var vals []float64
		for _, w := range in.Season.Same(n.week, baseloadMinYears) {
			if w.BaseW > 0 {
				vals = append(vals, w.BaseW)
			}
		}
		if len(vals) >= baseloadMinYears {
			exp[i] = Median(vals)
		}
	}
	return exp
}

// baseloadSeasonKnown reports whether every night has its expectation from the
// same weeks of baseloadMinYears earlier years, as baseloadExpect uses it.
func baseloadSeasonKnown(in *Input, ns []baseloadNight) bool {
	if in.Season == nil || len(ns) == 0 {
		return false
	}
	for _, n := range ns {
		count := 0
		for _, w := range in.Season.Same(n.week, baseloadMinYears) {
			if w.BaseW > 0 {
				count++
			}
		}
		if count < baseloadMinYears {
			return false
		}
	}
	return true
}

// baseloadDevice returns the device whose average night power rose by at least
// half of the shift from the nights before at to the nights from at on, the one
// with the largest rise. The empty string when no device qualifies.
func baseloadDevice(in *Input, ns []baseloadNight, at int, shift float64) string {
	best, bestRise := "", 0.0
	for _, id := range sortedDeviceIDs(in.Devices) {
		var pre, post []float64
		for i, n := range ns {
			v, ok := n.dev[id]
			if !ok {
				continue
			}
			if i < at {
				pre = append(pre, v)
			} else {
				post = append(post, v)
			}
		}
		if len(post) == 0 {
			continue
		}
		if len(pre) == 0 {
			pre = []float64{0}
		}
		rise := Median(post) - Median(pre)
		if rise >= shift/2 && rise > bestRise {
			best, bestRise = id, rise
		}
	}
	return best
}

// baseloadRound rounds v to a multiple of step.
func baseloadRound(v, step float64) float64 {
	return math.Round(v/step) * step
}
