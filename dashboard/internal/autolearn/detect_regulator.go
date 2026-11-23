package autolearn

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// RegulatorDetector rates the tuning of a zero feed-in regulator from its own
// steps (spec 15.6 "Regelgüte Nulleinspeisung"). The dead time d is the shift
// of the largest correlation between output and net, 0 to 5 steps. K is the
// median of the net change per output change, and gain_grenze is
// d^d / ((d+1)^(d+1) * K). A gain above 1.25 times 0.8 * gain_grenze swings.
// The deadband and the largest change per step are rated against the noise of
// the net, one output stage times K, and the largest usual load jump settled
// in three steps. Each PV band below and above 40 % of the best curve is rated
// on its own. Without a PV model, minutes or steps the detector finds nothing.
var RegulatorDetector = Detector{
	Name:      "regulator_quality",
	Group:     "reliability",
	Light:     false,
	PerDevice: false,
	Run:       regulatorRun,
}

const (
	// regulatorWindow is the span of steps the rating reads.
	regulatorWindow = 14 * 24 * time.Hour
	// regulatorMinChanges is the number of steps with an output change needed.
	regulatorMinChanges = 200
	// regulatorMinBand is the number of changes a PV band needs to be rated alone.
	regulatorMinBand = 30
	// regulatorMaxDead is the largest dead time searched, in steps.
	regulatorMaxDead = 5
	// regulatorBandShare is the share of the best curve that splits the PV bands.
	regulatorBandShare = 0.4
	// regulatorKSpread is the largest relative spread of K that counts as safe.
	regulatorKSpread = 0.25
	// regulatorKDiff is the relative difference of K between the bands that
	// makes the cautious value count.
	regulatorKDiff = 0.3
	// regulatorSwingShare is the factor above the target gain that counts as swinging.
	regulatorSwingShare = 1.25
	// regulatorDeadFactor is the factor by which the deadband or the change
	// limit may differ from the target before it is reported.
	regulatorDeadFactor = 2.0
	// regulatorGainTarget is the share of gain_grenze the target gain takes.
	regulatorGainTarget = 0.8
	// regulatorGainMin and regulatorGainMax bound the gain (schema).
	regulatorGainMin = 0.1
	regulatorGainMax = 1.0
	// regulatorJumpSteps is the number of steps a usual load jump may take to settle.
	regulatorJumpSteps = 3.0
	// regulatorDefaultGain is the gain of a regulator without a gain field.
	regulatorDefaultGain = 0.8
)

// regulatorResult is the rating of one regulator. Measured is set when the
// regulator works on a measured base: only the dead time is rated then.
type regulatorResult struct {
	Measured     bool
	Dead         int
	K            float64
	Spread       float64
	Changes      int
	GainSoll     float64
	DeadbandSoll float64
	// MaxChangeSoll is 0 when no usual load jump was seen.
	MaxChangeSoll float64
	SettleS       float64
	FlipsH        float64
	Days          float64
}

// regulatorPair is one output change with the net change d steps later.
type regulatorPair struct {
	i int     // index of the output change
	x float64 // output change in W
	k float64 // net change over output change, signed
}

// regulatorRun rates the regulators of the enabled scenarios.
func regulatorRun(ctx context.Context, in *Input, _ string) []Candidate {
	if ctx.Err() != nil || in == nil || in.Steps == nil || in.PV == nil || in.Minutes == nil || in.Doc.Location == nil {
		return nil
	}
	var out []Candidate
	for _, sc := range in.Doc.Scenarios {
		if ctx.Err() != nil {
			return nil
		}
		if !sc.Enabled || sc.Regulator == nil {
			continue
		}
		steps := in.Steps(sc.ID, in.Now.Add(-regulatorWindow), in.Now)
		res, ok := regulatorEval(in, sc, steps)
		if !ok || res.Measured {
			continue
		}
		if cand, ok := regulatorCandidate(sc, res); ok {
			out = append(out, cand)
		}
	}
	return out
}

// regulatorEval rates the steps of one scenario. It returns false when the
// steps are too few, the dead time or K cannot be found, or K scatters.
func regulatorEval(in *Input, sc Scenario, steps []Step) (regulatorResult, bool) {
	if sc.Regulator == nil || in.Doc.Location == nil || in.PV == nil || in.Minutes == nil {
		return regulatorResult{}, false
	}
	model, raster, ok := in.PV()
	if !ok {
		return regulatorResult{}, false
	}
	pts := make([]Step, 0, len(steps))
	for _, st := range steps {
		if st.To != nil {
			pts = append(pts, st)
		}
	}
	sort.SliceStable(pts, func(a, b int) bool { return pts[a].At < pts[b].At })
	n := len(pts)
	if n < 3 {
		return regulatorResult{}, false
	}
	x, y, valid := regulatorDeltas(pts)
	changes := 0
	for i := 1; i < n; i++ {
		if valid[i] && x[i] != 0 {
			changes++
		}
	}
	if changes < regulatorMinChanges {
		return regulatorResult{}, false
	}
	d, ok := regulatorDeadTime(x, y, valid)
	if !ok {
		return regulatorResult{}, false
	}
	res := regulatorResult{Dead: d, Changes: changes, Measured: sc.Regulator.BaseSource != "last"}
	res.Days = float64(pts[n-1].At-pts[0].At) / 86400
	res.FlipsH = regulatorFlips(x, valid) / math.Max(float64(pts[n-1].At-pts[0].At)/3600, 1e-9)
	res.SettleS = float64(d+3) * regulatorMedianGap(pts)
	if res.Measured {
		return res, true
	}

	pairs := regulatorPairs(x, y, valid, d)
	ks := make([]float64, 0, len(pairs))
	for _, p := range pairs {
		ks = append(ks, math.Abs(p.k))
	}
	if len(ks) == 0 {
		return regulatorResult{}, false
	}
	k := Median(ks)
	if k <= 0 {
		return regulatorResult{}, false
	}
	res.Spread = MADScale(ks) / k
	if res.Spread >= regulatorKSpread {
		return regulatorResult{}, false
	}

	bands := regulatorBands(in, pts, pairs, model, raster)
	var lo, hi []regulatorPair
	for j, p := range pairs {
		switch bands[j] {
		case 0:
			lo = append(lo, p)
		case 1:
			hi = append(hi, p)
		}
	}
	if len(lo) >= regulatorMinBand && len(hi) >= regulatorMinBand {
		kLo, kHi := regulatorMedianK(lo), regulatorMedianK(hi)
		if math.Abs(kHi-kLo)/math.Max(kLo, kHi) > regulatorKDiff {
			res = regulatorCautious(res, x, y, valid, d, lo, hi)
			res.K = k
			return res, true
		}
	}
	gain, dead, maxChange, _ := regulatorBandParams(x, y, valid, pairs, k, d)
	res.K = k
	res.GainSoll = gain
	res.DeadbandSoll = dead
	res.MaxChangeSoll = maxChange
	return res, true
}

// regulatorBandParams returns the targets of the pairs with K as given.
func regulatorBandParams(x, y []float64, valid []bool, pairs []regulatorPair, k float64, d int) (gain, dead, maxChange, stage float64) {
	noise, jump := regulatorNoise(x, y, valid)
	stage = regulatorStage(pairs)
	limit := math.Pow(float64(d), float64(d)) / (math.Pow(float64(d+1), float64(d+1)) * k)
	gain = math.Min(regulatorGainMax, math.Max(regulatorGainMin, regulatorGainTarget*limit))
	dead = math.Max(2*noise, k*stage)
	if jump > 0 {
		maxChange = math.Max(jump/(regulatorJumpSteps*k), stage)
	}
	return gain, dead, maxChange, stage
}

// regulatorCautious rates both PV bands with their own K and keeps the more
// cautious value of each parameter: the smaller gain and change limit, the
// larger deadband.
func regulatorCautious(res regulatorResult, x, y []float64, valid []bool, d int, lo, hi []regulatorPair) regulatorResult {
	gLo, dLo, mLo, _ := regulatorBandParams(x, y, valid, lo, regulatorMedianK(lo), d)
	gHi, dHi, mHi, _ := regulatorBandParams(x, y, valid, hi, regulatorMedianK(hi), d)
	res.GainSoll = math.Min(gLo, gHi)
	res.DeadbandSoll = math.Max(dLo, dHi)
	switch {
	case mLo > 0 && mHi > 0:
		res.MaxChangeSoll = math.Min(mLo, mHi)
	case mLo > 0:
		res.MaxChangeSoll = mLo
	default:
		res.MaxChangeSoll = mHi
	}
	return res
}

// regulatorDeltas returns the output change and the net change of every step
// against the step before. A pair with a gap above the step gap cap is not valid.
func regulatorDeltas(pts []Step) (x, y []float64, valid []bool) {
	n := len(pts)
	x = make([]float64, n)
	y = make([]float64, n)
	valid = make([]bool, n)
	for i := 1; i < n; i++ {
		if pts[i].At-pts[i-1].At > stepGapCap {
			continue
		}
		x[i] = float64(*pts[i].To - *pts[i-1].To)
		y[i] = float64(pts[i].Net - pts[i-1].Net)
		valid[i] = true
	}
	return x, y, valid
}

// regulatorDeadTime returns the shift from 0 to regulatorMaxDead steps with
// the largest correlation between the output change and the later net change.
func regulatorDeadTime(x, y []float64, valid []bool) (int, bool) {
	n := len(x)
	best, bestR, found := 0, math.Inf(-1), false
	for lag := 0; lag <= regulatorMaxDead; lag++ {
		var xs, ys []float64
		for i := 1; i+lag < n; i++ {
			if valid[i] && valid[i+lag] {
				xs = append(xs, x[i])
				ys = append(ys, y[i+lag])
			}
		}
		r, ok := regulatorCorrelation(xs, ys)
		if ok && r > bestR {
			best, bestR, found = lag, r, true
		}
	}
	return best, found
}

// regulatorCorrelation returns the Pearson correlation, false without variance.
func regulatorCorrelation(xs, ys []float64) (float64, bool) {
	n := float64(len(xs))
	if n < 2 {
		return 0, false
	}
	var sx, sy float64
	for i := range xs {
		sx += xs[i]
		sy += ys[i]
	}
	mx, my := sx/n, sy/n
	var cxy, cxx, cyy float64
	for i := range xs {
		dx, dy := xs[i]-mx, ys[i]-my
		cxy += dx * dy
		cxx += dx * dx
		cyy += dy * dy
	}
	if cxx == 0 || cyy == 0 {
		return 0, false
	}
	return cxy / math.Sqrt(cxx*cyy), true
}

// regulatorPairs returns the output changes whose net change d steps later is valid.
func regulatorPairs(x, y []float64, valid []bool, d int) []regulatorPair {
	var out []regulatorPair
	n := len(x)
	for i := 1; i < n; i++ {
		j := i + d
		if !valid[i] || x[i] == 0 || j >= n || !valid[j] {
			continue
		}
		out = append(out, regulatorPair{i: i, x: x[i], k: y[j] / x[i]})
	}
	return out
}

// regulatorBands returns per pair 0 for the lower PV band, 1 for the upper
// band and -1 when the PV or the best curve is unknown.
func regulatorBands(in *Input, pts []Step, pairs []regulatorPair, model PVModel, raster *Raster) []int {
	bands := make([]int, len(pairs))
	for j := range bands {
		bands[j] = -1
	}
	if len(pts) == 0 {
		return bands
	}
	site := in.Doc.Location
	from := time.Unix(pts[0].At, 0).UTC().Truncate(time.Minute)
	to := time.Unix(pts[len(pts)-1].At, 0).UTC().Add(time.Minute)
	pv := in.Minutes.Range("b:pv", from, to)
	for j, p := range pairs {
		at := pts[p.i].At
		m := int((at - pv.From.Unix()) / 60)
		if m < 0 || m >= len(pv.Values) {
			continue
		}
		val := float64(pv.Values[m])
		if math.IsNaN(val) {
			continue
		}
		az, el := sun.Position(time.Unix(at, 0).UTC(), site.Lat, site.Lon)
		best := model.Best(az, el, raster)
		if best <= 0 {
			continue
		}
		if val/best < regulatorBandShare {
			bands[j] = 0
		} else {
			bands[j] = 1
		}
	}
	return bands
}

// regulatorNoise returns the noise of the net and the largest usual load
// jump: both are the 95th percentile of the net change without an output change,
// the noise halved.
func regulatorNoise(x, y []float64, valid []bool) (noise, jump float64) {
	var diffs []float64
	for i := 1; i < len(x); i++ {
		if valid[i] && x[i] == 0 {
			diffs = append(diffs, math.Abs(y[i]))
		}
	}
	jump = pvQuantile(diffs, 0.95)
	return jump / 2, jump
}

// regulatorStage returns the smallest output change in W of the pairs.
func regulatorStage(pairs []regulatorPair) float64 {
	stage := math.Inf(1)
	for _, p := range pairs {
		stage = math.Min(stage, math.Abs(p.x))
	}
	if math.IsInf(stage, 1) {
		return 0
	}
	return stage
}

// regulatorMedianK returns the median of the absolute K of the pairs.
func regulatorMedianK(pairs []regulatorPair) float64 {
	ks := make([]float64, 0, len(pairs))
	for _, p := range pairs {
		ks = append(ks, math.Abs(p.k))
	}
	return Median(ks)
}

// regulatorFlips counts the sign changes of the output between valid changes.
func regulatorFlips(x []float64, valid []bool) float64 {
	flips := 0.0
	prev := 0.0
	for i := 1; i < len(x); i++ {
		if !valid[i] || x[i] == 0 {
			continue
		}
		if prev != 0 && (x[i] > 0) != (prev > 0) {
			flips++
		}
		prev = x[i]
	}
	return flips
}

// regulatorMedianGap returns the median gap between the steps in seconds.
func regulatorMedianGap(pts []Step) float64 {
	gaps := make([]float64, 0, len(pts))
	for i := 1; i < len(pts); i++ {
		if g := pts[i].At - pts[i-1].At; g > 0 {
			gaps = append(gaps, float64(g))
		}
	}
	return Median(gaps)
}

// regulatorCandidate builds the finding of a regulator whose gain swings or
// whose deadband or change limit differs from the target by more than the factor.
func regulatorCandidate(sc Scenario, res regulatorResult) (Candidate, bool) {
	r := sc.Regulator
	gain := r.Gain
	if gain == 0 {
		gain = regulatorDefaultGain
	}
	gainIst, gainSoll := gain, roundTo(res.GainSoll, 2)
	var ops []Op
	var bars []ScaleBar
	if gain > regulatorSwingShare*res.GainSoll {
		ops = append(ops, Op{Path: []any{"while", "regulator", "gain"}, Value: gainSoll})
		bars = append(bars, ScaleBar{Name: "gain", Ist: gainIst, Soll: gainSoll})
	}
	if r.DeadbandW > regulatorDeadFactor*res.DeadbandSoll || r.DeadbandW < res.DeadbandSoll/regulatorDeadFactor {
		soll := math.Round(res.DeadbandSoll)
		ops = append(ops, Op{Path: []any{"while", "regulator", "deadband_w"}, Value: soll})
		bars = append(bars, ScaleBar{Name: "deadband_w", Ist: r.DeadbandW, Soll: soll, Unit: "W"})
	}
	if res.MaxChangeSoll > 0 {
		off := r.MaxChangeW == nil || *r.MaxChangeW > regulatorDeadFactor*res.MaxChangeSoll || *r.MaxChangeW < res.MaxChangeSoll/regulatorDeadFactor
		if off {
			soll := math.Max(1, math.Round(res.MaxChangeSoll))
			ops = append(ops, Op{Path: []any{"while", "regulator", "max_change_w"}, Value: soll})
			// Ist 0 stands for no change limit.
			ist := 0.0
			if r.MaxChangeW != nil {
				ist = *r.MaxChangeW
			}
			bars = append(bars, ScaleBar{Name: "max_change_w", Ist: ist, Soll: soll, Unit: "W"})
		}
	}
	if len(ops) == 0 {
		return Candidate{}, false
	}
	name := sc.Name
	if name == "" {
		name = sc.ID
	}
	benefit := Benefit{}
	finding := Finding{
		ID:         FindingID("regulator", sc.ID),
		Kind:       "regulator",
		Group:      "check",
		RuleIDs:    []string{sc.ID},
		Severity:   "info",
		Confidence: "high",
		Summary: Text{Code: "regulator.quality", Params: map[string]any{
			"scenario": name, "gain": gainIst, "gain_soll": gainSoll}},
		Evidence: Evidence{Shape: "scale", IstValue: &gainIst, SollValue: &gainSoll, Scales: bars},
		Facts: []Fact{
			{Code: "k", Value: roundTo(res.K, 2)},
			{Code: "dead_steps", Value: res.Dead},
			{Code: "flips_h", Value: roundTo(res.FlipsH, 1)},
			{Code: "settle_s", Value: math.Round(res.SettleS)},
		},
		Apply: []Fix{{Code: "set_regulator", RuleID: sc.ID, Ops: ops}},
	}
	return Candidate{
		Finding:     finding,
		Detector:    "regulator_quality",
		NEff:        res.Days,
		MinNEff:     0,
		Lower:       1,
		Limit:       1,
		HalvesAgree: true,
		Benefit:     benefit,
		Values: map[string]float64{
			"gain": gainIst, "gain_soll": gainSoll, "k": res.K, "dead_steps": float64(res.Dead),
		},
	}, true
}

// roundTo rounds v to the given number of decimals.
func roundTo(v float64, decimals int) float64 {
	p := math.Pow(10, float64(decimals))
	return math.Round(v*p) / p
}
