package chainwatch

import (
	"math"
	"slices"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

// verdict is what one detector says about one sample. Unknown means the sample
// cannot tell, it changes no tracker.
type verdict int

const (
	unknown verdict = iota
	no
	yes
)

// switchOffKind is the tracker kind of a switched-off point against a point
// that shows power. It is reported as a contradiction.
const switchOffKind = "contradiction_off"

// segView is one segment in one sample. a and b are its points in chain order,
// src and dst the same points in flow order.
type segView struct {
	a, b, src, dst *energy.ChainPointState
	state          energy.ChainSegmentState
}

func (s segView) info() feedInfo {
	return feedInfo{from: s.src.EntityID, to: s.dst.EntityID}
}

// chainView is one chain in one sample as the detectors read it.
type chainView struct {
	state    *energy.ChainState
	points   []string
	segments []segView
	counting *energy.ChainPointState
}

func newChainView(cs *energy.ChainState) chainView {
	v := chainView{state: cs}
	for i := range cs.Points {
		p := &cs.Points[i]
		v.points = append(v.points, p.EntityID)
		if p.EntityID == cs.Counting {
			v.counting = p
		}
	}
	for k := 0; k+1 < len(cs.Points) && k < len(cs.Segments); k++ {
		a, b := &cs.Points[k], &cs.Points[k+1]
		src, dst := a, b
		if cs.Segments[k].From != a.EntityID {
			src, dst = b, a
		}
		v.segments = append(v.segments, segView{a: a, b: b, src: src, dst: dst, state: cs.Segments[k]})
	}
	return v
}

// feedInfo is what a finding shows about its segment or point. It is set
// whenever the detector reports the finding.
type feedInfo struct {
	from, to string
	sugg     *energy.ChainSuggestion
}

// observeChain runs one sample of a chain: the trackers of vanished points go,
// the scales and inverts are checked, the sign counters move, the detectors
// unit, frozen, contradiction, sign and drift are evaluated, and last the
// samples are learned.
func (m *Monitor) observeChain(at time.Time, cs *energy.ChainState) {
	v := newChainView(cs)
	m.pruneChain(cs.ID, v.points)

	for _, s := range v.segments {
		m.table.syncPair(pairOf(s.a, s.b), *s.a, *s.b, at)
		m.table.syncPair(pairOf(s.src, s.dst), *s.src, *s.dst, at)
	}
	if cs.Flow != "idle" {
		for _, s := range v.segments {
			m.table.countSign(pairOf(s.a, s.b), *s.a, *s.b)
		}
	}

	for _, s := range v.segments {
		uv, sugg := detectUnit(v, s)
		info := s.info()
		info.sugg = sugg
		m.feed(at, segKey(cs.ID, energy.FindingUnit, s), uv, m.holds.Unit, m.holds.Clear, info)
	}
	for i := range v.state.Points {
		p := &v.state.Points[i]
		key := trackKey{chain: cs.ID, kind: energy.FindingFrozen, a: p.EntityID}
		// The frozen window already waits for the hold, so the tracker takes the first yes.
		m.feed(at, key, m.frozenVerdict(at, v, p), 0, 0, feedInfo{from: p.EntityID})
	}
	for _, s := range v.segments {
		m.feed(at, segKey(cs.ID, energy.FindingContradiction, s), detectContradiction(v, s), m.holds.Contradiction, m.holds.Clear, s.info())
	}
	for i := range v.state.Points {
		off := &v.state.Points[i]
		if !off.SwitchOff {
			continue
		}
		for j := range v.state.Points {
			p := &v.state.Points[j]
			key := trackKey{chain: cs.ID, kind: switchOffKind, a: off.EntityID, b: p.EntityID}
			m.feed(at, key, detectSwitchOff(p), m.holds.Contradiction, m.holds.Clear, feedInfo{from: p.EntityID, to: off.EntityID})
		}
	}
	for _, s := range v.segments {
		// Sign has no hold of its own: the counters carry the condition.
		sv, sugg := detectSign(m.table, at, v, s)
		info := s.info()
		info.sugg = sugg
		m.feed(at, segKey(cs.ID, energy.FindingSign, s), sv, 0, m.holds.Clear, info)
	}
	for _, s := range v.segments {
		key := segKey(cs.ID, energy.FindingDrift, s)
		if m.busyOn(cs.ID, s.a.EntityID, s.b.EntityID, energy.FindingUnit, energy.FindingFrozen, energy.FindingContradiction) {
			// Another finding explains the loss, the drift starts over afterwards.
			delete(m.tracks, key)
			continue
		}
		m.feed(at, key, detectDrift(v, s), m.holds.Drift, m.holds.Clear, s.info())
	}

	for _, s := range v.segments {
		if !learnable(v, s) {
			continue
		}
		if m.busyOn(cs.ID, s.a.EntityID, s.b.EntityID,
			energy.FindingUnit, energy.FindingFrozen, energy.FindingContradiction, energy.FindingDrift) {
			continue
		}
		m.table.Learn(pairOf(s.src, s.dst), math.Abs(v.counting.Value), math.Abs(s.src.Value), math.Abs(s.dst.Value), at)
	}
}

// learnable reports whether a segment sample may teach the table: a flowing
// chain without a substitute, a fresh counting point, a fresh segment, a source
// above ChainMinInputW and only finite values.
func learnable(v chainView, s segView) bool {
	return v.counting != nil && v.counting.Freshness == "fresh" && finite(v.counting.Value) &&
		v.state.Flow != "idle" && v.state.SubstituteFrom == "" &&
		s.state.Available && finite(s.src.Value) && finite(s.dst.Value) &&
		math.Abs(s.src.Value) >= energy.ChainMinInputW
}

// pruneChain drops the trackers and frozen windows of the chain whose points
// are no longer in the chain as it is now.
func (m *Monitor) pruneChain(chain string, points []string) {
	for key := range m.tracks {
		if key.chain == chain && !keepsKey(key, points) {
			delete(m.tracks, key)
		}
	}
	for key := range m.windows {
		if key.chain == chain && !containsString(points, key.point) {
			delete(m.windows, key)
		}
	}
}

// busyOn reports whether an active finding of one of the kinds sits on the
// segment a-b of chain. Frozen counts when it is on a or b, contradiction in
// either order.
func (m *Monitor) busyOn(chain, a, b string, kinds ...string) bool {
	for _, kind := range kinds {
		switch kind {
		case energy.FindingFrozen:
			if m.isActive(trackKey{chain: chain, kind: kind, a: a}) || m.isActive(trackKey{chain: chain, kind: kind, a: b}) {
				return true
			}
		case energy.FindingContradiction:
			for _, k := range []string{energy.FindingContradiction, switchOffKind} {
				if m.isActive(trackKey{chain: chain, kind: k, a: a, b: b}) || m.isActive(trackKey{chain: chain, kind: k, a: b, b: a}) {
					return true
				}
			}
		default:
			if m.isActive(trackKey{chain: chain, kind: kind, a: a, b: b}) {
				return true
			}
		}
	}
	return false
}

func (m *Monitor) isActive(key trackKey) bool {
	e := m.tracks[key]
	return e != nil && e.active
}

// feed hands one reading of a detector to its tracker. A tracker is created
// when a reading holds and dropped once it is neither pending nor active.
func (m *Monitor) feed(at time.Time, key trackKey, v verdict, hold, clear time.Duration, info feedInfo) {
	e := m.tracks[key]
	if e == nil {
		if v != yes {
			return
		}
		e = &trackEntry{}
		m.tracks[key] = e
	}
	e.update(at, v, hold, clear)
	if v == yes {
		e.from, e.to, e.sugg = info.from, info.to, info.sugg
	}
	if !e.active && e.since.IsZero() {
		delete(m.tracks, key)
	}
}

func segKey(chain, kind string, s segView) trackKey {
	return trackKey{chain: chain, kind: kind, a: s.a.EntityID, b: s.b.EntityID}
}

func pairOf(source, target *energy.ChainPointState) energystats.ChainPair {
	return energystats.ChainPair{Source: source.EntityID, Target: target.EntityID}
}

func verdictOf(b bool) verdict {
	if b {
		return yes
	}
	return no
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// windowKey names the frozen window of one point.
type windowKey struct {
	chain, point string
}

// span is the lowest and highest value a point read in a window.
type span struct {
	min, max float64
}

// frozenWindow is the time since a point last changed, with the spread of every
// other fresh point of the chain in that time.
type frozenWindow struct {
	last   float64
	since  time.Time
	spans  map[string]*span
	scales map[string]float64 // scale of the other points when the window started
}

// scaleChanged reports whether another point changed its scale since the window
// started. A scale correction moves the values without a movement of the
// process, so the window starts over.
func (w *frozenWindow) scaleChanged(v chainView, p *energy.ChainPointState) bool {
	for i := range v.state.Points {
		q := &v.state.Points[i]
		if scale, ok := w.scales[q.EntityID]; ok && q.EntityID != p.EntityID && scale != q.Scale {
			return true
		}
	}
	return false
}

// observe takes the fresh values of the other points into the spans.
func (w *frozenWindow) observe(v chainView, p *energy.ChainPointState) {
	for i := range v.state.Points {
		q := &v.state.Points[i]
		if q.EntityID == p.EntityID || q.Freshness != "fresh" || !finite(q.Value) {
			continue
		}
		w.scales[q.EntityID] = q.Scale
		s := w.spans[q.EntityID]
		if s == nil {
			w.spans[q.EntityID] = &span{min: q.Value, max: q.Value}
			continue
		}
		s.min = math.Min(s.min, q.Value)
		s.max = math.Max(s.max, q.Value)
	}
}

// move is the largest spread of the other points since the window started.
func (w *frozenWindow) move() float64 {
	out := 0.0
	for _, s := range w.spans {
		out = math.Max(out, s.max-s.min)
	}
	return out
}

// frozenVerdict reports whether p is frozen: fresh, not near zero, unchanged for
// the Frozen hold, while another fresh point moved by at least FrozenMoveW in
// that time. A change of value ends the window at once.
func (m *Monitor) frozenVerdict(at time.Time, v chainView, p *energy.ChainPointState) verdict {
	key := windowKey{chain: v.state.ID, point: p.EntityID}
	if p.Freshness != "fresh" || !finite(p.Value) || math.Abs(p.Value) < energy.ChainMinInputW {
		delete(m.windows, key)
		return no
	}
	w := m.windows[key]
	if w == nil || w.last != p.Value || w.scaleChanged(v, p) {
		w = &frozenWindow{last: p.Value, since: at, spans: map[string]*span{}, scales: map[string]float64{}}
		m.windows[key] = w
	}
	w.observe(v, p)
	if at.Sub(w.since) < m.holds.Frozen || w.move() < FrozenMoveW {
		return no
	}
	return yes
}

// detectContradiction reports two adjacent points that cannot both be right. One
// of them reads under 1 W, the other shows power. Without a learned band the
// other must reach ContradictionW. With a band it must reach
// ContradictionLearnedW and the loss must lie outside the band.
func detectContradiction(v chainView, s segView) verdict {
	if !s.state.Available || !finite(s.src.Value) || !finite(s.dst.Value) {
		return unknown
	}
	srcZero := math.Abs(s.src.Value) < energy.ChainIdleW
	dstZero := math.Abs(s.dst.Value) < energy.ChainIdleW
	var other *energy.ChainPointState
	switch {
	case dstZero && !srcZero:
		other = s.src
	case srcZero && !dstZero:
		other = s.dst
	default:
		return no
	}
	otherW := math.Abs(other.Value)
	band := s.state.Normal
	if band == nil {
		return verdictOf(otherW >= ContradictionW)
	}
	if otherW < ContradictionLearnedW {
		return no
	}
	return verdictOf(s.state.LossW < band.LowW || s.state.LossW > band.HighW)
}

// detectSwitchOff reports a point that shows power while the switch of its
// device is off. The pair is the switched-off point and p.
func detectSwitchOff(p *energy.ChainPointState) verdict {
	if p.Freshness != "fresh" || !finite(p.Value) {
		return unknown
	}
	return verdictOf(math.Abs(p.Value) >= ContradictionLearnedW)
}

// detectDrift reports whether the loss of the segment left its normal band. The
// band is the learned one or a manual tolerance, it comes with the snapshot.
// Without a band, without a usable reading, with the flow idle or with a
// non-finite loss the sample is unknown.
func detectDrift(v chainView, s segView) verdict {
	band := s.state.Normal
	if band == nil || !s.state.Available || v.state.Flow == "idle" ||
		v.state.SubstituteFrom != "" || !finite(s.state.LossW) {
		return unknown
	}
	if s.state.LossW >= band.LowW && s.state.LossW <= band.HighW {
		return no
	}
	return yes
}

// detectUnit reports two points that differ by a factor of 1000 within
// UnitTolerance of the larger one, the larger at least UnitMinW. The suggestion
// scales the smaller point by 1000, or the larger one by 0.001 from
// UnitFlipAboveW on.
func detectUnit(v chainView, s segView) (verdict, *energy.ChainSuggestion) {
	a, b := s.a, s.b
	if a.Freshness != "fresh" || b.Freshness != "fresh" || !finite(a.Value) || !finite(b.Value) {
		return unknown, nil
	}
	small, large := a, b
	if math.Abs(a.Value) > math.Abs(b.Value) {
		small, large = b, a
	}
	smallW, largeW := math.Abs(small.Value), math.Abs(large.Value)
	if largeW < UnitMinW || math.Abs(largeW-1000*smallW) > UnitTolerance*largeW {
		return no, nil
	}
	if largeW >= UnitFlipAboveW {
		return yes, &energy.ChainSuggestion{EntityID: large.EntityID, ScaleFactor: 0.001}
	}
	return yes, &energy.ChainSuggestion{EntityID: small.EntityID, ScaleFactor: 1000}
}

// detectSign reports a pair whose meter counts against the flow: the pair is at
// least SignMinAge old, has SignMinOpposite opposite samples and at most
// SignMaxAgreeShare agreeing ones. The suggestion inverts the point of the pair
// farther from the counting point.
func detectSign(t *Table, at time.Time, v chainView, s segView) (verdict, *energy.ChainSuggestion) {
	agree, opposite, firstSeen, ok := t.signCounts(pairOf(s.a, s.b))
	if !ok {
		return no, nil
	}
	total := agree + opposite
	if at.Sub(firstSeen) < SignMinAge || opposite < SignMinOpposite ||
		float64(agree) > SignMaxAgreeShare*float64(total) {
		return no, nil
	}
	return yes, &energy.ChainSuggestion{EntityID: signTarget(v, s), Invert: true}
}

// signTarget is the point of the segment that lies farther from the counting
// point in chain order. Ties go to the later point, a missing counting point to
// the later point too.
func signTarget(v chainView, s segView) string {
	ia := slices.Index(v.points, s.a.EntityID)
	ib := slices.Index(v.points, s.b.EntityID)
	ic := slices.Index(v.points, v.state.Counting)
	if ic < 0 {
		return s.b.EntityID
	}
	if intAbs(ia-ic) > intAbs(ib-ic) {
		return s.a.EntityID
	}
	return s.b.EntityID
}

func intAbs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
