package chainwatch

import (
	"math"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

func putBin(tb *Table, pair energystats.ChainPair, class int, b bin) {
	entry := tb.pairs[pair]
	if entry == nil {
		entry = &pairEntry{ScaleSource: 1, ScaleTarget: 1, Classes: map[int]*bin{}}
		tb.pairs[pair] = entry
	}
	entry.Classes[class] = &b
}

func TestCurveMultipliesSegments(t *testing.T) {
	tb := NewTable()
	ab := energystats.ChainPair{Source: "a", Target: "b"}
	ba := energystats.ChainPair{Source: "b", Target: "a"}
	bc := energystats.ChainPair{Source: "b", Target: "c"}
	cb := energystats.ChainPair{Source: "c", Target: "b"}
	points := []string{"a", "b", "c"}

	putBin(tb, ab, 5, bin{N: CurveMinSamples, SourceW: 100, TargetW: 90})
	putBin(tb, bc, 5, bin{N: CurveMinSamples, SourceW: 100, TargetW: 90})
	curve := tb.Curve(points)
	if len(curve.Down) != 1 {
		t.Fatalf("Down = %v, want one point", curve.Down)
	}
	if curve.Down[0].PowerW != 275 || math.Abs(curve.Down[0].Efficiency-0.81) > 1e-9 {
		t.Fatalf("Down[0] = %+v, want {275, 0.81}", curve.Down[0])
	}
	if curve.Up == nil || len(curve.Up) != 0 {
		t.Fatalf("Up = %v, want non-nil empty", curve.Up)
	}

	putBin(tb, bc, 5, bin{N: CurveMinSamples - 1, SourceW: 100, TargetW: 90})
	if got := tb.Curve(points).Down; len(got) != 0 {
		t.Fatalf("segment under CurveMinSamples: Down = %v, want none", got)
	}
	delete(tb.pairs[bc].Classes, 5)
	if got := tb.Curve(points).Down; len(got) != 0 {
		t.Fatalf("missing class: Down = %v, want none", got)
	}
	putBin(tb, bc, 5, bin{N: CurveMinSamples, SourceW: 100, TargetW: 90})

	putBin(tb, ab, 2, bin{N: CurveMinSamples, SourceW: 100, TargetW: 100})
	putBin(tb, bc, 2, bin{N: CurveMinSamples, SourceW: 100, TargetW: 100})
	down := tb.Curve(points).Down
	if len(down) != 2 || down[0].PowerW != 125 || down[1].PowerW != 275 {
		t.Fatalf("Down = %v, want classes 2 and 5 ascending", down)
	}

	putBin(tb, ba, 5, bin{N: CurveMinSamples, SourceW: 100, TargetW: 50})
	putBin(tb, cb, 5, bin{N: CurveMinSamples, SourceW: 100, TargetW: 50})
	up := tb.Curve(points).Up
	if len(up) != 1 || up[0].PowerW != 275 || math.Abs(up[0].Efficiency-0.25) > 1e-9 {
		t.Fatalf("Up = %v, want {275, 0.25}", up)
	}
}

func TestBestRange(t *testing.T) {
	tb := NewTable()
	ab := energystats.ChainPair{Source: "a", Target: "b"}
	points := []string{"a", "b"}

	for class, eff := range map[int]float64{2: 0.80, 3: 0.88, 4: 0.90, 5: 0.895, 6: 0.85} {
		putBin(tb, ab, class, bin{N: CurveMinSamples, SourceW: 100, TargetW: 100 * eff})
	}
	best := tb.Curve(points).BestDown
	if best == nil || best.FromW != 200 || best.ToW != 300 {
		t.Fatalf("BestDown = %+v, want 200 to 300", best)
	}

	gap := NewTable()
	for _, class := range []int{2, 3, 4, 6} {
		putBin(gap, ab, class, bin{N: CurveMinSamples, SourceW: 100, TargetW: 90})
	}
	best = gap.Curve(points).BestDown
	if best == nil || best.FromW != 100 || best.ToW != 250 {
		t.Fatalf("BestDown with gap = %+v, want 100 to 250", best)
	}

	few := NewTable()
	for _, class := range []int{2, 3} {
		putBin(few, ab, class, bin{N: CurveMinSamples, SourceW: 100, TargetW: 90})
	}
	if best := few.Curve(points).BestDown; best != nil {
		t.Fatalf("BestDown with two points = %+v, want nil", best)
	}
}
