package chainwatch

import (
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
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
