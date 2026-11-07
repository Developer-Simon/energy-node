package chainwatch

import (
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

// classPoint is a curve point with its load class, so that the best range can
// tell gaps apart.
type classPoint struct {
	class int
	point energy.ChainCurvePoint
}

// Curve returns the efficiency curve of a chain given as points in the normal
// flow direction. Down runs along the points, Up against them. The efficiency
// of a class is the product of the target/source ratios of the segments, and a
// class counts only if every segment has CurveMinSamples samples in it.
func (t *Table) Curve(points []string) energy.ChainCurve {
	t.mu.RLock()
	defer t.mu.RUnlock()

	reversed := make([]string, len(points))
	for i, p := range points {
		reversed[len(points)-1-i] = p
	}
	down := t.directionLocked(points)
	up := t.directionLocked(reversed)
	return energy.ChainCurve{
		Down:     toCurvePoints(down),
		Up:       toCurvePoints(up),
		BestDown: bestRange(down),
		BestUp:   bestRange(up),
	}
}

// directionLocked walks the segments of seq in order, classes ascending.
func (t *Table) directionLocked(seq []string) []classPoint {
	if len(seq) < 2 {
		return nil
	}
	var out []classPoint
	for class := 0; class <= MaxClass; class++ {
		efficiency := 1.0
		ok := true
		for i := 0; i+1 < len(seq) && ok; i++ {
			entry := t.pairs[energystats.ChainPair{Source: seq[i], Target: seq[i+1]}]
			if entry == nil {
				ok = false
				break
			}
			b := entry.Classes[class]
			if b == nil || b.N < CurveMinSamples || b.SourceW <= 0 {
				ok = false
				break
			}
			efficiency *= b.TargetW / b.SourceW
		}
		if ok {
			out = append(out, classPoint{
				class: class,
				point: energy.ChainCurvePoint{
					PowerW:     float64(class)*ClassWidthW + ClassWidthW/2,
					Efficiency: efficiency,
				},
			})
		}
	}
	return out
}

func toCurvePoints(pts []classPoint) []energy.ChainCurvePoint {
	out := make([]energy.ChainCurvePoint, 0, len(pts))
	for _, p := range pts {
		out = append(out, p.point)
	}
	return out
}

// bestRange returns the power span around the most efficient class: the run of
// adjacent classes within BestRangeSlack of the maximum. Nil if there are fewer
// than BestRangeMinClass points.
func bestRange(pts []classPoint) *energy.ChainPowerRange {
	if len(pts) < BestRangeMinClass {
		return nil
	}
	peak := 0
	for i := range pts {
		if pts[i].point.Efficiency > pts[peak].point.Efficiency {
			peak = i
		}
	}
	limit := pts[peak].point.Efficiency - BestRangeSlack
	lo, hi := peak, peak
	for lo > 0 && pts[lo-1].class == pts[lo].class-1 && pts[lo-1].point.Efficiency >= limit {
		lo--
	}
	for hi+1 < len(pts) && pts[hi+1].class == pts[hi].class+1 && pts[hi+1].point.Efficiency >= limit {
		hi++
	}
	return &energy.ChainPowerRange{
		FromW: float64(pts[lo].class) * ClassWidthW,
		ToW:   float64(pts[hi].class+1) * ClassWidthW,
	}
}
