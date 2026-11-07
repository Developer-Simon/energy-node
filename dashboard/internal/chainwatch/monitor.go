package chainwatch

import (
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

// Holds are the hold times of the diagnoses. A condition must hold for its
// time before the finding shows. Clear is how long a shown finding stays after
// its condition stopped, Frozen ends at the first change.
type Holds struct{ Drift, Frozen, Contradiction, Unit, Clear time.Duration }

// DefaultHolds are the hold times of the diagnoses in production.
var DefaultHolds = Holds{30 * time.Minute, 15 * time.Minute, 10 * time.Minute, 15 * time.Minute, 5 * time.Minute}

// Thresholds of the detectors. The detectors of unit, frozen, contradiction
// and sign use them in later steps.
const (
	FrozenMoveW           = 20.0
	ContradictionW        = 50.0
	ContradictionLearnedW = 20.0
	SignMinOpposite       = 360
	SignMaxAgreeShare     = 0.05
	SignMinAge            = 24 * time.Hour
	UnitMinW              = 50.0
	UnitTolerance         = 0.25
	UnitFlipAboveW        = 50000.0
)

// Monitor learns the normal loss of the chain segments and reports the four
// diagnoses. Observe feeds it one snapshot at a time, the readers may run at
// the same time.
type Monitor struct {
	table   *Table
	holds   Holds
	now     func() time.Time
	mu      sync.RWMutex
	tracks  map[trackKey]*trackEntry
	windows map[windowKey]*frozenWindow
	labels  map[string]string
}

var _ energy.ChainInsight = (*Monitor)(nil)

// NewMonitor returns a monitor that learns into table. now gives the time for
// the age of learned pairs.
func NewMonitor(table *Table, holds Holds, now func() time.Time) *Monitor {
	if table == nil {
		table = NewTable()
	}
	if now == nil {
		now = time.Now
	}
	return &Monitor{
		table:   table,
		holds:   holds,
		now:     now,
		tracks:  map[trackKey]*trackEntry{},
		windows: map[windowKey]*frozenWindow{},
		labels:  map[string]string{},
	}
}

// tracker turns the readings of one detector into a finding. A true reading
// starts a run and makes the finding active once the run has lasted hold. A
// false reading ends a pending run at once, and an active finding once no true
// reading came for clear. An unknown reading changes nothing.
type tracker struct {
	since, lastTrue time.Time
	active          bool
}

func (tr *tracker) update(at time.Time, v verdict, hold, clear time.Duration) {
	switch v {
	case yes:
		if tr.since.IsZero() {
			tr.since = at
		}
		tr.lastTrue = at
		if !tr.active && at.Sub(tr.since) >= hold {
			tr.active = true
		}
	case no:
		if !tr.active {
			tr.since = time.Time{}
			return
		}
		if at.Sub(tr.lastTrue) >= clear {
			*tr = tracker{}
		}
	}
}

// trackKey names a tracker: the chain, the kind and the points. Segment kinds
// use the points in chain order, frozen has only a.
type trackKey struct {
	chain, kind, a, b string
}

// onPair reports whether the finding of the key concerns the pair a-b in
// either order. Frozen concerns the point a or b.
func (k trackKey) onPair(a, b string) bool {
	if k.kind == energy.FindingFrozen {
		return k.a == a || k.a == b
	}
	return (k.a == a && k.b == b) || (k.a == b && k.b == a)
}

// trackEntry is a tracker with what the finding shows: the flow order of its
// segment and the suggestion.
type trackEntry struct {
	tracker
	from, to string
	sugg     *energy.ChainSuggestion
}

// Observe feeds one snapshot taken at at. A chain missing from the snapshot
// loses its trackers.
func (m *Monitor) Observe(at time.Time, snapshot energy.Snapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()

	seen := map[string]bool{}
	for i := range snapshot.Chains {
		cs := &snapshot.Chains[i]
		seen[cs.ID] = true
		m.labels[cs.ID] = cs.Label
		m.observeChain(at, cs)
	}
	for key := range m.tracks {
		if !seen[key.chain] {
			delete(m.tracks, key)
		}
	}
	for key := range m.windows {
		if !seen[key.chain] {
			delete(m.windows, key)
		}
	}
	for id := range m.labels {
		if !seen[id] {
			delete(m.labels, id)
		}
	}
}

// Relearn drops what is learned about the pair a-b in both directions, and
// every finding on it.
func (m *Monitor) Relearn(a, b string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.table.Reset(energystats.ChainPair{Source: a, Target: b})
	for key := range m.tracks {
		if key.onPair(a, b) {
			delete(m.tracks, key)
		}
	}
}

// Prune drops the learned pairs that are no longer adjacent in any chain, and
// the trackers of chains that are gone or whose points changed.
func (m *Monitor) Prune(chains map[string]energy.Chain) {
	m.table.Prune(chains)

	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.tracks {
		chain, ok := chains[key.chain]
		if !ok || !keepsKey(key, chain.Points) {
			delete(m.tracks, key)
		}
	}
	for key := range m.windows {
		if !slices.Contains(chains[key.chain].Points, key.point) {
			delete(m.windows, key)
		}
	}
}

// Findings returns the active findings of a chain, sorted by kind, then From.
func (m *Monitor) Findings(chainID string) []energy.ChainFinding {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.findingsLocked(func(id string) bool { return id == chainID })
}

// AllFindings returns the active findings of every chain, sorted by chain ID,
// then kind and From.
func (m *Monitor) AllFindings() []energy.ChainFinding {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.findingsLocked(func(string) bool { return true })
}

func (m *Monitor) findingsLocked(include func(chainID string) bool) []energy.ChainFinding {
	var out []energy.ChainFinding
	for key, e := range m.tracks {
		if !e.active || !include(key.chain) {
			continue
		}
		out = append(out, energy.ChainFinding{
			Kind:       outputKind(key.kind),
			ChainID:    key.chain,
			ChainLabel: m.labels[key.chain],
			Since:      e.since,
			From:       e.from,
			To:         e.to,
			Suggestion: e.sugg,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.ChainID != b.ChainID {
			return a.ChainID < b.ChainID
		}
		if ra, rb := kindRank(a.Kind), kindRank(b.Kind); ra != rb {
			return ra < rb
		}
		if a.From != b.From {
			return a.From < b.From
		}
		return a.To < b.To
	})
	return out
}

// outputKind is the kind a finding shows. The switched-off tracker kind shows
// as a contradiction.
func outputKind(kind string) string {
	if kind == switchOffKind {
		return energy.FindingContradiction
	}
	return kind
}

// kindRank orders the kinds in the order the findings show them.
func kindRank(kind string) int {
	switch kind {
	case energy.FindingUnit:
		return 0
	case energy.FindingFrozen:
		return 1
	case energy.FindingContradiction:
		return 2
	case energy.FindingSign:
		return 3
	default:
		return 4
	}
}

// Segment returns what is learned about source to target in the load class of
// countingW. It implements energy.ChainInsight.
func (m *Monitor) Segment(source, target string, countingW float64) energy.ChainSegmentInsight {
	return m.table.Segment(energystats.ChainPair{Source: source, Target: target}, countingW, m.now())
}

// PairLearned reports whether a-b has a learned class. It implements
// energy.ChainInsight.
func (m *Monitor) PairLearned(a, b string) bool {
	return m.table.PairLearned(a, b, m.now())
}

// Curve returns the efficiency curve of the points. It implements
// energy.ChainInsight.
func (m *Monitor) Curve(points []string) energy.ChainCurve {
	return m.table.Curve(points)
}

// keepsKey reports whether the points still hold the finding of key: both
// points of a segment adjacent in chain order, the frozen point in the chain.
func keepsKey(key trackKey, points []string) bool {
	i := slices.Index(points, key.a)
	if key.kind == energy.FindingFrozen {
		return i >= 0
	}
	j := slices.Index(points, key.b)
	if key.kind == switchOffKind {
		return i >= 0 && j >= 0
	}
	return i >= 0 && j >= 0 && (i-j == 1 || j-i == 1)
}

// syncPair brings the entry of pair in line with the scales and inverts of its
// points, the source first. A changed scale drops the pair in both directions.
// A changed invert drops only the sign counters. The entry is created when it
// is missing.
func (t *Table) syncPair(pair energystats.ChainPair, src, dst energy.ChainPointState, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry := t.pairs[pair]
	if entry != nil && (entry.ScaleSource != src.Scale || entry.ScaleTarget != dst.Scale) {
		t.resetLocked(pair)
		entry = nil
	}
	if entry == nil {
		entry = t.entryLocked(pair, now)
		entry.ScaleSource, entry.ScaleTarget = src.Scale, dst.Scale
		entry.InvertSource, entry.InvertTarget = src.Invert, dst.Invert
		t.dirty = true
		return
	}
	if entry.InvertSource != src.Invert || entry.InvertTarget != dst.Invert {
		entry.InvertSource, entry.InvertTarget = src.Invert, dst.Invert
		entry.Agree, entry.Opposite = 0, 0
		t.dirty = true
	}
}

// countSign counts one sample of the pair in chain order: opposite signs or
// agreeing ones. Values below ChainMinInputW and points that are not fresh
// count nothing. The counters halve at LearnWindow.
func (t *Table) countSign(pair energystats.ChainPair, a, b energy.ChainPointState) {
	if a.Freshness != "fresh" || b.Freshness != "fresh" || !finite(a.Value) || !finite(b.Value) {
		return
	}
	if absW(a.Value) < energy.ChainMinInputW || absW(b.Value) < energy.ChainMinInputW {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	entry := t.pairs[pair]
	if entry == nil {
		return
	}
	if (a.Value > 0) != (b.Value > 0) {
		entry.Opposite++
	} else {
		entry.Agree++
	}
	if entry.Agree+entry.Opposite >= LearnWindow {
		entry.Agree /= 2
		entry.Opposite /= 2
	}
	t.dirty = true
}

// signCounts returns the sign counters of pair and the time its entry was first
// seen. ok is false when the table has no entry.
func (t *Table) signCounts(pair energystats.ChainPair) (agree, opposite int, firstSeen time.Time, ok bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	entry := t.pairs[pair]
	if entry == nil {
		return 0, 0, time.Time{}, false
	}
	return entry.Agree, entry.Opposite, entry.FirstSeen, true
}

func absW(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
