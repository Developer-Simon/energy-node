package chainwatch

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

const testChainID = "c1"

// sampleEvery is the snapshot interval of the tests.
const sampleEvery = 10 * time.Second

var t0 = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

// rig is a monitor with a clock that the tests move with every observation.
type rig struct {
	mu  sync.Mutex
	now time.Time
	tb  *Table
	m   *Monitor
}

func newRig() *rig {
	r := &rig{tb: NewTable(), now: t0}
	r.m = NewMonitor(r.tb, DefaultHolds, r.clock)
	return r
}

func (r *rig) clock() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.now
}

// observe hands a snapshot with the given chains to the monitor at at.
func (r *rig) observe(at time.Time, chains ...energy.ChainState) {
	r.mu.Lock()
	r.now = at
	r.mu.Unlock()
	r.m.Observe(at, energy.Snapshot{Chains: chains})
}

// feed observes cs every sampleEvery from start+from to start+to, inclusive.
func (r *rig) feed(start time.Time, from, to time.Duration, cs energy.ChainState) {
	for off := from; off <= to; off += sampleEvery {
		r.observe(start.Add(off), cs)
	}
}

// testPoint is a fresh measuring point reading valueW, scale 1, no invert.
func testPoint(id string, valueW float64) energy.ChainPointState {
	return energy.ChainPointState{EntityID: id, Label: id, Value: valueW, HasValue: true, Freshness: "fresh", Scale: 1}
}

// buildChain makes the state of testChainID. Points run in the normal flow
// direction, the segments follow flow ("down", "up" or "idle").
func buildChain(flow, counting string, points ...energy.ChainPointState) energy.ChainState {
	cs := energy.ChainState{ID: testChainID, Label: "Testkette", Counting: counting, Flow: flow, Points: points}
	for i := range cs.Points {
		cs.Points[i].Counting = cs.Points[i].EntityID == counting
	}
	for i := 0; i+1 < len(points); i++ {
		src, dst := points[i], points[i+1]
		if flow == "up" {
			src, dst = dst, src
		}
		cs.Segments = append(cs.Segments, energy.ChainSegmentState{
			From:      src.EntityID,
			To:        dst.EntityID,
			Available: src.Freshness == "fresh" && dst.Freshness == "fresh",
			LossW:     math.Abs(src.Value) - math.Abs(dst.Value),
		})
	}
	return cs
}

// classN returns the learned sample count of source-target in the load class
// of countingW.
func classN(tb *Table, source, target string, countingW float64) int {
	entry := tb.pairs[energystats.ChainPair{Source: source, Target: target}]
	if entry == nil {
		return 0
	}
	if b := entry.Classes[ClassOf(countingW)]; b != nil {
		return b.N
	}
	return 0
}

// totalN returns the learned sample count of source-target over all classes.
func totalN(tb *Table, source, target string) int {
	entry := tb.pairs[energystats.ChainPair{Source: source, Target: target}]
	if entry == nil {
		return 0
	}
	n := 0
	for _, b := range entry.Classes {
		n += b.N
	}
	return n
}

func TestObserveLearnsOnlyValidSamples(t *testing.T) {
	stale := func(p energy.ChainPointState) energy.ChainPointState {
		p.Freshness = "stale"
		return p
	}
	substituted := buildChain("down", "a", testPoint("a", 300), testPoint("b", 290), testPoint("c", 280))
	substituted.SubstituteFrom = "x"

	cases := []struct {
		name   string
		chain  energy.ChainState
		countW float64
		want   int
	}{
		{"fresh flow with source 300 W", buildChain("down", "a", testPoint("a", 300), testPoint("b", 290), testPoint("c", 280)), 300, 1},
		{"source 8 W", buildChain("down", "a", testPoint("a", 8), testPoint("b", 7), testPoint("c", 6)), 8, 0},
		{"idle flow", buildChain("idle", "a", testPoint("a", 300), testPoint("b", 290), testPoint("c", 280)), 300, 0},
		{"segment not available", buildChain("down", "a", testPoint("a", 300), stale(testPoint("b", 290)), testPoint("c", 280)), 300, 0},
		{"counting point stale", buildChain("down", "c", testPoint("a", 300), testPoint("b", 290), stale(testPoint("c", 280))), 280, 0},
		{"substitute value", substituted, 300, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig()
			r.observe(t0, tc.chain)
			if got := classN(r.tb, "a", "b", tc.countW); got != tc.want {
				t.Fatalf("N = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestObserveDoesNotLearnDuringFinding(t *testing.T) {
	r := newRig()
	narrow := &energy.ChainNormalRange{LowW: 0, HighW: 5}
	r.feed(t0, 0, 29*time.Minute+50*time.Second, driftChain(10, narrow))
	if classN(r.tb, "a", "b", 300) == 0 {
		t.Fatal("nothing learned before the finding")
	}

	r.feed(t0, 30*time.Minute, 30*time.Minute, driftChain(10, narrow))
	if len(r.m.Findings(testChainID)) != 1 {
		t.Fatalf("Findings = %v, want the drift finding", r.m.Findings(testChainID))
	}
	atFinding := classN(r.tb, "a", "b", 300)

	r.feed(t0, 30*time.Minute+sampleEvery, 40*time.Minute, driftChain(10, narrow))
	if got := classN(r.tb, "a", "b", 300); got != atFinding {
		t.Fatalf("N grew from %d to %d during the finding", atFinding, got)
	}
}

func TestScaleChangeResetsPair(t *testing.T) {
	r := newRig()
	ab := energystats.ChainPair{Source: "a", Target: "b"}
	r.feed(t0, 0, 10*time.Minute, buildChain("down", "a", testPoint("a", 300), testPoint("b", 290)))
	if classN(r.tb, "a", "b", 300) == 0 {
		t.Fatal("nothing learned before the scale change")
	}

	scaled := testPoint("b", 290)
	scaled.Scale = 2
	at := t0.Add(10*time.Minute + sampleEvery)
	r.observe(at, buildChain("down", "a", testPoint("a", 300), scaled))

	if n := classN(r.tb, "a", "b", 300); n != 1 {
		t.Fatalf("N = %d after the scale change, want 1 (only the new sample)", n)
	}
	if fs := r.tb.pairs[ab].FirstSeen; !fs.Equal(at) {
		t.Fatalf("FirstSeen = %v, want %v", fs, at)
	}
}

func TestInvertChangeResetsOnlyCounters(t *testing.T) {
	r := newRig()
	ab := energystats.ChainPair{Source: "a", Target: "b"}
	r.feed(t0, 0, 10*time.Minute, buildChain("down", "a", testPoint("a", 300), testPoint("b", 290)))
	before := r.tb.pairs[ab]
	if before.Agree == 0 {
		t.Fatal("no sign counters before the invert change")
	}
	n := classN(r.tb, "a", "b", 300)
	firstSeen := before.FirstSeen

	// A substitute value, so the sample itself does not learn; the counter sees the new sign.
	inverted := testPoint("b", -290)
	inverted.Invert = true
	substituted := buildChain("down", "a", testPoint("a", 300), inverted)
	substituted.SubstituteFrom = "x"
	r.observe(t0.Add(10*time.Minute+sampleEvery), substituted)

	entry := r.tb.pairs[ab]
	if entry.Agree != 0 || entry.Opposite != 1 {
		t.Fatalf("Agree, Opposite = %d, %d, want 0, 1", entry.Agree, entry.Opposite)
	}
	if got := classN(r.tb, "a", "b", 300); got != n {
		t.Fatalf("N = %d after the invert change, want %d", got, n)
	}
	if !entry.FirstSeen.Equal(firstSeen) {
		t.Fatalf("FirstSeen changed on an invert change: %v", entry.FirstSeen)
	}
}

func TestChainRemovedDropsTrackers(t *testing.T) {
	r := newRig()
	r.feed(t0, 0, 30*time.Minute, driftChain(10, &energy.ChainNormalRange{LowW: 0, HighW: 5}))
	if len(r.m.Findings(testChainID)) != 1 {
		t.Fatal("no drift finding before the chain is removed")
	}

	r.observe(t0.Add(30*time.Minute + sampleEvery))
	if got := r.m.Findings(testChainID); len(got) != 0 {
		t.Fatalf("Findings = %v after the chain is gone, want none", got)
	}
	if got := r.m.AllFindings(); len(got) != 0 {
		t.Fatalf("AllFindings = %v after the chain is gone, want none", got)
	}
	if len(r.m.tracks) != 0 {
		t.Fatalf("%d trackers left after the chain is gone", len(r.m.tracks))
	}
}

func TestRelearnClearsPairAndFindings(t *testing.T) {
	r := newRig()
	r.feed(t0, 0, 30*time.Minute, driftChain(10, &energy.ChainNormalRange{LowW: 0, HighW: 5}))
	if len(r.m.Findings(testChainID)) != 1 {
		t.Fatal("no drift finding before Relearn")
	}

	r.m.Relearn("a", "b")

	if got := r.m.Findings(testChainID); len(got) != 0 {
		t.Fatalf("Findings = %v after Relearn, want none", got)
	}
	for _, pair := range []energystats.ChainPair{{Source: "a", Target: "b"}, {Source: "b", Target: "a"}} {
		if _, ok := r.tb.pairs[pair]; ok {
			t.Fatalf("pair %v still in the table after Relearn", pair)
		}
	}
	if r.m.PairLearned("a", "b") {
		t.Fatal("PairLearned after Relearn = true")
	}
}

func TestObserveIsSafeWithConcurrentReads(t *testing.T) {
	r := newRig()
	done := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				r.m.Segment("a", "b", 300)
				r.m.Findings(testChainID)
				r.m.Curve([]string{"a", "b"})
				r.m.AllFindings()
			}
		}()
	}

	r.feed(t0, 0, 30*time.Minute, driftChain(10, &energy.ChainNormalRange{LowW: 0, HighW: 5}))
	r.feed(t0, 31*time.Minute, 40*time.Minute, buildChain("down", "a", testPoint("a", 300), testPoint("b", 290)))
	close(done)
	wg.Wait()
}

func TestObserveDoesNotLearnDuringFrozenOrContradiction(t *testing.T) {
	// Frozen: a stays at 120 W, b swings. Learning stops once frozen shows.
	r := newRig()
	r.feedSwing(t0, 14*time.Minute+50*time.Second, 120, 80, 160)
	if classN(r.tb, "a", "b", 120) == 0 {
		t.Fatal("nothing learned before frozen")
	}
	r.observe(t0.Add(15*time.Minute), buildChain("down", "a", testPoint("a", 120), testPoint("b", 80)))
	if len(frozenFindings(r)) != 1 {
		t.Fatal("no frozen finding at 15:00")
	}
	atFinding := classN(r.tb, "a", "b", 120)
	r.feedSwing(t0, 25*time.Minute, 120, 80, 160)
	if got := classN(r.tb, "a", "b", 120); got != atFinding {
		t.Fatalf("N grew from %d to %d during frozen", atFinding, got)
	}

	// Contradiction: a 200 W into b at 0 W, learning stops once it shows.
	r = newRig()
	r.feed(t0, 0, 9*time.Minute+50*time.Second, buildChain("down", "a", testPoint("a", 200), testPoint("b", 0)))
	before := classN(r.tb, "a", "b", 200)
	r.feed(t0, 10*time.Minute, 10*time.Minute, buildChain("down", "a", testPoint("a", 200), testPoint("b", 0)))
	if len(r.m.Findings(testChainID)) != 1 {
		t.Fatal("no contradiction at 10:00")
	}
	atFinding = classN(r.tb, "a", "b", 200)
	if atFinding < before {
		t.Fatalf("N fell from %d to %d", before, atFinding)
	}
	r.feed(t0, 10*time.Minute+sampleEvery, 20*time.Minute, buildChain("down", "a", testPoint("a", 200), testPoint("b", 0)))
	if got := classN(r.tb, "a", "b", 200); got != atFinding {
		t.Fatalf("N grew from %d to %d during contradiction", atFinding, got)
	}
}

func TestObserveSkipsNonFiniteSamples(t *testing.T) {
	r := newRig()
	nan := testPoint("b", math.NaN())
	r.observe(t0, buildChain("down", "a", testPoint("a", 300), nan))
	inf := testPoint("b", math.Inf(1))
	r.observe(t0.Add(sampleEvery), buildChain("down", "a", testPoint("a", 300), inf))
	// The counting point is c, the source of a-b is finite: only the counting value is bad.
	r.observe(t0.Add(2*sampleEvery), buildChain("down", "c", testPoint("a", 300), testPoint("b", 290), testPoint("c", math.NaN())))
	if got := totalN(r.tb, "a", "b"); got != 0 {
		t.Fatalf("N = %d over all classes for non-finite samples, want 0", got)
	}
}

func TestAllFindingsOrder(t *testing.T) {
	m := NewMonitor(NewTable(), DefaultHolds, func() time.Time { return t0 })
	add := func(chain, kind, a, b, from string) {
		m.tracks[trackKey{chain: chain, kind: kind, a: a, b: b}] = &trackEntry{
			tracker: tracker{since: t0, active: true},
			from:    from,
			to:      b,
		}
	}
	add("c1", energy.FindingDrift, "r", "s", "b")
	add("c1", energy.FindingDrift, "p", "q", "a")
	add("c1", energy.FindingSign, "p", "q", "z")
	add("c1", energy.FindingUnit, "p", "q", "b")
	add("c1", energy.FindingFrozen, "c", "", "c")
	add("c1", energy.FindingContradiction, "p", "q", "a")
	add("c1", energy.FindingUnit, "x", "y", "x")
	m.tracks[trackKey{chain: "c1", kind: energy.FindingUnit, a: "x", b: "y"}].active = false
	add("c2", energy.FindingDrift, "p", "q", "a")

	type row struct{ chain, kind, from string }
	want := []row{
		{"c1", energy.FindingUnit, "b"},
		{"c1", energy.FindingFrozen, "c"},
		{"c1", energy.FindingContradiction, "a"},
		{"c1", energy.FindingSign, "z"},
		{"c1", energy.FindingDrift, "a"},
		{"c1", energy.FindingDrift, "b"},
		{"c2", energy.FindingDrift, "a"},
	}
	toRows := func(fs []energy.ChainFinding) []row {
		var out []row
		for _, f := range fs {
			out = append(out, row{f.ChainID, f.Kind, f.From})
		}
		return out
	}
	if got := toRows(m.AllFindings()); !reflect.DeepEqual(got, want) {
		t.Fatalf("AllFindings = %v, want %v", got, want)
	}
	if got := toRows(m.Findings("c1")); !reflect.DeepEqual(got, want[:6]) {
		t.Fatalf("Findings(c1) = %v, want %v", got, want[:6])
	}
}

func TestSignDoesNotStopLearning(t *testing.T) {
	r := newRig()
	r.observe(t0, signChain(200))
	for off := sampleEvery; off <= 24*time.Hour; off += sampleEvery {
		r.observe(t0.Add(off), signAt(off, -200))
	}
	if len(findingsOf(r, energy.FindingSign)) != 1 {
		t.Fatal("no sign finding at 24:00")
	}
	bin := r.tb.pairs[energystats.ChainPair{Source: "a", Target: "b"}].Classes[ClassOf(200)]
	before := bin.TargetW

	// The reverse flow now reads 230 W on b, the table must follow it while sign is active.
	for off := 24*time.Hour + sampleEvery; off <= 24*time.Hour+10*time.Minute; off += sampleEvery {
		r.observe(t0.Add(off), signAt(off, -230))
	}
	if bin.TargetW <= before {
		t.Fatalf("TargetW = %v, want above %v: learning stopped during sign", bin.TargetW, before)
	}
}

func TestObserveDoesNotLearnDuringUnit(t *testing.T) {
	r := newRig()
	r.feed(t0, 0, 14*time.Minute+50*time.Second, unitChain(300, 300000))
	if classN(r.tb, "a", "b", 300) == 0 {
		t.Fatal("nothing learned before the unit finding")
	}
	r.feed(t0, 15*time.Minute, 15*time.Minute, unitChain(300, 300000))
	if len(findingsOf(r, energy.FindingUnit)) != 1 {
		t.Fatal("no unit finding at 15:00")
	}
	atFinding := classN(r.tb, "a", "b", 300)
	r.feed(t0, 15*time.Minute+sampleEvery, 25*time.Minute, unitChain(300, 300000))
	if got := classN(r.tb, "a", "b", 300); got != atFinding {
		t.Fatalf("N grew from %d to %d during the unit finding", atFinding, got)
	}
}

func TestScaleChangeResetsUnitLearning(t *testing.T) {
	r := newRig()
	r.feed(t0, 0, 15*time.Minute, unitChain(300, 300000))
	if totalN(r.tb, "a", "b") == 0 {
		t.Fatal("nothing learned before the scale change")
	}

	// The scale of a is corrected, the values agree now and the unit finding is
	// still shown until Clear has passed.
	fixed := testPoint("a", 300000)
	fixed.Scale = 1000
	fixedChain := func() energy.ChainState { return buildChain("down", "a", fixed, testPoint("b", 300000)) }
	at := t0.Add(15*time.Minute + sampleEvery)
	r.observe(at, fixedChain())
	if totalN(r.tb, "a", "b") != 0 {
		t.Fatalf("N = %d after the scale change, the wrong samples must be gone", totalN(r.tb, "a", "b"))
	}
	if len(findingsOf(r, energy.FindingUnit)) != 1 {
		t.Fatal("unit finding ended before Clear")
	}

	// The last true reading is at 15:00, Clear is 5 min: the finding ends at 20:00.
	r.feed(t0, 15*time.Minute+2*sampleEvery, 19*time.Minute+50*time.Second, fixedChain())
	if len(findingsOf(r, energy.FindingUnit)) != 1 {
		t.Fatal("unit finding ended before Clear had passed")
	}
	r.observe(t0.Add(20*time.Minute), fixedChain())
	if len(findingsOf(r, energy.FindingUnit)) != 0 {
		t.Fatal("unit finding still shown after Clear")
	}
	before := totalN(r.tb, "a", "b")
	r.observe(t0.Add(20*time.Minute+sampleEvery), fixedChain())
	if got := totalN(r.tb, "a", "b"); got <= before {
		t.Fatalf("N = %d after the unit ended, want the pair to learn anew (was %d)", got, before)
	}
}

func TestCountersSkipIdleFlow(t *testing.T) {
	r := newRig()
	pair := energystats.ChainPair{Source: "a", Target: "b"}
	r.observe(t0, buildChain("idle", "a", testPoint("a", 300), testPoint("b", -300)))
	if e := r.tb.pairs[pair]; e == nil || e.Opposite != 0 || e.Agree != 0 {
		t.Fatalf("counters moved with the flow idle: %+v", e)
	}
	r.observe(t0.Add(sampleEvery), buildChain("down", "a", testPoint("a", 300), testPoint("b", -300)))
	if e := r.tb.pairs[pair]; e.Opposite != 1 {
		t.Fatalf("Opposite = %d with the flow running, want 1", e.Opposite)
	}
}

// learnOne feeds one fresh chain sample that learns a pair, so the table is dirty.
func learnOne(r *rig, at time.Time) {
	r.observe(at, buildChain("down", "a", testPoint("a", 300), testPoint("b", 290), testPoint("c", 280)))
}

func fileMtime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.ModTime()
}

func TestMonitorSavesHourlyWhenDirty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "learned.json")
	r := newRig()
	r.m.SetPersistence(path, func(string, ...any) {})

	learnOne(r, t0.Add(10*time.Minute))
	r.observe(t0.Add(59*time.Minute), energy.ChainState{})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("saved before the first hour: %v", err)
	}

	r.observe(t0.Add(time.Hour), energy.ChainState{})
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("not saved after one hour: %v", err)
	}
	loaded := Load(path, nil)
	if n := classN(loaded, "a", "b", 300); n != 1 {
		t.Fatalf("saved N = %d, want 1", n)
	}

	// Without a change the next hour writes nothing: a sentinel mtime stays.
	sentinel := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, sentinel, sentinel); err != nil {
		t.Fatal(err)
	}
	r.observe(t0.Add(2*time.Hour+time.Minute), energy.ChainState{})
	if got := fileMtime(t, path); !got.Equal(sentinel) {
		t.Fatalf("unchanged table was rewritten, mtime %v", got)
	}
}

func TestMonitorCloseSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "learned.json")
	r := newRig()
	r.m.SetPersistence(path, func(string, ...any) {})
	learnOne(r, t0.Add(time.Minute))

	r.m.Close()
	if n := classN(Load(path, nil), "a", "b", 300); n != 1 {
		t.Fatalf("Close did not save the learned sample")
	}
}

func TestMonitorSaveErrorIsLoggedAndRetried(t *testing.T) {
	// A directory at the file path makes the rename fail.
	path := filepath.Join(t.TempDir(), "learned.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	var logs []string
	r := newRig()
	r.m.SetPersistence(path, func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	})
	learnOne(r, t0.Add(time.Minute))

	r.observe(t0.Add(time.Hour), energy.ChainState{})
	if len(logs) != 1 {
		t.Fatalf("logged %d times after the failed save, want 1: %v", len(logs), logs)
	}

	// Fix the path. The retry comes saveRetry later, not before.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	r.observe(t0.Add(time.Hour+saveRetry/2), energy.ChainState{})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("retried before saveRetry: %v", err)
	}
	r.observe(t0.Add(time.Hour+saveRetry), energy.ChainState{})
	if n := classN(Load(path, nil), "a", "b", 300); n != 1 {
		t.Fatalf("retry did not save the learned sample")
	}
}

func TestMonitorLearningOff(t *testing.T) {
	r := newRig()
	chain := buildChain("down", "a", testPoint("a", 300), testPoint("b", 290), testPoint("c", 280))
	r.feed(t0, 0, time.Minute, chain)
	before := classN(r.tb, "a", "b", 300)
	if before == 0 {
		t.Fatal("nothing learned before SetLearning(false)")
	}

	r.m.SetLearning(false)
	if r.m.LearningOn() {
		t.Fatal("LearningOn after SetLearning(false) = true")
	}
	r.feed(t0, 2*time.Minute, 10*time.Minute, chain)
	if got := classN(r.tb, "a", "b", 300); got != before {
		t.Fatalf("N grew from %d to %d with learning off", before, got)
	}

	r.m.SetLearning(true)
	r.feed(t0, 11*time.Minute, 12*time.Minute, chain)
	if got := classN(r.tb, "a", "b", 300); got <= before {
		t.Fatalf("N = %d after SetLearning(true), want more than %d", got, before)
	}
}

func TestMonitorLearningOffKeepsDetectors(t *testing.T) {
	r := newRig()
	r.m.SetLearning(false)
	r.feed(t0, 0, 15*time.Minute, unitChain(0.3, 300))
	if got := findingsOf(r, energy.FindingUnit); len(got) != 1 {
		t.Fatalf("unit findings with learning off = %d, want 1", len(got))
	}
}
