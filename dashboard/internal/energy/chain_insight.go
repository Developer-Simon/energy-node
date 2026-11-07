package energy

import (
	"math"
	"slices"
	"time"
)

// Factors of the learned normal band. The band is centred on the learned loss
// and as wide as the largest of three terms: a multiple of the deviation, half
// the loss and a share of the source power.
const (
	normalDeviationFactor = 3.0
	normalLossFactor      = 0.5
	normalSourcePercent   = 3.0
)

// ChainTolerance is a fixed tolerance of one segment of a chain, set by the
// user instead of the learned band. A and B are two adjacent points, in the
// order of Chain.Points, and the tolerance holds for both directions. W is an
// absolute width in W, Pct a width in percent of the source power. The wider
// of the two applies.
type ChainTolerance struct {
	A   string   `json:"a"`
	B   string   `json:"b"`
	W   *float64 `json:"w,omitempty"`
	Pct *float64 `json:"pct,omitempty"`
}

// KeepAdjacentTolerances returns the tolerances whose two points still stand
// next to each other in points. Every other tolerance is dropped.
func KeepAdjacentTolerances(tolerances []ChainTolerance, points []string) []ChainTolerance {
	var out []ChainTolerance
	for _, tolerance := range tolerances {
		i, j := slices.Index(points, tolerance.A), slices.Index(points, tolerance.B)
		if i < 0 || j < 0 {
			continue
		}
		if i-j == 1 || j-i == 1 {
			out = append(out, tolerance)
		}
	}
	return out
}

// ToleranceFor returns the tolerance of the segment between a and b in either
// order, or nil when the segment has none.
func (c Chain) ToleranceFor(a, b string) *ChainTolerance {
	for i := range c.Tolerances {
		t := &c.Tolerances[i]
		if (t.A == a && t.B == b) || (t.A == b && t.B == a) {
			return t
		}
	}
	return nil
}

// ChainNormalRange is the band in W in which a segment counts as normal. Manual
// is true when a tolerance set the band instead of the learned data.
type ChainNormalRange struct {
	LowW   float64 `json:"low_w"`
	HighW  float64 `json:"high_w"`
	Manual bool    `json:"manual,omitempty"`
}

// ChainSegmentInsight is what the learned model knows about one segment in one
// load class: whether it is learned, the expected loss and the deviation.
type ChainSegmentInsight struct {
	Learned    bool
	LossW      float64
	DeviationW float64
}

// NormalBand returns the normal band of a segment. A tolerance replaces the
// learned band: its width is the wider of W and Pct of sourceW, centred on the
// learned loss. Without a tolerance the width follows the learned deviation,
// half the loss and the share of sourceW, whichever is largest.
func NormalBand(insight ChainSegmentInsight, sourceW float64, tolerance *ChainTolerance) ChainNormalRange {
	if tolerance != nil {
		half := 0.0
		if tolerance.W != nil {
			half = *tolerance.W
		}
		if tolerance.Pct != nil {
			half = math.Max(half, *tolerance.Pct*math.Abs(sourceW)/100)
		}
		return ChainNormalRange{LowW: insight.LossW - half, HighW: insight.LossW + half, Manual: true}
	}
	half := math.Max(normalDeviationFactor*math.Abs(insight.DeviationW),
		math.Max(normalLossFactor*math.Abs(insight.LossW), normalSourcePercent*math.Abs(sourceW)/100))
	return ChainNormalRange{LowW: insight.LossW - half, HighW: insight.LossW + half}
}

const (
	FindingDrift         = "drift"
	FindingFrozen        = "frozen"
	FindingContradiction = "contradiction"
	FindingSign          = "sign"
	FindingUnit          = "unit"
)

// ChainSuggestion is a fix the learned model proposes for a finding.
type ChainSuggestion struct {
	EntityID    string  `json:"entity_id"`
	Invert      bool    `json:"invert,omitempty"`
	ScaleFactor float64 `json:"scale_factor,omitempty"`
}

// ChainFinding is a problem the learned model found in a chain. From and To
// name the entities of the affected segment, for frozen only From, the frozen
// point.
type ChainFinding struct {
	Kind       string           `json:"kind"`
	ChainID    string           `json:"chain_id"`
	ChainLabel string           `json:"chain_label"`
	Since      time.Time        `json:"since"`
	From       string           `json:"from"`
	To         string           `json:"to,omitempty"`
	Suggestion *ChainSuggestion `json:"suggestion,omitempty"`
}

// ChainCurvePoint is the efficiency of one power class of a segment.
type ChainCurvePoint struct {
	PowerW     float64 `json:"power_w"`
	Efficiency float64 `json:"efficiency"`
}

// ChainPowerRange is a span of power in W.
type ChainPowerRange struct {
	FromW float64 `json:"from_w"`
	ToW   float64 `json:"to_w"`
}

// ChainCurve is the efficiency curve of a chain, per direction, with the most
// efficient span of each direction.
type ChainCurve struct {
	Down     []ChainCurvePoint `json:"down"`
	Up       []ChainCurvePoint `json:"up"`
	BestDown *ChainPowerRange  `json:"best_down,omitempty"`
	BestUp   *ChainPowerRange  `json:"best_up,omitempty"`
}

// ChainInsight is the learned model of the measurement chains. The energy
// package reads it, the model itself lives outside this package and is
// attached with Resolver.SetChainInsight.
type ChainInsight interface {
	Segment(source, target string, countingW float64) ChainSegmentInsight
	PairLearned(a, b string) bool
	Findings(chainID string) []ChainFinding
	Curve(points []string) ChainCurve
}

// SetChainInsight attaches the learned model. It survives SetOverrides.
func (r *Resolver) SetChainInsight(insight ChainInsight) {
	r.mu.Lock()
	r.insight = insight
	r.mu.Unlock()
}

// ChainInsight returns the attached learned model, nil when there is none.
func (r *Resolver) ChainInsight() ChainInsight {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.insight
}
