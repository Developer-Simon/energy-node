package autolearn

import (
	"math"
	"sort"
)

const (
	// pvFitMinCells is how many filled cells a fit needs.
	pvFitMinCells = 20
	// pvEpsW keeps the log ratio finite for cells with zero power.
	pvEpsW = 1.0
	// pvMinModelRatio is the model value below which a cell gives no PMax ratio.
	pvMinModelRatio = 0.05
	// pvSafeSpanDeg is the sun azimuth range the filled cells must cover.
	pvSafeSpanDeg = 60.0
	// pvCapMargin lifts the highest cell value a little above the measured peak.
	pvCapMargin = 1.05
	// pvSplitAccept is the score ratio below which a split east/west fit replaces the single surface.
	pvSplitAccept = 0.7
	// pvDegRad converts degrees to radians.
	pvDegRad = math.Pi / 180
)

// Surface is one roof plane: its peak power and orientation.
type Surface struct {
	PMax       float64
	TiltDeg    float64
	AzimuthDeg float64
}

// PVModel is the fitted roof geometry for the clear-sky curve.
type PVModel struct {
	Surfaces []Surface
	CapW     float64
	Safe     bool
	FittedAt int64
}

// pvSky is one sky position with the values the fit needs precomputed.
type pvSky struct {
	az, el float64
	w      float64
	azRad  float64
	sinEl  float64
	cosEl  float64
	pass   float64 // Durchlass(el) = 0.7^(1/sin(el)^0.678)
}

// newPVSky precomputes the sky geometry of one position.
func newPVSky(az, el, w float64) pvSky {
	s := math.Sin(el * pvDegRad)
	return pvSky{
		az:    az,
		el:    el,
		w:     w,
		azRad: az * pvDegRad,
		sinEl: s,
		cosEl: math.Cos(el * pvDegRad),
		pass:  math.Pow(0.7, 1/math.Pow(s, 0.678)),
	}
}

// pvGeom returns the model without PMax for a surface with the given tilt
// and azimuth in degrees: max(0, cos(incidence)) times the passage.
func pvGeom(az, el, tiltDeg, surfAzDeg float64) float64 {
	t := tiltDeg * pvDegRad
	a := surfAzDeg * pvDegRad
	s := newPVSky(az, el, 0)
	return s.geom(math.Sin(t), math.Cos(t), a)
}

// geom is pvGeom with the surface trigonometry already computed.
func (s pvSky) geom(sinT, cosT, surfAzRad float64) float64 {
	c := s.sinEl*cosT + s.cosEl*sinT*math.Cos(s.azRad-surfAzRad)
	if c <= 0 {
		return 0
	}
	return c * s.pass
}

// pvScoreQuantile is the quantile of the absolute log ratios used as the fit
// score. The 95th percentile instead of the median keeps cells where one roof
// plane is in shadow from being ignored.
const pvScoreQuantile = 0.95

// pvQuantile sorts xs in place and returns the value at quantile q (0..1),
// using the nearest rank, 0 for an empty slice.
func pvQuantile(xs []float64, q float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sort.Float64s(xs)
	idx := int(math.Ceil(q*float64(len(xs)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(xs) {
		idx = len(xs) - 1
	}
	return xs[idx]
}

// pvMedian sorts xs in place and returns its median, 0 for an empty slice.
func pvMedian(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sort.Float64s(xs)
	n := len(xs)
	if n%2 == 1 {
		return xs[n/2]
	}
	return (xs[n/2-1] + xs[n/2]) / 2
}

// pvSkies returns every filled cell of the raster as a sky position.
func pvSkies(r *Raster) []pvSky {
	var out []pvSky
	r.Each(func(az, el, w float64) {
		out = append(out, newPVSky(az, el, w))
	})
	return out
}

// fitSurface grid-searches tilt and azimuth in step degrees. For each grid
// point PMax is the median of w/model over the cells with a usable model,
// and the score is the median absolute log ratio of PMax*model to w over all
// cells. ok is false when no grid point has a usable PMax.
func fitSurface(pts []pvSky, step float64) (Surface, float64, bool) {
	best := Surface{}
	bestScore := math.Inf(1)
	found := false
	model := make([]float64, len(pts))
	ratios := make([]float64, 0, len(pts))
	scores := make([]float64, 0, len(pts))
	for ti := 0; float64(ti)*step <= 90; ti++ {
		tilt := float64(ti) * step
		sinT, cosT := math.Sincos(tilt * pvDegRad)
		for ai := 0; float64(ai)*step < 360; ai++ {
			azDeg := float64(ai) * step
			aRad := azDeg * pvDegRad
			ratios = ratios[:0]
			for i, p := range pts {
				model[i] = p.geom(sinT, cosT, aRad)
				if model[i] > pvMinModelRatio && p.w > 0 {
					ratios = append(ratios, p.w/model[i])
				}
			}
			if len(ratios) == 0 {
				continue
			}
			pmax := pvMedian(ratios)
			scores = scores[:0]
			for i, p := range pts {
				scores = append(scores, math.Abs(math.Log((pmax*model[i]+pvEpsW)/(p.w+pvEpsW))))
			}
			score := pvQuantile(scores, pvScoreQuantile)
			if score < bestScore {
				bestScore = score
				best = Surface{PMax: pmax, TiltDeg: tilt, AzimuthDeg: azDeg}
				found = true
			}
		}
	}
	return best, bestScore, found
}

// overallScore is the median absolute log ratio of the summed model of all
// surfaces to w over all cells, the same score fitSurface uses.
func overallScore(pts []pvSky, surfaces []Surface) float64 {
	scores := make([]float64, 0, len(pts))
	for _, p := range pts {
		model := 0.0
		for _, s := range surfaces {
			model += surfaceValue(s, p)
		}
		scores = append(scores, math.Abs(math.Log((model+pvEpsW)/(p.w+pvEpsW))))
	}
	return pvQuantile(scores, pvScoreQuantile)
}

// surfaceValue is the model value of one surface at a sky position.
func surfaceValue(s Surface, p pvSky) float64 {
	sinT, cosT := math.Sincos(s.TiltDeg * pvDegRad)
	return s.PMax * p.geom(sinT, cosT, s.AzimuthDeg*pvDegRad)
}

// FitPV fits roof planes to the raster. A single surface is fitted to all
// cells. With maxSurfaces >= 2 a split fit is also tried: one surface on the
// east cells (az < 180) and one on the west cells (az >= 180). The split is
// accepted only when its overall score is below pvSplitAccept times the
// single-surface score and both PMax are positive. maxSurfaces >= 3 adds
// nothing beyond two surfaces. ok is false with fewer than 20 filled cells or
// when no surface can be fitted.
func FitPV(r *Raster, step float64, maxSurfaces int) (PVModel, bool) {
	if r == nil || step <= 0 {
		return PVModel{}, false
	}
	pts := pvSkies(r)
	if len(pts) < pvFitMinCells {
		return PVModel{}, false
	}
	first, singleScore, ok := fitSurface(pts, step)
	if !ok {
		return PVModel{}, false
	}
	surfaces := []Surface{first}

	if maxSurfaces >= 2 {
		var east, west []pvSky
		for _, p := range pts {
			if p.az < 180 {
				east = append(east, p)
			} else {
				west = append(west, p)
			}
		}
		if len(east) >= pvFitMinCells && len(west) >= pvFitMinCells {
			e, _, okE := fitSurface(east, step)
			w, _, okW := fitSurface(west, step)
			if okE && okW && e.PMax > 0 && w.PMax > 0 {
				split := []Surface{e, w}
				if overallScore(pts, split) < pvSplitAccept*singleScore {
					surfaces = split
				}
			}
		}
	}

	model := PVModel{Surfaces: surfaces}
	minAz, maxAz := math.Inf(1), math.Inf(-1)
	for _, p := range pts {
		if p.w > model.CapW {
			model.CapW = p.w
		}
		minAz = math.Min(minAz, p.az)
		maxAz = math.Max(maxAz, p.az)
	}
	model.CapW *= pvCapMargin
	model.Safe = maxAz-minAz >= pvSafeSpanDeg
	return model, true
}

// Best returns the expected PV power in W at az/el: the larger of the raster
// cell value and the modelled surfaces, capped at CapW. Below 3 degrees of
// sun elevation it returns 0.
func (m PVModel) Best(az, el float64, r *Raster) float64 {
	if el < pvRasterMinElDeg {
		return 0
	}
	cell := 0.0
	if r != nil {
		if w, ok := r.Cell(az, el); ok {
			cell = w
		}
	}
	p := newPVSky(az, el, 0)
	model := 0.0
	for _, s := range m.Surfaces {
		model += surfaceValue(s, p)
	}
	return math.Min(m.CapW, math.Max(cell, model))
}
