package autolearn

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// OverriddenDetector flags a rule that the user keeps reversing by hand or from
// outside soon after the rule switched (spec 15.4 "Überstimmt"). The fix adds
// the condition that separates the reversed switches from the accepted ones, or
// moves the start of the rule's time window to the usual switch-back time.
var OverriddenDetector = Detector{
	Name:      "overridden",
	Group:     "reliability",
	Light:     false,
	PerDevice: true,
	Run:       overriddenRun,
}

const (
	// overriddenMinCases is the number of reversed switches a finding needs.
	overriddenMinCases = 5
	// overriddenLimit is the lower Wilson bound of the reversed share a finding needs.
	overriddenLimit = 0.4
	// overriddenFallbackSec is the time limit when the distances show no valley.
	overriddenFallbackSec = 600.0
	// overriddenBins is the histogram size of the valley search.
	overriddenBins = 20
	// overriddenMinSide is the number of cases each side of a split needs.
	overriddenMinSide = 4
	// overriddenMinSeparation is the share of cases a split must classify right.
	overriddenMinSeparation = 0.8
	// overriddenRoundW rounds the threshold of a load or surplus condition in watts.
	overriddenRoundW = 50.0
	// overriddenRoundMin rounds the proposed start of a time window in minutes.
	overriddenRoundMin = 5.0
	// overriddenSunOffsetMax is the largest sun offset in minutes the rule schema accepts.
	overriddenSunOffsetMax = 240
	// overriddenMaxEvidence caps the reversal times kept as evidence.
	overriddenMaxEvidence = 50
)

// Names of the features a split can use.
const (
	overriddenDayType   = "day_type"
	overriddenHour      = "hour"
	overriddenHouseLoad = "house_load"
	overriddenSurplus   = "surplus"
	overriddenToSunset  = "to_sunset"
	overriddenElevation = "sun_elevation"
	overriddenSignal    = "signal"
)

// overriddenSunSearchMin is the half width in minutes of the search for the
// elevation distance around the sunset.
const overriddenSunSearchMin = 240

// overrideSwitch is a switch of the device caused by a rule.
type overrideSwitch struct {
	at      int64
	on      bool
	rule    string
	house   float64
	surplus float64
}

// overrideManual is a switch of the device by hand or from outside.
type overrideManual struct {
	at int64
	on bool
}

// overrideCase is one switch of a rule with the state of the world at the
// time, and whether the user reversed it within the time limit.
type overrideCase struct {
	at         int64
	on         bool
	group      int
	hasGroup   bool
	hour       float64
	house      float64
	surplus    float64
	reversed   bool
	reverseMin float64
	// sunOK is set when the location gives the sun state of the switch. It is
	// false without a location and on polar days.
	sunOK     bool
	toSunset  float64
	sunset    time.Time
	elevation float64
	// hasSignal is set when the rule has a balance condition and its value at
	// the switch is known. signal is that value in W.
	hasSignal bool
	signal    float64
}

// overridePoint is one case of a numeric feature.
type overridePoint struct {
	x  float64
	ov bool
}

// overrideSplit is the split of a feature with the largest information gain.
// For the day type, group is the group on the left side and groups the
// accepted groups. For the numeric features, cut separates left from right.
type overrideSplit struct {
	feature      string
	group        int
	groups       []int
	cut          float64
	acceptedLeft bool
	gain         float64
	acc          float64
}

// overriddenRun finds the rules of the device that the user reverses in time
// often enough, one candidate per rule.
func overriddenRun(ctx context.Context, in *Input, device string) []Candidate {
	if ctx.Err() != nil {
		return nil
	}
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	rules := ruleByID(in.Doc)
	switches, manual := overrideSwitches(in.Days, device)
	seen := map[string]bool{}
	var ids []string
	for _, s := range switches {
		if !seen[s.rule] {
			seen[s.rule] = true
			ids = append(ids, s.rule)
		}
	}
	sort.Strings(ids)
	var out []Candidate
	for _, id := range ids {
		if ctx.Err() != nil {
			return nil
		}
		rule, ok := rules[id]
		if !ok {
			continue
		}
		if c, ok := overriddenCandidate(device, in, rule, switches, manual, loc); ok {
			out = append(out, c)
		}
	}
	return out
}

// overrideSwitches collects the rule switches and the hand and external
// switches of the device, both sorted by time.
func overrideSwitches(days []DayFile, device string) ([]overrideSwitch, []overrideManual) {
	var rule []overrideSwitch
	var manual []overrideManual
	for i := range days {
		for _, e := range days[i].Events {
			if e.Device != device {
				continue
			}
			switch Origin(e.Origin) {
			case OriginRule:
				if e.Rule != "" {
					rule = append(rule, overrideSwitch{at: e.At, on: e.On, rule: e.Rule, house: e.House, surplus: e.Surplus})
				}
			case OriginHand, OriginExternal:
				manual = append(manual, overrideManual{at: e.At, on: e.On})
			}
		}
	}
	sort.SliceStable(rule, func(a, b int) bool { return rule[a].at < rule[b].at })
	sort.SliceStable(manual, func(a, b int) bool { return manual[a].at < manual[b].at })
	return rule, manual
}

// overrideDistances returns, per rule switch, the seconds to the first hand or
// external switch in the opposite direction before the next rule switch of the
// device, or -1 when there is none.
func overrideDistances(sw []overrideSwitch, manual []overrideManual) []int64 {
	out := make([]int64, len(sw))
	for i, s := range sw {
		out[i] = -1
		next := int64(math.MaxInt64)
		if i+1 < len(sw) {
			next = sw[i+1].at
		}
		k := sort.Search(len(manual), func(k int) bool { return manual[k].at >= s.at })
		for ; k < len(manual) && manual[k].at < next; k++ {
			if manual[k].on != s.on {
				out[i] = manual[k].at - s.at
				break
			}
		}
	}
	return out
}

// overrideFirst returns the first switch at or after from in the direction on
// that lies within sec seconds of from.
func overrideFirst(manual []overrideManual, from int64, on bool, sec float64) (int64, bool) {
	k := sort.Search(len(manual), func(k int) bool { return manual[k].at >= from })
	for ; k < len(manual) && float64(manual[k].at-from) <= sec; k++ {
		if manual[k].on == on {
			return manual[k].at, true
		}
	}
	return 0, false
}

// overriddenDeadline learns the time limit from the valley of the logarithmic
// distances. Without a valley it is overriddenFallbackSec.
func overriddenDeadline(dists []float64) float64 {
	cut, ok := OtsuSplit(dists, overriddenBins)
	if !ok {
		return overriddenFallbackSec
	}
	return math.Exp(cut)
}

// overriddenCandidate builds the candidate of one rule, or reports false when
// the rule fails the gates.
func overriddenCandidate(device string, in *Input, rule Rule, sw []overrideSwitch, manual []overrideManual, loc *time.Location) (Candidate, bool) {
	dist := overrideDistances(sw, manual)
	var idx []int
	var logs []float64
	for i, s := range sw {
		if s.rule != rule.ID {
			continue
		}
		idx = append(idx, i)
		if dist[i] >= 0 {
			logs = append(logs, math.Log(math.Max(float64(dist[i]), 1)))
		}
	}
	n := len(idx)
	if n < overriddenMinCases {
		return Candidate{}, false
	}
	limit := overriddenDeadline(logs)

	cases := make([]overrideCase, n)
	hits := 0
	signalField, hasField := overriddenSignalField(rule)
	days := make(map[string]*DayFile, len(in.Days))
	for i := range in.Days {
		days[in.Days[i].Day] = &in.Days[i]
	}
	for j, i := range idx {
		s := sw[i]
		c := overrideCase{at: s.at, on: s.on, house: s.house, surplus: s.surplus}
		at := time.Unix(s.at, 0).In(loc)
		c.hour = float64(at.Hour())
		if g, ok := in.Base.DayType[at.Format(dayLayout)]; ok {
			c.group, c.hasGroup = g, true
		}
		if rev, ok := overrideFirst(manual, s.at, !s.on, limit); ok {
			c.reversed = true
			c.reverseMin = minuteOfDay(rev, loc)
			hits++
		}
		if hasField {
			c.signal, c.hasSignal = overrideSignal(in, signalField, at, loc, days)
		}
		if in.Doc.Location != nil {
			if m, set, e, ok := overrideSunState(at, in.Doc.Location); ok {
				c.sunOK, c.toSunset, c.sunset, c.elevation = true, m, set, e
			}
		}
		cases[j] = c
	}
	if hits < overriddenMinCases {
		return Candidate{}, false
	}
	lower := WilsonLower(hits, n)
	if lower < overriddenLimit {
		return Candidate{}, false
	}
	nulls := ShiftNull(func(shiftDays int) int {
		count := 0
		for _, i := range idx {
			s := sw[i]
			shifted := time.Unix(s.at, 0).In(loc).AddDate(0, 0, shiftDays).Unix()
			if _, ok := overrideFirst(manual, shifted, !s.on, limit); ok {
				count++
			}
		}
		return count
	})
	for _, v := range nulls {
		if hits <= v {
			return Candidate{}, false
		}
	}

	// A split that cannot be turned into a fix is not reported, the rule falls
	// back to the shift or to no fix.
	feature := "none"
	var apply []Fix
	var soll []int64
	var sollSpans [][2]int64
	if split, ok := overriddenSplit(cases, in.Base); ok {
		// A split the rule already holds is not offered again.
		if fix, spans, ok := overriddenAddFix(rule, in.Base, split, cases, in.Doc.Location); ok && !overriddenFixHeld(split.feature, fix, in.Doc) {
			feature = split.feature
			apply = []Fix{fix}
			sollSpans = spans
		}
	}
	if apply == nil {
		if fix, med, ok := overriddenShiftFix(rule, cases); ok {
			feature = "clock"
			apply = []Fix{fix}
			soll = []int64{med}
		}
	}

	share := math.Round(float64(hits)/float64(n)*100) / 100
	name := rule.Name
	if name == "" {
		name = rule.ID
	}
	devName := device
	if info, ok := in.Devices[device]; ok && info.Name != "" {
		devName = info.Name
	}
	var ist []int64
	for _, c := range cases {
		if c.reversed {
			ist = append(ist, int64(c.reverseMin))
		}
	}
	if len(ist) > overriddenMaxEvidence {
		ist = ist[len(ist)-overriddenMaxEvidence:]
	}
	finding := Finding{
		ID:         FindingID("overridden", rule.ID) + ":" + device,
		Kind:       "overridden",
		Group:      "check",
		RuleIDs:    []string{rule.ID},
		Severity:   "info",
		Confidence: "high",
		Summary: Text{Code: "check.overridden", Params: map[string]any{
			"rule":     name,
			"device":   devName,
			"failures": hits,
			"share":    share,
			"period_s": int(math.Round(limit)),
			"feature":  feature,
		}},
		Evidence: Evidence{Shape: "day", From: 0, To: int64(minutesPerDay), Ist: ist, Soll: soll, SollSpans: sollSpans},
		Facts: []Fact{
			{Code: "limit_s", Value: int(math.Round(limit))},
			{Code: "feature", Value: feature},
		},
		Apply:  apply,
		Device: device,
	}
	return Candidate{
		Finding:  finding,
		Detector: "overridden",
		NEff:     float64(n),
		MinNEff:  overriddenMinCases,
		Lower:    lower,
		Limit:    overriddenLimit,
		Values:   map[string]float64{"share": share},
	}, true
}

// overriddenSplit searches the feature whose split separates the reversed cases
// from the accepted ones with the largest information gain among the splits
// that pass the gates of overrideEvaluate: each side needs overriddenMinSide
// cases and the split needs overriddenMinSeparation accuracy. The accuracy of
// the split is returned with it.
func overriddenSplit(cases []overrideCase, base Base) (overrideSplit, bool) {
	var best overrideSplit
	found := false
	consider := func(s overrideSplit) {
		if !found || s.gain > best.gain {
			best, found = s, true
		}
	}

	// Day type: one group against all others.
	groupSet := map[int]bool{}
	var groups []int
	for _, c := range cases {
		if c.hasGroup && !groupSet[c.group] {
			groupSet[c.group] = true
			groups = append(groups, c.group)
		}
	}
	sort.Ints(groups)
	if len(groups) > 1 {
		total := overriddenTotals(cases, func(c overrideCase) bool { return c.hasGroup })
		for _, g := range groups {
			left := overriddenTotals(cases, func(c overrideCase) bool { return c.hasGroup && c.group == g })
			right := overrideCounts{n: total.n - left.n, ov: total.ov - left.ov}
			gain, acc, leftOv, ok := overrideEvaluate(left, right)
			if !ok {
				continue
			}
			s := overrideSplit{feature: overriddenDayType, group: g, gain: gain, acc: acc, acceptedLeft: !leftOv}
			if s.acceptedLeft {
				s.groups = []int{g}
			} else {
				for _, o := range groups {
					if o != g {
						s.groups = append(s.groups, o)
					}
				}
			}
			consider(s)
		}
	}

	considerNumeric := func(feature string, pts []overridePoint) {
		if s, ok := overriddenNumericSplit(feature, pts); ok {
			consider(s)
		}
	}
	considerNumeric(overriddenHour, overridePoints(cases, func(c overrideCase) float64 { return c.hour }))
	considerNumeric(overriddenHouseLoad, overridePoints(cases, func(c overrideCase) float64 { return c.house }))
	considerNumeric(overriddenSurplus, overridePoints(cases, func(c overrideCase) float64 { return c.surplus }))
	considerNumeric(overriddenToSunset, overrideSunPoints(cases, func(c overrideCase) float64 { return c.toSunset }))
	considerNumeric(overriddenElevation, overrideSunPoints(cases, func(c overrideCase) float64 { return c.elevation }))
	considerNumeric(overriddenSignal, overrideSignalPoints(cases))

	return best, found
}

// overrideCounts holds the cases and the reversed ones of a side.
type overrideCounts struct{ n, ov int }

// overriddenTotals counts the cases that pass the filter.
func overriddenTotals(cases []overrideCase, keep func(overrideCase) bool) overrideCounts {
	var t overrideCounts
	for _, c := range cases {
		if keep(c) {
			t.n++
			if c.reversed {
				t.ov++
			}
		}
	}
	return t
}

// overridePoints returns the cases with their value of one numeric feature.
func overridePoints(cases []overrideCase, value func(overrideCase) float64) []overridePoint {
	out := make([]overridePoint, 0, len(cases))
	for _, c := range cases {
		out = append(out, overridePoint{x: value(c), ov: c.reversed})
	}
	return out
}

// overrideSignalPoints returns the cases whose condition signal is known with
// its value in W.
func overrideSignalPoints(cases []overrideCase) []overridePoint {
	out := make([]overridePoint, 0, len(cases))
	for _, c := range cases {
		if c.hasSignal {
			out = append(out, overridePoint{x: c.signal, ov: c.reversed})
		}
	}
	return out
}

// overriddenSignalField is the field of the first balance condition of the
// rule, the signal the rule switches on. It reports false without one.
func overriddenSignalField(rule Rule) (string, bool) {
	for _, c := range rule.Conditions {
		if c.Type == "balance_threshold" && c.Field != "" {
			return c.Field, true
		}
	}
	return "", false
}

// overrideSignal is the value of the signal field at the minute of the switch,
// read from the minute record and otherwise from the quarter hour of the day
// file. ok is false when neither holds the value.
func overrideSignal(in *Input, field string, at time.Time, loc *time.Location, days map[string]*DayFile) (float64, bool) {
	if in.Minutes != nil {
		m := at.Truncate(time.Minute)
		if vals := in.Minutes.Range("b:"+field, m, m.Add(time.Minute)).Values; len(vals) > 0 && !math.IsNaN(float64(vals[0])) {
			return float64(vals[0]), true
		}
	}
	local := at.In(loc)
	f := days[local.Format(dayLayout)]
	if f == nil {
		return 0, false
	}
	v := quarterValue(f, field, quarterOf(local))
	return v, !math.IsNaN(v)
}

// overrideSunPoints returns the cases that have a sun state with their value of
// one sun feature. Without a location no case has one, and the feature is skipped.
func overrideSunPoints(cases []overrideCase, value func(overrideCase) float64) []overridePoint {
	out := make([]overridePoint, 0, len(cases))
	for _, c := range cases {
		if c.sunOK {
			out = append(out, overridePoint{x: value(c), ov: c.reversed})
		}
	}
	return out
}

// overrideSunState returns the minutes from the switch until the nearest sunset
// (negative after it), that sunset and the sun elevation in degrees at the
// switch. The nearest sunset is looked up on the day before, the day and the day
// after. ok is false when no sunset exists on those days.
func overrideSunState(at time.Time, site *sun.Location) (toSunset float64, sunset time.Time, elevation float64, ok bool) {
	best := math.Inf(1)
	for k := -1; k <= 1; k++ {
		_, set, has := sun.Times(at.AddDate(0, 0, k), site.Lat, site.Lon)
		if !has {
			continue
		}
		diff := set.Sub(at).Minutes()
		if !ok || math.Abs(diff) < math.Abs(best) {
			best, sunset, ok = diff, set, true
		}
	}
	if !ok {
		return 0, time.Time{}, 0, false
	}
	_, elevation = sun.Position(at, site.Lat, site.Lon)
	return best, sunset, elevation, true
}

// overrideElevationDistance returns the minutes before the sunset at which the
// sun stands at elevation elev, searched within overriddenSunSearchMin minutes of
// the sunset. The elevation falls through the search window, so a bisection
// finds the crossing. Negative values lie after the sunset.
func overrideElevationDistance(sunset time.Time, elev float64, site *sun.Location) (float64, bool) {
	lo := sunset.Add(-overriddenSunSearchMin * time.Minute)
	hi := sunset.Add(overriddenSunSearchMin * time.Minute)
	_, top := sun.Position(lo, site.Lat, site.Lon)
	_, bottom := sun.Position(hi, site.Lat, site.Lon)
	if top < elev || bottom > elev {
		return 0, false
	}
	for i := 0; i < 64 && hi.Sub(lo) > time.Second; i++ {
		mid := lo.Add(hi.Sub(lo) / 2)
		_, e := sun.Position(mid, site.Lat, site.Lon)
		if e >= elev {
			lo = mid
		} else {
			hi = mid
		}
	}
	cross := lo.Add(hi.Sub(lo) / 2)
	return sunset.Sub(cross).Minutes(), true
}

// overriddenSunDistance returns the distance in minutes before the sunset that
// the fix of a sun split uses. For the minutes until sunset it is the cut itself.
// For the elevation it is the median distance at which the sun of each case
// stands at the cut elevation.
func overriddenSunDistance(s overrideSplit, cases []overrideCase, site *sun.Location) (float64, bool) {
	if s.feature == overriddenToSunset {
		return s.cut, true
	}
	if site == nil {
		return 0, false
	}
	var ds []float64
	for _, c := range cases {
		if !c.sunOK {
			continue
		}
		if d, ok := overrideElevationDistance(c.sunset, s.cut, site); ok {
			ds = append(ds, d)
		}
	}
	if len(ds) == 0 {
		return 0, false
	}
	return Median(ds), true
}

// overriddenNumericSplit sweeps the cut between neighbouring values and keeps
// the cut with the largest information gain.
func overriddenNumericSplit(feature string, pts []overridePoint) (overrideSplit, bool) {
	if len(pts) < 2*overriddenMinSide {
		return overrideSplit{}, false
	}
	sorted := append([]overridePoint(nil), pts...)
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].x < sorted[b].x })
	var all overrideCounts
	for _, p := range sorted {
		all.n++
		if p.ov {
			all.ov++
		}
	}
	var best overrideSplit
	found := false
	var left overrideCounts
	for i := 0; i < len(sorted)-1; i++ {
		left.n++
		if sorted[i].ov {
			left.ov++
		}
		if sorted[i].x == sorted[i+1].x {
			continue
		}
		right := overrideCounts{n: all.n - left.n, ov: all.ov - left.ov}
		gain, acc, leftOv, ok := overrideEvaluate(left, right)
		if !ok {
			continue
		}
		if !found || gain > best.gain {
			found = true
			best = overrideSplit{feature: feature, cut: (sorted[i].x + sorted[i+1].x) / 2,
				gain: gain, acc: acc, acceptedLeft: !leftOv}
		}
	}
	return best, found
}

// overrideEvaluate returns the information gain of a split into left and right,
// the share of cases it classifies right, and whether the left side holds the
// reversed cases. It is not ok when a side has fewer than overriddenMinSide cases
// or when the share it classifies right is below overriddenMinSeparation. The
// separation is a filter, so only well separated splits compete on gain.
func overrideEvaluate(left, right overrideCounts) (gain, acc float64, leftOverride, ok bool) {
	if left.n < overriddenMinSide || right.n < overriddenMinSide {
		return 0, 0, false, false
	}
	n := float64(left.n + right.n)
	total := overrideEntropy(float64(left.ov+right.ov), n)
	gain = total -
		float64(left.n)/n*overrideEntropy(float64(left.ov), float64(left.n)) -
		float64(right.n)/n*overrideEntropy(float64(right.ov), float64(right.n))
	asLeft := left.ov + (right.n - right.ov)
	asRight := (left.n - left.ov) + right.ov
	leftOverride = asLeft >= asRight
	acc = float64(max(asLeft, asRight)) / n
	ok = acc >= overriddenMinSeparation
	return gain, acc, leftOverride, ok
}

// overrideEntropy is the entropy in bits of ov reversed cases out of n.
func overrideEntropy(ov, n float64) float64 {
	if n == 0 || ov == 0 || ov == n {
		return 0
	}
	p := ov / n
	return -(p*math.Log2(p) + (1-p)*math.Log2(1-p))
}

// overriddenAddFix builds the add_condition fix of a split that keeps the
// accepted side. It also returns the accepted span of the day in minutes for
// the hour split. The sun splits keep the window from the sunset, or up to the
// sunset, offset by the distance of the cut.
func overriddenAddFix(rule Rule, base Base, s overrideSplit, cases []overrideCase, site *sun.Location) (Fix, [][2]int64, bool) {
	at := len(rule.Conditions)
	var cond map[string]any
	var spans [][2]int64
	switch s.feature {
	case overriddenDayType:
		days := map[int]bool{}
		for _, g := range s.groups {
			if g < 0 || g >= len(base.Groups) {
				return Fix{}, nil, false
			}
			for _, wd := range base.Groups[g] {
				days[mondayIndex(wd)] = true
			}
		}
		if len(days) == 0 || len(days) == 7 {
			return Fix{}, nil, false
		}
		weekdays := make([]int, 0, len(days))
		for d := range days {
			weekdays = append(weekdays, d)
		}
		sort.Ints(weekdays)
		cond = map[string]any{"type": "time_window", "start": "00:00", "end": "23:59", "weekdays": weekdays}
	case overriddenHour:
		h := int(math.Ceil(s.cut))
		if h < 1 || h > 23 {
			return Fix{}, nil, false
		}
		edge := clockOfHour(h)
		if s.acceptedLeft {
			cond = map[string]any{"type": "time_window", "start": "00:00", "end": edge}
			spans = [][2]int64{{0, int64(h * 60)}}
		} else {
			cond = map[string]any{"type": "time_window", "start": edge, "end": "23:59"}
			spans = [][2]int64{{int64(h * 60), int64(minutesPerDay)}}
		}
	case overriddenToSunset, overriddenElevation:
		d, ok := overriddenSunDistance(s, cases, site)
		if !ok {
			return Fix{}, nil, false
		}
		off := int(math.Round(-d/overriddenRoundMin) * overriddenRoundMin)
		// The schema accepts sun offsets only within -240..240 minutes.
		if off < -overriddenSunOffsetMax || off > overriddenSunOffsetMax {
			return Fix{}, nil, false
		}
		if s.acceptedLeft {
			cond = map[string]any{"type": "sun_window", "from": "sunset", "to": "sunrise", "from_offset_min": off, "to_offset_min": 0}
		} else {
			cond = map[string]any{"type": "sun_window", "from": "sunrise", "to": "sunset", "from_offset_min": 0, "to_offset_min": off}
		}
	case overriddenHouseLoad, overriddenSurplus, overriddenSignal:
		field, threshold := "load_total", math.Round(s.cut/overriddenRoundW)*overriddenRoundW
		switch s.feature {
		case overriddenSurplus:
			field = "grid_export"
		case overriddenSignal:
			f, ok := overriddenSignalField(rule)
			if !ok {
				return Fix{}, nil, false
			}
			field = f
		}
		comparison := "above"
		if s.acceptedLeft {
			comparison = "below"
		}
		cond = map[string]any{"type": "balance_threshold", "field": field, "comparison": comparison, "threshold": threshold}
	default:
		return Fix{}, nil, false
	}
	return Fix{Code: "add_condition", RuleID: rule.ID,
		Ops: []Op{{Path: []any{"conditions", at}, Value: cond}}}, spans, true
}

// overriddenFixHeld reports whether the rule already holds the condition of an
// add_condition fix. A signal threshold is held only with the same value, since
// FixApplied would take the rule's own export condition for it whatever its
// threshold. Every other feature uses FixApplied.
func overriddenFixHeld(feature string, fix Fix, doc Doc) bool {
	if feature != overriddenSignal {
		return FixApplied(fix, doc)
	}
	if len(fix.Ops) != 1 {
		return false
	}
	want, ok := normalizedCondition(fix.Ops[0].Value)
	if !ok {
		return false
	}
	for _, r := range doc.Rules {
		if r.ID != fix.RuleID {
			continue
		}
		for _, c := range r.Conditions {
			if c.Type == want["type"] && c.Field == want["field"] && c.Comparison == want["comparison"] && c.Threshold == want["threshold"] {
				return true
			}
		}
	}
	return false
}

// overriddenShiftFix moves the start of the rule's time window to the median
// switch-back time, when the reversals cluster at a similar clock time. It also
// returns the median in minutes of the day.
func overriddenShiftFix(rule Rule, cases []overrideCase) (Fix, int64, bool) {
	var mins []float64
	for _, c := range cases {
		if c.reversed {
			mins = append(mins, c.reverseMin)
		}
	}
	if _, ok := reliabilityCluster(mins); !ok {
		return Fix{}, 0, false
	}
	idx := -1
	for i, c := range rule.Conditions {
		if c.Type == "time_window" {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Fix{}, 0, false
	}
	med := math.Mod(math.Round(CircMedian(mins)/overriddenRoundMin)*overriddenRoundMin, minutesPerDay)
	return Fix{Code: "shift_time", RuleID: rule.ID,
		Ops: []Op{{Path: []any{"conditions", idx, "start"}, Value: switchTimeClock(med)}}}, int64(med), true
}

// minuteOfDay returns the minute of the local day that holds the unix second at.
func minuteOfDay(at int64, loc *time.Location) float64 {
	t := time.Unix(at, 0).In(loc)
	return float64(t.Hour()*60 + t.Minute())
}

// mondayIndex maps a weekday to the rule document order, Monday 0 to Sunday 6.
func mondayIndex(wd time.Weekday) int {
	return (int(wd) + 6) % 7
}

// clockOfHour formats a full hour as HH:00.
func clockOfHour(h int) string {
	return switchTimeClock(float64(h * 60))
}
