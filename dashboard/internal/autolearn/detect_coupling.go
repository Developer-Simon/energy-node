package autolearn

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

// CouplingDetector suggests the rule "when A switches, B follows" for two
// devices that are switched within a short time, by hand or externally. It
// follows spec 15.5 "Kopplungen" and runs once a day over all devices.
var CouplingDetector = Detector{
	Name:      "coupling",
	Group:     "habit",
	Light:     false,
	PerDevice: false,
	Run:       couplingRun,
}

const (
	// couplingMinCases is the number of switchings of A that a pair needs.
	couplingMinCases = 10
	// couplingLimit is the lower Wilson bound of "B follows A" for a candidate.
	couplingLimit = 0.6
	// couplingDirShare is the share of the pairs in which A comes before B.
	couplingDirShare = 0.8
	// couplingChainShare is the share of the longest window's hits that the
	// shortest window must contain.
	couplingChainShare = 0.8
	// couplingHalfShare is the hit share each half of the cases needs for the
	// halves to agree. The spec gives no number, the value is an assumption.
	couplingHalfShare = 0.6
)

// couplingWindows are the windows in seconds, shortest first.
var couplingWindows = []int64{30, 120, 600}

// couplingStream is the switching times of one device in one direction, sorted.
type couplingStream struct {
	times  []int64
	entity string
}

// couplingKey names a stream by device and direction.
type couplingKey struct {
	device string
	on     bool
}

// couplingRun finds the pairs (A with direction, B with direction) where B
// follows A within a window, and suggests the rule for each pair that passes
// the gates. Pairs of rules that already link A and B are dropped.
func couplingRun(ctx context.Context, in *Input, _ string) []Candidate {
	if ctx.Err() != nil {
		return nil
	}
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	streams, entities := couplingStreams(in)
	devices := make([]string, 0, len(entities))
	for d := range entities {
		devices = append(devices, d)
	}
	sort.Strings(devices)

	var out []Candidate
	for _, a := range devices {
		for _, onA := range []bool{false, true} {
			for _, b := range devices {
				if a == b {
					continue
				}
				for _, onB := range []bool{false, true} {
					if ctx.Err() != nil {
						return nil
					}
					sa, sb := streams[couplingKey{a, onA}], streams[couplingKey{b, onB}]
					if sa == nil || sb == nil || len(sa.times) < couplingMinCases {
						continue
					}
					if couplingLinked(in.Doc, entities[a], entities[b]) {
						continue
					}
					if c, ok := couplingCandidate(in, loc, a, b, onA, onB, sa, sb); ok {
						out = append(out, c)
					}
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Finding.ID < out[j].Finding.ID })
	return out
}

// couplingStreams collects the hand and external switchings of all devices,
// and the entities that name each device.
func couplingStreams(in *Input) (map[couplingKey]*couplingStream, map[string]map[string]bool) {
	streams := map[couplingKey]*couplingStream{}
	entities := map[string]map[string]bool{}
	addEntity := func(device, entity string) {
		if entities[device] == nil {
			entities[device] = map[string]bool{}
		}
		if entity != "" {
			entities[device][entity] = true
		}
	}
	for id, info := range in.Devices {
		addEntity(id, "")
		for _, e := range info.Entities {
			addEntity(id, e)
		}
	}
	for i := range in.Days {
		for _, ev := range in.Days[i].Events {
			if ev.Origin != string(OriginHand) && ev.Origin != string(OriginExternal) {
				continue
			}
			addEntity(ev.Device, ev.Entity)
			key := couplingKey{ev.Device, ev.On}
			s := streams[key]
			if s == nil {
				s = &couplingStream{entity: ev.Entity}
				streams[key] = s
			}
			s.times = append(s.times, ev.At)
		}
	}
	for _, s := range streams {
		sort.Slice(s.times, func(i, j int) bool { return s.times[i] < s.times[j] })
	}
	return streams, entities
}

// couplingLinked reports whether a rule already switches one device on the
// other: an action on one entity with a condition on the other, in either
// direction. Such a suggestion would repeat the rule or close a circle.
func couplingLinked(doc Doc, a, b map[string]bool) bool {
	for _, r := range doc.Rules {
		if ruleActsOn(r, b) && ruleReadsEntity(r, a) {
			return true
		}
		if ruleActsOn(r, a) && ruleReadsEntity(r, b) {
			return true
		}
	}
	return false
}

func ruleActsOn(r Rule, entities map[string]bool) bool {
	for _, act := range r.Actions {
		if act.EntityID != "" && entities[act.EntityID] {
			return true
		}
	}
	return false
}

func ruleReadsEntity(r Rule, entities map[string]bool) bool {
	for _, c := range r.Conditions {
		if c.EntityID != "" && entities[c.EntityID] {
			return true
		}
	}
	return false
}

// couplingCandidate checks one pair of streams and builds its candidate, or
// reports false when a gate fails.
func couplingCandidate(in *Input, loc *time.Location, a, b string, onA, onB bool, sa, sb *couplingStream) (Candidate, bool) {
	sa = couplingCases(in, loc, a, b, sa)
	if len(sa.times) < couplingMinCases {
		return Candidate{}, false
	}
	// The longest window sets the reference, the shortest window that keeps
	// enough of its hits is chosen.
	longest := couplingWindows[len(couplingWindows)-1]
	refFlags, _, _ := couplingMatch(sa.times, sb.times, longest)
	ref := couplingHits(refFlags)
	if ref == 0 {
		return Candidate{}, false
	}
	var window int64
	for _, w := range couplingWindows {
		flags, _, _ := couplingMatch(sa.times, sb.times, w)
		if float64(couplingHits(flags)) >= couplingChainShare*float64(ref) {
			window = w
			break
		}
	}
	if window == 0 {
		return Candidate{}, false
	}

	flags, matchedB, deltas := couplingMatch(sa.times, sb.times, window)
	hits := couplingHits(flags)
	cases := len(sa.times)
	lower := WilsonLower(hits, cases)
	if lower < couplingLimit {
		return Candidate{}, false
	}

	// Day shift null: B moved by one to seven days against the same A.
	nulls := ShiftNull(func(k int) int {
		return couplingCount(sa.times, shiftDays(sb.times, loc, k), window)
	})
	for _, n := range nulls {
		if hits <= n {
			return Candidate{}, false
		}
	}

	back := couplingCount(sb.times, sa.times, window)
	share := float64(hits) / float64(hits+back)
	if share < couplingDirShare {
		return Candidate{}, false
	}

	lag := Median(deltas)
	typical := reactionDefault.Seconds()
	if in.Reactions != nil {
		typical = in.Reactions.Typical(sb.entity).Seconds()
	}
	if lag < typical {
		return Candidate{}, false
	}

	halves := couplingHalves(flags)
	return couplingBuild(in, loc, a, b, onA, onB, sa, sb, window, hits, cases, lower, share, lag, matchedB, deltas, halves), true
}

// couplingCases keeps the switchings of a that count as cases for the pair a
// and b. A switching on an absent day does not count, and neither does one at
// a quarter in which a or b was not reachable, since b could not follow then
// (spec 15.3 Zähltage). The quarter of the switching of a is used for both.
func couplingCases(in *Input, loc *time.Location, a, b string, sa *couplingStream) *couplingStream {
	days := make(map[string]*DayFile, len(in.Days))
	for i := range in.Days {
		days[in.Days[i].Day] = &in.Days[i]
	}
	out := &couplingStream{entity: sa.entity}
	for _, t := range sa.times {
		at := time.Unix(t, 0).In(loc)
		day := at.Format(dayLayout)
		if in.Base.Away[day] {
			continue
		}
		q := quarterOf(at)
		if f := days[day]; f != nil && (unreachable(f.Devices[a], q) || unreachable(f.Devices[b], q)) {
			continue
		}
		out.times = append(out.times, t)
	}
	return out
}

// couplingBuild assembles the candidate of a pair that passed the gates.
func couplingBuild(in *Input, loc *time.Location, a, b string, onA, onB bool, sa, sb *couplingStream, window int64,
	hits, cases int, lower, share, lag float64, matchedB []int64, deltas []float64, halves bool) Candidate {
	dirA, dirB := couplingDirName(onA), couplingDirName(onB)
	weights := make([]float64, len(sa.times))
	ist := make([]int64, 0, len(sa.times))
	for i, t := range sa.times {
		at := time.Unix(t, 0).In(loc)
		age := math.Max(0, in.Now.Sub(at).Hours()/24)
		weights[i] = DayWeight(age)
		ist = append(ist, couplingMinute(at))
	}
	soll := make([]int64, 0, len(matchedB))
	for _, t := range matchedB {
		soll = append(soll, couplingMinute(time.Unix(t, 0).In(loc)))
	}
	spanDays := (float64(sa.times[len(sa.times)-1]-sa.times[0]) / 86400) + 1
	weeks := math.Max(spanDays/7, 1)
	benefit := Benefit{Unit: "actions_week", Value: float64(hits) / weeks}
	rate := float64(hits) / float64(cases)

	finding := Finding{
		ID:         fmt.Sprintf("coupling:%s:%s:%s:%s", a, dirA, b, dirB),
		Kind:       "coupling",
		Group:      "habit",
		Severity:   "info",
		Confidence: "high",
		Summary: Text{Code: "coupling.follow", Params: map[string]any{
			"a": a, "b": b, "hits": hits, "cases": cases, "lag_s": int64(math.Round(lag))}},
		Evidence: Evidence{Shape: "day", From: 0, To: minutesPerDay, Ist: ist, Soll: soll},
		Facts: []Fact{
			{Code: "window_s", Value: window},
			{Code: "direction_share", Value: share},
		},
		Apply: []Fix{{Code: "follow_switch", Template: "follow_switch", Prefill: withEntity(withEntity(map[string]any{
			"source": a, "source_on": onA, "target": b, "target_on": onB}, "source_entity", sa.entity), "target_entity", sb.entity)}},
		Device:  b,
		Benefit: &benefit,
	}
	return Candidate{
		Finding:     finding,
		Detector:    "coupling",
		NEff:        NEff(weights),
		MinNEff:     couplingMinCases,
		Lower:       lower,
		Limit:       couplingLimit,
		HalvesAgree: halves,
		Benefit:     benefit,
		Values:      map[string]float64{"hit_rate": rate, "direction": share},
		Spread:      map[string]float64{"lag": MADScale(deltas)},
	}
}

// couplingMatch returns for each A switching whether a B switching follows it
// within the window, the B times that were matched and the lags in seconds.
// B must come strictly after A.
func couplingMatch(a, b []int64, window int64) (flags []bool, matchedB []int64, deltas []float64) {
	flags = make([]bool, len(a))
	for i, ta := range a {
		j := sort.Search(len(b), func(j int) bool { return b[j] > ta })
		if j < len(b) && b[j]-ta <= window {
			flags[i] = true
			matchedB = append(matchedB, b[j])
			deltas = append(deltas, float64(b[j]-ta))
		}
	}
	return flags, matchedB, deltas
}

// couplingHits counts the true flags.
func couplingHits(flags []bool) int {
	n := 0
	for _, f := range flags {
		if f {
			n++
		}
	}
	return n
}

// couplingCount counts the A switchings that a B switching follows within the window.
func couplingCount(a, b []int64, window int64) int {
	flags, _, _ := couplingMatch(a, b, window)
	return couplingHits(flags)
}

// shiftDays moves each time by k calendar days on the wall clock and sorts the result.
func shiftDays(times []int64, loc *time.Location, k int) []int64 {
	out := make([]int64, len(times))
	for i, t := range times {
		out[i] = time.Unix(t, 0).In(loc).AddDate(0, 0, k).Unix()
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// couplingHalves reports whether the first and the second half of the cases
// both reach the hit share.
func couplingHalves(flags []bool) bool {
	mid := len(flags) / 2
	if mid == 0 {
		return false
	}
	first := float64(couplingHits(flags[:mid])) / float64(mid)
	second := float64(couplingHits(flags[mid:])) / float64(len(flags)-mid)
	return first >= couplingHalfShare && second >= couplingHalfShare
}

// couplingMinute is the minute of the day of a time, with the seconds as fraction, rounded.
func couplingMinute(t time.Time) int64 {
	m := float64(t.Hour()*60+t.Minute()) + float64(t.Second())/60
	return int64(math.Round(m))
}

// couplingDirName is the direction part of the ID.
func couplingDirName(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
