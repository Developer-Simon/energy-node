package autolearn

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

// SwitchTimeDetector suggests a time rule for a device that is switched by
// hand or externally at about the same clock time on regular days. It follows
// spec 15.5 "Schaltzeiten" with the clock reference only.
var SwitchTimeDetector = Detector{
	Name:      "switch_time",
	Group:     "habit",
	Light:     false,
	PerDevice: true,
	Run:       switchTimeRun,
}

const (
	// switchTimeMinShare is the share of the largest density a peak needs.
	// The spec gives no number, the value is an assumption.
	switchTimeMinShare = 0.2
	// switchTimeWindow is the number of spreads around a centre whose events
	// belong to a peak.
	switchTimeWindow = 3.0
	// switchTimeMinSpread keeps the window from collapsing to zero.
	switchTimeMinSpread = 1.0
	// switchTimeMinNEff is the minimum effective number of counted days.
	switchTimeMinNEff = 10.0
	// switchTimeLimit is the lower bound for high confidence.
	switchTimeLimit = 0.6
	// switchTimeHighSpread is the spread in minutes for high confidence.
	switchTimeHighSpread = 20.0
	// switchTimeEstLower and switchTimeEstSpread are the bounds for an estimate.
	switchTimeEstLower  = 0.45
	switchTimeEstSpread = 40.0
	// switchTimeRoundMin rounds the proposed time, switchTimeIDMin the time in the ID.
	switchTimeRoundMin = 5.0
	switchTimeIDMin    = 30.0
)

// swRef is one day of the device that the detector reads.
type swRef struct {
	date  string
	t     time.Time
	group int
	dd    *DeviceDay
	// prev is the device of the previous calendar day, nil when that day is
	// missing. The quarter before 00:00 is read from it.
	prev   *DeviceDay
	weight float64
	// events holds the minutes of the day of the hand and external events,
	// index 0 for off and 1 for on.
	events [2][]float64
	// entities names the switch entity of each event, parallel to events.
	entities [2][]string
}

// switchTimeRun finds the peaks of the switching times of one device. Each
// day type group and direction is run on its own.
func switchTimeRun(ctx context.Context, in *Input, device string) []Candidate {
	refs := switchTimeRefs(in, device)
	var out []Candidate
	for g := range in.Base.Groups {
		for _, on := range []bool{false, true} {
			if ctx.Err() != nil {
				return nil
			}
			for _, p := range switchTimeGroup(in, device, g, on, refs) {
				out = append(out, p.Candidate)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Finding.ID < out[j].Finding.ID })
	return out
}

// switchTimeRefs collects the days of the device that are not absent and have
// a day type, sorted by date.
func switchTimeRefs(in *Input, device string) []swRef {
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	byDate := map[string]*DeviceDay{}
	for i := range in.Days {
		byDate[in.Days[i].Day] = in.Days[i].Devices[device]
	}
	var refs []swRef
	for i := range in.Days {
		f := &in.Days[i]
		g, ok := in.Base.DayType[f.Day]
		if !ok || in.Base.Away[f.Day] {
			continue
		}
		t, err := time.ParseInLocation(dayLayout, f.Day, loc)
		if err != nil {
			continue
		}
		age := math.Max(0, in.Now.Sub(t).Hours()/24)
		r := swRef{date: f.Day, t: t, group: g, dd: f.Devices[device], weight: DayWeight(age)}
		r.prev = byDate[t.AddDate(0, 0, -1).Format(dayLayout)]
		for _, ev := range f.Events {
			if ev.Device != device || (ev.Origin != string(OriginHand) && ev.Origin != string(OriginExternal)) {
				continue
			}
			at := time.Unix(ev.At, 0).In(loc)
			dir := 0
			if ev.On {
				dir = 1
			}
			r.events[dir] = append(r.events[dir], float64(at.Hour()*60+at.Minute())+float64(at.Second())/60)
			r.entities[dir] = append(r.entities[dir], ev.Entity)
		}
		refs = append(refs, r)
	}
	sort.SliceStable(refs, func(a, b int) bool { return refs[a].date < refs[b].date })
	return refs
}

// switchTimePeak is a peak that passed the gates: the candidate of the
// suggestion, the counted days, and the centre rounded to 5 minutes.
type switchTimePeak struct {
	Candidate
	Days   int
	Minute float64
}

// switchTimeGroup is the cluster search of one group and direction, shared by
// the suggestion and the learned value. Each peak of the circular density
// gives at most one peak. Peaks are visited in time order, and a peak whose ID
// is already taken by an earlier one is dropped.
func switchTimeGroup(in *Input, device string, g int, on bool, refs []swRef) []switchTimePeak {
	dir := 0
	if on {
		dir = 1
	}
	var group []int
	var minutes, weights []float64
	var entities []string
	for i := range refs {
		if refs[i].group != g {
			continue
		}
		group = append(group, i)
		for _, m := range refs[i].events[dir] {
			minutes = append(minutes, m)
			weights = append(weights, refs[i].weight)
		}
		entities = append(entities, refs[i].entities[dir]...)
	}
	if len(minutes) < 2 {
		return nil
	}
	dens := CircKDE(minutes, weights)
	var out []switchTimePeak
	ids := map[string]bool{}
	for _, peak := range dens.Peaks(switchTimeMinShare) {
		c, ok := switchTimeCandidate(in, device, g, on, refs, group, minutes, entities, peak)
		if !ok || ids[c.Finding.ID] {
			continue
		}
		ids[c.Finding.ID] = true
		out = append(out, c)
	}
	return out
}

// switchTimeCandidate builds the peak of one centre, or reports false when
// the peak fails the gates.
func switchTimeCandidate(in *Input, device string, g int, on bool, refs []swRef, group []int, minutes []float64, entities []string, peak float64) (switchTimePeak, bool) {
	dir := 0
	dirName := "off"
	if on {
		dir = 1
		dirName = "on"
	}
	center, spread, n := switchTimeCluster(minutes, peak)
	if n < 2 {
		return switchTimePeak{}, false
	}
	window := switchTimeWindow * spread
	c := wrapMinutes(center)
	q := int(c / 15)
	prev := (q + dayQuarters - 1) % dayQuarters

	// Counted days: the quarter of the centre was seen, and the device was not
	// already in the target state in the quarter before. For a centre in
	// quarter 0 the quarter before is quarter 95 of the previous day, and a day
	// without a previous day is not counted.
	var counted, hit []int
	for _, i := range group {
		r := &refs[i]
		if r.dd == nil || r.dd.Seen[q] == 0 {
			continue
		}
		before, pq := r.dd, prev
		if q == 0 {
			before, pq = r.prev, dayQuarters-1
			if before == nil {
				continue
			}
		}
		if (on && before.OnMin[pq] == 15) || (!on && before.OnMin[pq] == 0) {
			continue
		}
		counted = append(counted, i)
		if switchTimeNear(r.events[dir], center, window) {
			hit = append(hit, i)
		}
	}
	days := len(counted)
	if days == 0 {
		return switchTimePeak{}, false
	}
	weights := make([]float64, days)
	for k, i := range counted {
		weights[k] = refs[i].weight
	}
	neff := NEff(weights)
	if neff < switchTimeMinNEff {
		return switchTimePeak{}, false
	}
	hits := len(hit)
	lower := WilsonLower(hits, days)
	var conf string
	switch {
	case lower >= switchTimeLimit && spread <= switchTimeHighSpread:
		conf = "high"
	case lower >= switchTimeEstLower && spread <= switchTimeEstSpread:
		conf = "estimate"
	default:
		return switchTimePeak{}, false
	}
	rate := float64(hits) / float64(days)

	propMin := math.Mod(math.Round(c/switchTimeRoundMin)*switchTimeRoundMin, minutesPerDay)
	idMin := math.Mod(math.Round(c/switchTimeIDMin)*switchTimeIDMin, minutesPerDay)
	clock := switchTimeClock(propMin)
	id := fmt.Sprintf("switch_time:%s:%s:%d:%s", device, dirName, g, switchTimeCompact(idMin))

	// The rule document counts weekdays from Monday (0) to Sunday (6).
	weekdays := make([]int, 0, len(in.Base.Groups[g]))
	for _, d := range in.Base.Groups[g] {
		weekdays = append(weekdays, mondayIndex(d))
	}
	sort.Ints(weekdays)
	ist := make([]int64, 0, len(minutes))
	for _, m := range minutes {
		ist = append(ist, int64(math.Round(m)))
	}
	benefit := switchTimeBenefit(refs, counted, hits)
	season := switchTimeSeason(refs, counted, hit)
	agree := switchTimeHalvesAgree(refs, counted, dir, center, window, spread)

	finding := Finding{
		ID:         id,
		Kind:       "switch_time",
		Group:      "habit",
		Severity:   "info",
		Confidence: conf,
		Summary: Text{Code: "switch_time." + dirName, Params: map[string]any{
			"hits": hits, "days": days, "time": clock, "device": device}},
		Evidence: Evidence{Shape: "day", From: 0, To: minutesPerDay, Ist: ist, Soll: []int64{int64(propMin)}},
		Facts: []Fact{
			{Code: "spread_min", Value: spread},
			{Code: "hit_rate", Value: rate},
		},
		Apply:   []Fix{{Code: "time_point", Template: "time_point", Prefill: switchTimePrefill(device, entities, on, clock, weekdays)}},
		Device:  device,
		Season:  season,
		Benefit: &benefit,
	}
	cand := Candidate{
		Finding:     finding,
		Detector:    "switch_time",
		NEff:        neff,
		MinNEff:     switchTimeMinNEff,
		Lower:       lower,
		Limit:       switchTimeLimit,
		HalvesAgree: agree,
		Benefit:     benefit,
		Values:      map[string]float64{"hit_rate": rate, "time": propMin},
		// The quiet check never holds the shown time within less than 5 minutes.
		Spread: map[string]float64{"time": quietSpread(spread)},
	}
	return switchTimePeak{Candidate: cand, Days: days, Minute: propMin}, true
}

// switchTimePrefill is the prefill of the time_point fix. The entity is the
// switch the events of the peak name, the assistant selects it by entity ID.
func switchTimePrefill(device string, entities []string, on bool, clock string, weekdays []int) map[string]any {
	prefill := map[string]any{"device": device, "on": on, "time": clock, "days": weekdays}
	return withEntity(prefill, "entity", dominantEntity(entities))
}

// quietSpread is the spread of a shown time: never below 5 minutes, and 5 for
// a NaN spread, which math.Max would pass on.
func quietSpread(spread float64) float64 {
	if math.IsNaN(spread) || spread < 5 {
		return 5
	}
	return spread
}

// switchTimeCluster takes the events within three kernel widths of the peak and
// computes their circular median and spread. It is a single pass on purpose: a
// second pass with a robust spread collapses a bimodal peak (23:55 and 00:05)
// onto one of its halves. n is the number of events, below two there is no
// cluster.
func switchTimeCluster(minutes []float64, peak float64) (center, spread float64, n int) {
	var members []float64
	for _, m := range minutes {
		if math.Abs(circDelta(m, peak)) <= switchTimeWindow*circSigmaMin {
			members = append(members, m)
		}
	}
	n = len(members)
	if n < 2 {
		return peak, circSigmaMin, n
	}
	center = CircMedian(members)
	spread = math.Max(CircSpread(members, center), switchTimeMinSpread)
	return center, spread, n
}

// switchTimeNear reports whether one of the minutes lies within the window of centre.
func switchTimeNear(minutes []float64, center, window float64) bool {
	for _, m := range minutes {
		if math.Abs(circDelta(m, center)) <= window {
			return true
		}
	}
	return false
}

// switchTimeBenefit is the number of hits per week over the span of the counted days.
func switchTimeBenefit(refs []swRef, counted []int, hits int) Benefit {
	first := refs[counted[0]].t
	last := refs[counted[len(counted)-1]].t
	weeks := math.Max((last.Sub(first).Hours()/24+1)/7, 1)
	return Benefit{Unit: "actions_week", Value: float64(hits) / weeks}
}

// switchTimeSeason returns the range of weeks when the hits lie only in part of
// the counted weeks, otherwise nil. The weeks are ISO weeks, the range does not
// wrap around the year end.
func switchTimeSeason(refs []swRef, counted, hit []int) *Season {
	all := map[int]bool{}
	for _, i := range counted {
		y, w := refs[i].t.ISOWeek()
		all[y*100+w] = true
	}
	hitWeeks := map[int]bool{}
	for _, i := range hit {
		y, w := refs[i].t.ISOWeek()
		hitWeeks[y*100+w] = true
	}
	if len(hitWeeks) == 0 || len(hitWeeks) == len(all) {
		return nil
	}
	from, to := math.MaxInt, math.MinInt
	for k := range hitWeeks {
		from = min(from, k)
		to = max(to, k)
	}
	if from%100 > to%100 {
		// The hits lie across the year end, a range would wrap around.
		return nil
	}
	return &Season{FromWeek: from % 100, ToWeek: to % 100}
}

// switchTimeHalvesAgree compares the centre of the first and the second half of
// the counted days. Each half needs two events in the window, and the centres
// must lie within one spread of each other.
func switchTimeHalvesAgree(refs []swRef, counted []int, dir int, center, window, spread float64) bool {
	mid := len(counted) / 2
	parts := [][]int{counted[:mid], counted[mid:]}
	var centers []float64
	for _, part := range parts {
		var members []float64
		for _, i := range part {
			for _, m := range refs[i].events[dir] {
				if math.Abs(circDelta(m, center)) <= window {
					members = append(members, m)
				}
			}
		}
		if len(members) < 2 {
			return false
		}
		centers = append(centers, CircMedian(members))
	}
	return math.Abs(circDelta(centers[0], centers[1])) <= spread
}

// switchTimeClock formats minutes of the day as HH:MM.
func switchTimeClock(m float64) string {
	v := int(m)
	return fmt.Sprintf("%02d:%02d", v/60, v%60)
}

// switchTimeCompact formats minutes of the day as HHMM, for IDs.
func switchTimeCompact(m float64) string {
	v := int(m)
	return fmt.Sprintf("%02d%02d", v/60, v%60)
}

// LearnSwitchTimeDetector learns the usual switch times of a device for the
// day type of today. It runs the same cluster search and gates as
// SwitchTimeDetector and keeps the earliest on peak and the latest off peak.
var LearnSwitchTimeDetector = Detector{
	Name:      "learn_switch_time",
	Group:     "habit",
	Light:     false,
	PerDevice: true,
	Run:       learnSwitchTimeRun,
}

// learnSwitchTimeRun returns usual_on and usual_off of a device, each only when
// a peak of its direction passes the gates in the day type group of today.
func learnSwitchTimeRun(ctx context.Context, in *Input, device string) []Candidate {
	g, ok := learnTodayGroup(in)
	if !ok {
		return nil
	}
	refs := switchTimeRefs(in, device)
	var out []Candidate
	for _, on := range []bool{true, false} {
		if ctx.Err() != nil {
			return nil
		}
		peaks := switchTimeGroup(in, device, g, on, refs)
		if len(peaks) == 0 {
			continue
		}
		best := peaks[0]
		for _, p := range peaks[1:] {
			if (on && p.Minute < best.Minute) || (!on && p.Minute > best.Minute) {
				best = p
			}
		}
		out = append(out, learnSwitchCandidate(device, on, best))
	}
	return out
}

// learnSwitchCandidate is the learned value of one direction, the peak carries
// the counted days and the rounded clock time.
func learnSwitchCandidate(device string, on bool, p switchTimePeak) Candidate {
	name := "usual_off"
	if on {
		name = "usual_on"
	}
	return Candidate{
		Detector: "learn_switch_time",
		NEff:     p.NEff,
		MinNEff:  switchTimeMinNEff,
		Lower:    p.Lower,
		Limit:    p.Limit,
		Value: &Value{
			Key:    "device/" + device + "/" + name,
			Device: device,
			Kind:   ValueTime,
			Group:  "habit",
			Text:   switchTimeClock(p.Minute),
			N:      p.Days,
		},
	}
}
