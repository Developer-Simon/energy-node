package chainwatch

import (
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

// driftChain is a two-point chain a to b, flow down, with a loss of lossW and
// the given normal band on its only segment.
func driftChain(lossW float64, band *energy.ChainNormalRange) energy.ChainState {
	cs := buildChain("down", "a", testPoint("a", 300), testPoint("b", 300-lossW))
	cs.Segments[0].Normal = band
	return cs
}

var narrowBand = &energy.ChainNormalRange{LowW: 0, HighW: 5}

func TestDriftNeedsLearnedClass(t *testing.T) {
	r := newRig()
	r.feed(t0, 0, 2*time.Hour, driftChain(100, nil))
	if got := r.m.Findings(testChainID); len(got) != 0 {
		t.Fatalf("Findings = %v without a learned class, want none", got)
	}
}

func TestDriftAfterHold(t *testing.T) {
	r := newRig()
	r.feed(t0, 0, 29*time.Minute+50*time.Second, driftChain(10, narrowBand))
	if got := r.m.Findings(testChainID); len(got) != 0 {
		t.Fatalf("Findings = %v at 29:50, want none", got)
	}

	r.feed(t0, 30*time.Minute, 30*time.Minute, driftChain(10, narrowBand))
	got := r.m.Findings(testChainID)
	if len(got) != 1 {
		t.Fatalf("Findings = %v at 30:00, want one", got)
	}
	f := got[0]
	if f.Kind != energy.FindingDrift || f.ChainID != testChainID {
		t.Fatalf("finding = %+v, want drift on %s", f, testChainID)
	}
	if !f.Since.Equal(t0) {
		t.Fatalf("Since = %v, want %v", f.Since, t0)
	}
	if f.From != "a" || f.To != "b" {
		t.Fatalf("From, To = %q, %q, want a, b", f.From, f.To)
	}
}

func TestDriftFromToFollowFlow(t *testing.T) {
	r := newRig()
	// Flow up: the segment runs from b to a, its loss is 290 - 300 = -10.
	up := buildChain("up", "a", testPoint("a", 300), testPoint("b", 290))
	up.Segments[0].Normal = narrowBand
	r.feed(t0, 0, 30*time.Minute, up)
	got := r.m.Findings(testChainID)
	if len(got) != 1 {
		t.Fatalf("Findings = %v, want one", got)
	}
	if got[0].From != "b" || got[0].To != "a" {
		t.Fatalf("From, To = %q, %q, want b, a", got[0].From, got[0].To)
	}
}

func TestDriftInBandResetsHold(t *testing.T) {
	r := newRig()
	r.feed(t0, 0, 19*time.Minute+50*time.Second, driftChain(10, narrowBand))
	r.feed(t0, 20*time.Minute, 20*time.Minute, driftChain(2, narrowBand))
	r.feed(t0, 20*time.Minute+sampleEvery, 50*time.Minute, driftChain(10, narrowBand))
	if got := r.m.Findings(testChainID); len(got) != 0 {
		t.Fatalf("Findings = %v at 50:00, the hold restarted at 20:10 and should not be reached yet", got)
	}

	r.feed(t0, 50*time.Minute+sampleEvery, 50*time.Minute+sampleEvery, driftChain(10, narrowBand))
	got := r.m.Findings(testChainID)
	if len(got) != 1 {
		t.Fatalf("Findings = %v at 50:10, want one", got)
	}
	if want := t0.Add(20*time.Minute + sampleEvery); !got[0].Since.Equal(want) {
		t.Fatalf("Since = %v, want %v", got[0].Since, want)
	}
}

func TestDriftUnknownSamplesKeepHold(t *testing.T) {
	r := newRig()
	r.feed(t0, 0, 14*time.Minute+50*time.Second, driftChain(10, narrowBand))
	r.feed(t0, 15*time.Minute, 19*time.Minute+50*time.Second, driftChain(10, nil))
	r.feed(t0, 20*time.Minute, 29*time.Minute+50*time.Second, driftChain(10, narrowBand))
	if got := r.m.Findings(testChainID); len(got) != 0 {
		t.Fatalf("Findings = %v at 29:50, want none", got)
	}

	r.feed(t0, 30*time.Minute, 30*time.Minute, driftChain(10, narrowBand))
	got := r.m.Findings(testChainID)
	if len(got) != 1 {
		t.Fatalf("Findings = %v at 30:00, want one", got)
	}
	if !got[0].Since.Equal(t0) {
		t.Fatalf("Since = %v, want the start %v, the unknown gap must not reset it", got[0].Since, t0)
	}
}

func TestDriftEndsAfterClear(t *testing.T) {
	r := newRig()
	r.feed(t0, 0, 30*time.Minute, driftChain(10, narrowBand))
	if len(r.m.Findings(testChainID)) != 1 {
		t.Fatal("no drift finding at 30:00")
	}

	// The last out-of-band sample is at 30:00, Clear is 5 min.
	r.feed(t0, 30*time.Minute+sampleEvery, 34*time.Minute+50*time.Second, driftChain(2, narrowBand))
	if len(r.m.Findings(testChainID)) != 1 {
		t.Fatal("finding ended before Clear had passed")
	}

	r.feed(t0, 35*time.Minute, 35*time.Minute, driftChain(2, narrowBand))
	if got := r.m.Findings(testChainID); len(got) != 0 {
		t.Fatalf("Findings = %v at 35:00, want none after Clear", got)
	}
}

func TestDriftRespectsManualBand(t *testing.T) {
	wide := &energy.ChainNormalRange{LowW: -50, HighW: 50, Manual: true}
	r := newRig()
	r.feed(t0, 0, 40*time.Minute, driftChain(10, wide))
	if got := r.m.Findings(testChainID); len(got) != 0 {
		t.Fatalf("Findings = %v inside the manual band, want none", got)
	}

	narrow := &energy.ChainNormalRange{LowW: 0, HighW: 5, Manual: true}
	r = newRig()
	r.feed(t0, 0, 30*time.Minute, driftChain(10, narrow))
	if len(r.m.Findings(testChainID)) != 1 {
		t.Fatal("no drift finding outside the manual band")
	}
}

// feedSwing observes a at aW and b swinging between lowW and highW from sample
// to sample, from t0 to to inclusive.
func (r *rig) feedSwing(start time.Time, to time.Duration, aW, lowW, highW float64) {
	i := 0
	for off := time.Duration(0); off <= to; off += sampleEvery {
		b := lowW
		if i%2 == 1 {
			b = highW
		}
		r.observe(start.Add(off), buildChain("down", "a", testPoint("a", aW), testPoint("b", b)))
		i++
	}
}

func frozenFindings(r *rig) []energy.ChainFinding {
	var out []energy.ChainFinding
	for _, f := range r.m.Findings(testChainID) {
		if f.Kind == energy.FindingFrozen {
			out = append(out, f)
		}
	}
	return out
}

func TestFrozenWhileOtherMoves(t *testing.T) {
	r := newRig()
	r.feedSwing(t0, 14*time.Minute+50*time.Second, 120, 80, 160)
	if got := frozenFindings(r); len(got) != 0 {
		t.Fatalf("frozen = %v at 14:50, want none", got)
	}

	r.observe(t0.Add(15*time.Minute), buildChain("down", "a", testPoint("a", 120), testPoint("b", 80)))
	got := frozenFindings(r)
	if len(got) != 1 {
		t.Fatalf("frozen = %v at 15:00, want one", got)
	}
	if got[0].From != "a" || got[0].To != "" {
		t.Fatalf("From, To = %q, %q, want a and an empty To", got[0].From, got[0].To)
	}
}

func TestFrozenNeedsMovement(t *testing.T) {
	r := newRig()
	// b moves 15 W, below FrozenMoveW.
	r.feedSwing(t0, 30*time.Minute, 120, 100, 115)
	if got := frozenFindings(r); len(got) != 0 {
		t.Fatalf("frozen = %v with b moving 15 W, want none", got)
	}

	// Everything constant: nothing moves, nothing is frozen.
	r = newRig()
	r.feed(t0, 0, 30*time.Minute, buildChain("down", "a", testPoint("a", 120), testPoint("b", 120)))
	if got := frozenFindings(r); len(got) != 0 {
		t.Fatalf("frozen = %v with all values constant, want none", got)
	}
}

func TestFrozenIgnoresZero(t *testing.T) {
	r := newRig()
	// a stays at 0 W, b swings 40 to 60 W: no frozen, and no contradiction either.
	r.feedSwing(t0, 30*time.Minute, 0, 40, 60)
	if got := r.m.Findings(testChainID); len(got) != 0 {
		t.Fatalf("Findings = %v with a at 0 W, want none", got)
	}
}

func TestFrozenEndsOnFirstChange(t *testing.T) {
	r := newRig()
	r.feedSwing(t0, 15*time.Minute, 120, 80, 160)
	if len(frozenFindings(r)) != 1 {
		t.Fatal("no frozen finding before the change")
	}

	r.observe(t0.Add(15*time.Minute+sampleEvery), buildChain("down", "a", testPoint("a", 121), testPoint("b", 80)))
	if got := frozenFindings(r); len(got) != 0 {
		t.Fatalf("frozen = %v after a changed, want none at once", got)
	}
}

func TestFrozenIgnoresMovementOfStalePoint(t *testing.T) {
	r := newRig()
	for i, off := 0, time.Duration(0); off <= 30*time.Minute; i, off = i+1, off+sampleEvery {
		b := testPoint("b", 80)
		if i%2 == 1 {
			b.Value = 160
		}
		b.Freshness = "stale"
		r.observe(t0.Add(off), buildChain("down", "a", testPoint("a", 120), b))
	}
	if got := frozenFindings(r); len(got) != 0 {
		t.Fatalf("frozen = %v on a with b stale, want none", got)
	}
}

func TestContradictionZeroPoint(t *testing.T) {
	r := newRig()
	r.feed(t0, 0, 9*time.Minute+50*time.Second, buildChain("down", "a", testPoint("a", 200), testPoint("b", 0)))
	if got := r.m.Findings(testChainID); len(got) != 0 {
		t.Fatalf("Findings = %v at 9:50, want none", got)
	}
	r.feed(t0, 10*time.Minute, 10*time.Minute, buildChain("down", "a", testPoint("a", 200), testPoint("b", 0)))
	got := r.m.Findings(testChainID)
	if len(got) != 1 || got[0].Kind != energy.FindingContradiction || got[0].From != "a" || got[0].To != "b" {
		t.Fatalf("Findings = %+v at 10:00, want one contradiction a to b", got)
	}

	r = newRig()
	r.feed(t0, 0, 30*time.Minute, buildChain("down", "a", testPoint("a", 40), testPoint("b", 0)))
	if got := r.m.Findings(testChainID); len(got) != 0 {
		t.Fatalf("Findings = %v with a at 40 W, want none", got)
	}
}

func TestContradictionIgnoresLearnedIdleDraw(t *testing.T) {
	withBand := func(lo, hi float64) energy.ChainState {
		cs := buildChain("down", "a", testPoint("a", 30), testPoint("b", 0))
		if lo != hi {
			cs.Segments[0].Normal = &energy.ChainNormalRange{LowW: lo, HighW: hi}
		}
		return cs
	}

	r := newRig()
	r.feed(t0, 0, 30*time.Minute, withBand(0, 0))
	if got := r.m.Findings(testChainID); len(got) != 0 {
		t.Fatalf("no band, a 30 W: Findings = %v, want none", got)
	}

	r = newRig()
	r.feed(t0, 0, 30*time.Minute, withBand(25, 35))
	if got := r.m.Findings(testChainID); len(got) != 0 {
		t.Fatalf("band 25 to 35, a 30 W: Findings = %v, want none", got)
	}

	r = newRig()
	r.feed(t0, 0, 10*time.Minute, withBand(5, 15))
	if got := r.m.Findings(testChainID); len(got) != 1 || got[0].Kind != energy.FindingContradiction {
		t.Fatalf("band 5 to 15, a 30 W: Findings = %v at 10:00, want one contradiction", got)
	}
}

func TestContradictionSwitchOff(t *testing.T) {
	off := func(aW, bW float64) energy.ChainState {
		a := testPoint("a", aW)
		a.SwitchOff = true
		return buildChain("down", "a", a, testPoint("b", bW))
	}

	// The switched-off point a stays quiet, b shows 25 W.
	r := newRig()
	r.feed(t0, 0, 10*time.Minute, off(0, 25))
	got := r.m.Findings(testChainID)
	if len(got) != 1 || got[0].Kind != energy.FindingContradiction || got[0].From != "b" || got[0].To != "a" {
		t.Fatalf("Findings = %v, want one contradiction b to a", got)
	}

	// The switched-off point itself shows 25 W.
	r = newRig()
	r.feed(t0, 0, 10*time.Minute, off(25, 5))
	got = r.m.Findings(testChainID)
	if len(got) != 1 || got[0].From != "a" || got[0].To != "a" {
		t.Fatalf("Findings = %v, want one contradiction a to a", got)
	}

	// Everything below 20 W.
	r = newRig()
	r.feed(t0, 0, 30*time.Minute, off(0, 10))
	if got := r.m.Findings(testChainID); len(got) != 0 {
		t.Fatalf("Findings = %v below 20 W, want none", got)
	}
}

func TestContradictionOnlyAdjacentForZero(t *testing.T) {
	r := newRig()
	r.feed(t0, 0, 10*time.Minute, buildChain("down", "a",
		testPoint("a", 200), testPoint("b", 195), testPoint("c", 0)))
	got := r.m.Findings(testChainID)
	if len(got) != 1 || got[0].Kind != energy.FindingContradiction || got[0].From != "b" || got[0].To != "c" {
		t.Fatalf("Findings = %+v, want exactly one contradiction b to c", got)
	}
}

func TestDriftSuppressedByFrozenOnSamePair(t *testing.T) {
	// a stays at 120 W, b swings by 20 W around it: the loss leaves the band,
	// but frozen on a explains it from 15 min on.
	r := newRig()
	narrow := &energy.ChainNormalRange{LowW: 0, HighW: 5}
	for i, off := 0, time.Duration(0); off <= 45*time.Minute; i, off = i+1, off+sampleEvery {
		b := 100.0
		if i%2 == 1 {
			b = 140
		}
		cs := buildChain("down", "a", testPoint("a", 120), testPoint("b", b))
		cs.Segments[0].Normal = narrow
		r.observe(t0.Add(off), cs)
	}
	for _, f := range r.m.Findings(testChainID) {
		if f.Kind == energy.FindingDrift {
			t.Fatalf("drift = %+v, want it suppressed by frozen", f)
		}
	}
	if len(frozenFindings(r)) != 1 {
		t.Fatalf("frozen = %v, want one", frozenFindings(r))
	}
}

// findingsOf returns the active findings of one kind on the test chain.
func findingsOf(r *rig, kind string) []energy.ChainFinding {
	var out []energy.ChainFinding
	for _, f := range r.m.Findings(testChainID) {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

// signChain is a at 200 W and b at bW, counting a: a and b agree in sign when bW is positive.
func signChain(bW float64) energy.ChainState {
	return buildChain("down", "a", testPoint("a", 200), testPoint("b", bW))
}

// signAt is the sign chain at off after t0. a alternates between 200 and 210 W,
// both in the same load class, so that a is never frozen while b changes.
func signAt(off time.Duration, bW float64) energy.ChainState {
	aW := 200.0
	if (off/sampleEvery)%2 == 1 {
		aW = 210
	}
	return buildChain("down", "a", testPoint("a", aW), testPoint("b", bW))
}

func TestSignFromCounters(t *testing.T) {
	r := newRig()
	r.observe(t0, signChain(200))
	// 359 opposite samples from 24 h on: not yet enough.
	for off := 24 * time.Hour; off <= 24*time.Hour+3580*time.Second; off += sampleEvery {
		r.observe(t0.Add(off), signAt(off, -200))
	}
	if got := findingsOf(r, energy.FindingSign); len(got) != 0 {
		t.Fatalf("sign = %v with 359 opposite samples, want none", got)
	}

	r.observe(t0.Add(24*time.Hour+3590*time.Second), signAt(24*time.Hour+3590*time.Second, -200))
	got := findingsOf(r, energy.FindingSign)
	if len(got) != 1 {
		t.Fatalf("sign = %v with 360 opposite samples, want one", got)
	}
	if s := got[0].Suggestion; s == nil || s.EntityID != "b" || !s.Invert {
		t.Fatalf("Suggestion = %+v, want invert on b", s)
	}
}

func TestSignOneWayMeterStaysQuiet(t *testing.T) {
	r := newRig()
	r.observe(t0, signChain(200))
	// First evening: four hours of reverse flow, the pair is younger than 24 h.
	for off := sampleEvery; off <= 4*time.Hour; off += sampleEvery {
		r.observe(t0.Add(off), signAt(off, -200))
	}
	if got := findingsOf(r, energy.FindingSign); len(got) != 0 {
		t.Fatalf("sign = %v in the first evening, want none", got)
	}

	// Then charging and discharging alternate, the agreeing share passes 5 %.
	for i, off := 0, 4*time.Hour+sampleEvery; off <= 25*time.Hour; i, off = i+1, off+sampleEvery {
		b := 200.0
		if i%2 == 1 {
			b = -200
		}
		r.observe(t0.Add(off), signAt(off, b))
	}
	if got := findingsOf(r, energy.FindingSign); len(got) != 0 {
		t.Fatalf("sign = %v with alternating flow, want none", got)
	}
	e := r.tb.pairs[energystats.ChainPair{Source: "a", Target: "b"}]
	if float64(e.Agree) <= SignMaxAgreeShare*float64(e.Agree+e.Opposite) {
		t.Fatalf("Agree, Opposite = %d, %d, the agreeing share should exceed 5 %%", e.Agree, e.Opposite)
	}
}

// primeSign makes the pair of pa and pb 25 h old with enough opposite samples,
// so one more sample shows the sign finding.
func primeSign(r *rig, pa, pb energy.ChainPointState) {
	pair := energystats.ChainPair{Source: pa.EntityID, Target: pb.EntityID}
	r.tb.syncPair(pair, pa, pb, t0.Add(-25*time.Hour))
	r.tb.pairs[pair].Opposite = SignMinOpposite + 10
}

func TestSignSuggestionAvoidsCountingPoint(t *testing.T) {
	cases := []struct {
		name     string
		counting string
		pa, pb   string
		want     string
	}{
		{"counting a, pair a b", "a", "a", "b", "b"},
		{"counting b, pair a b", "b", "a", "b", "a"},
		{"counting a, pair b c", "a", "b", "c", "c"},
	}
	values := map[string]float64{"a": 200, "b": -200, "c": 200}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig()
			primeSign(r, testPoint(tc.pa, values[tc.pa]), testPoint(tc.pb, values[tc.pb]))
			r.observe(t0, buildChain("down", tc.counting,
				testPoint("a", values["a"]), testPoint("b", values["b"]), testPoint("c", values["c"])))
			got := findingsOf(r, energy.FindingSign)
			if len(got) != 1 || got[0].Suggestion == nil {
				t.Fatalf("sign = %v, want one with a suggestion", got)
			}
			if got[0].Suggestion.EntityID != tc.want || !got[0].Suggestion.Invert {
				t.Fatalf("Suggestion = %+v, want invert on %s", got[0].Suggestion, tc.want)
			}
		})
	}
}

func TestCountersSkipSmallValues(t *testing.T) {
	tb := NewTable()
	pair := energystats.ChainPair{Source: "a", Target: "b"}
	tb.syncPair(pair, testPoint("a", 0), testPoint("b", 0), t0)
	tb.countSign(pair, testPoint("a", 300), testPoint("b", 5))
	tb.countSign(pair, testPoint("a", 9.9), testPoint("b", -300))
	stale := testPoint("b", -300)
	stale.Freshness = "stale"
	tb.countSign(pair, testPoint("a", 300), stale)
	if e := tb.pairs[pair]; e.Agree != 0 || e.Opposite != 0 {
		t.Fatalf("Agree, Opposite = %d, %d, want 0, 0", e.Agree, e.Opposite)
	}
	tb.countSign(pair, testPoint("a", 300), testPoint("b", -300))
	if e := tb.pairs[pair]; e.Opposite != 1 {
		t.Fatalf("Opposite = %d after a counted sample, want 1", e.Opposite)
	}
}

func TestCountersHalveAtWindow(t *testing.T) {
	tb := NewTable()
	pair := energystats.ChainPair{Source: "a", Target: "b"}
	tb.syncPair(pair, testPoint("a", 0), testPoint("b", 0), t0)
	tb.pairs[pair].Agree = LearnWindow - 1
	tb.countSign(pair, testPoint("a", 300), testPoint("b", 300))
	if e := tb.pairs[pair]; e.Agree != LearnWindow/2 || e.Opposite != 0 {
		t.Fatalf("Agree, Opposite = %d, %d at the window, want %d, 0", e.Agree, e.Opposite, LearnWindow/2)
	}

	tb.pairs[pair].Opposite = LearnWindow - 1
	tb.pairs[pair].Agree = 0
	tb.countSign(pair, testPoint("a", 300), testPoint("b", -300))
	if e := tb.pairs[pair]; e.Opposite != LearnWindow/2 || e.Agree != 0 {
		t.Fatalf("Agree, Opposite = %d, %d at the window, want 0, %d", e.Agree, e.Opposite, LearnWindow/2)
	}
}

// unitChain is a and b at aW and bW, flow down, counting a.
func unitChain(aW, bW float64) energy.ChainState {
	return buildChain("down", "a", testPoint("a", aW), testPoint("b", bW))
}

func TestUnitFactor1000(t *testing.T) {
	r := newRig()
	r.feed(t0, 0, 15*time.Minute, unitChain(0.3, 300))
	got := findingsOf(r, energy.FindingUnit)
	if len(got) != 1 || got[0].Suggestion == nil || got[0].Suggestion.EntityID != "a" || got[0].Suggestion.ScaleFactor != 1000 {
		t.Fatalf("unit = %+v, want one with factor 1000 on a", got)
	}

	r = newRig()
	r.feed(t0, 0, 15*time.Minute, unitChain(300, 300000))
	got = findingsOf(r, energy.FindingUnit)
	if len(got) != 1 || got[0].Suggestion == nil || got[0].Suggestion.EntityID != "b" || got[0].Suggestion.ScaleFactor != 0.001 {
		t.Fatalf("unit = %+v, want one with factor 0.001 on b", got)
	}

	for _, cs := range []energy.ChainState{unitChain(0.3, 40), unitChain(1, 300)} {
		r = newRig()
		r.feed(t0, 0, 30*time.Minute, cs)
		if got := findingsOf(r, energy.FindingUnit); len(got) != 0 {
			t.Fatalf("unit = %+v for a=%v b=%v, want none", got, cs.Points[0].Value, cs.Points[1].Value)
		}
	}
}

func TestUnitEndsAfterClear(t *testing.T) {
	r := newRig()
	r.feed(t0, 0, 15*time.Minute, unitChain(0.3, 300))
	if len(findingsOf(r, energy.FindingUnit)) != 1 {
		t.Fatal("no unit finding at 15:00")
	}
	// The last true reading is at 15:00, Clear is 5 min.
	r.feed(t0, 15*time.Minute+sampleEvery, 19*time.Minute+50*time.Second, unitChain(1, 300))
	if len(findingsOf(r, energy.FindingUnit)) != 1 {
		t.Fatal("unit finding ended before Clear")
	}
	r.feed(t0, 20*time.Minute, 20*time.Minute, unitChain(1, 300))
	if got := findingsOf(r, energy.FindingUnit); len(got) != 0 {
		t.Fatalf("unit = %v at 20:00, want none after Clear", got)
	}
}

func TestDriftSuppressedByUnitOnSamePair(t *testing.T) {
	// 0.3 against 300 is a unit error, and the loss is far outside the band.
	r := newRig()
	narrow := &energy.ChainNormalRange{LowW: 0, HighW: 5}
	cs := unitChain(0.3, 300)
	cs.Segments[0].Normal = narrow
	r.feed(t0, 0, 40*time.Minute, cs)
	if len(findingsOf(r, energy.FindingUnit)) != 1 {
		t.Fatal("no unit finding")
	}
	if got := findingsOf(r, energy.FindingDrift); len(got) != 0 {
		t.Fatalf("drift = %v suppressed by unit, want none", got)
	}
}
