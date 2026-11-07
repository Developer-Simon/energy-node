package chainwatch

import (
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
	for _, s := range v.segments {
		m.table.countSign(pairOf(s.a, s.b), *s.a, *s.b)
	}

	for _, s := range v.segments {
		m.feed(at, segKey(cs.ID, energy.FindingUnit, s), detectUnit(v, s), m.holds.Unit, m.holds.Clear, s.info())
	}
	for i := range v.state.Points {
		p := &v.state.Points[i]
		key := trackKey{chain: cs.ID, kind: energy.FindingFrozen, a: p.EntityID}
		m.feed(at, key, detectFrozen(v, p), m.holds.Frozen, 0, feedInfo{from: p.EntityID})
	}
	for _, s := range v.segments {
		m.feed(at, segKey(cs.ID, energy.FindingContradiction, s), detectContradiction(v, s), m.holds.Contradiction, m.holds.Clear, s.info())
	}
	for _, s := range v.segments {
		sv, sugg := detectSign(v, s)
		info := s.info()
		info.sugg = sugg
		// Sign has no hold of its own yet, 4c sets it.
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
		m.table.Learn(pairOf(s.src, s.dst), absW(v.counting.Value), absW(s.src.Value), absW(s.dst.Value), at)
	}
}

// learnable reports whether a segment sample may teach the table: a flowing
// chain without a substitute, a fresh counting point, a fresh segment and a
// source above ChainMinInputW.
func learnable(v chainView, s segView) bool {
	return v.counting != nil && v.counting.Freshness == "fresh" &&
		v.state.Flow != "idle" && v.state.SubstituteFrom == "" &&
		s.state.Available && absW(s.src.Value) >= energy.ChainMinInputW
}

// pruneChain drops the trackers of the chain whose points are no longer in the
// chain as it is now.
func (m *Monitor) pruneChain(chain string, points []string) {
	for key := range m.tracks {
		if key.chain == chain && !keepsKey(key, points) {
			delete(m.tracks, key)
		}
	}
}

// busyOn reports whether an active finding of one of the kinds sits on the
// segment a-b of chain. Frozen counts when it is on a or b.
func (m *Monitor) busyOn(chain, a, b string, kinds ...string) bool {
	for _, kind := range kinds {
		if kind == energy.FindingFrozen {
			if m.isActive(trackKey{chain: chain, kind: kind, a: a}) || m.isActive(trackKey{chain: chain, kind: kind, a: b}) {
				return true
			}
			continue
		}
		if m.isActive(trackKey{chain: chain, kind: kind, a: a, b: b}) {
			return true
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

// detectDrift reports whether the loss of the segment left its normal band. The
// band is the learned one or a manual tolerance, it comes with the snapshot.
// Without a band, without a usable reading or with the flow idle the sample is
// unknown.
func detectDrift(v chainView, s segView) verdict {
	band := s.state.Normal
	if band == nil || !s.state.Available || v.state.Flow == "idle" || v.state.SubstituteFrom != "" {
		return unknown
	}
	if s.state.LossW >= band.LowW && s.state.LossW <= band.HighW {
		return no
	}
	return yes
}

// detectUnit reports a wrong scale or unit on the segment.
// TODO(4c): not implemented yet, every sample is unknown.
func detectUnit(v chainView, s segView) verdict {
	return unknown
}

// detectFrozen reports a point whose value does not move while the others do.
// TODO(4b): not implemented yet, every sample is unknown.
func detectFrozen(v chainView, p *energy.ChainPointState) verdict {
	return unknown
}

// detectContradiction reports a segment whose points contradict each other.
// TODO(4b): not implemented yet, every sample is unknown.
func detectContradiction(v chainView, s segView) verdict {
	return unknown
}

// detectSign reports a meter that counts against the flow. It returns the
// suggestion that goes with a yes.
// TODO(4c): not implemented yet, every sample is unknown.
func detectSign(v chainView, s segView) (verdict, *energy.ChainSuggestion) {
	return unknown, nil
}
