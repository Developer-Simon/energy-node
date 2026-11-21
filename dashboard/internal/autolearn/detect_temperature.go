package autolearn

import "math"

// Knee fit of daily energy over the daily mean outdoor temperature. A heating
// load rises below a knee temperature, a cooling load rises above it. The fit
// is a hinge: y = a + b*max(0, knee-T) for heating and y = a + b*max(0, T-knee)
// for cooling. The knee is searched on a 1 degree grid and every candidate is
// fitted robustly with Theil-Sen.

const (
	kneeMinC       = 8.0
	kneeMaxC       = 20.0
	kneeMinPoints  = 6
	kneeMinExplain = 0.5
)

// FitKnee fits a hinge of y over temp and returns the knee temperature, the
// intercept a, the slope b, the share of the spread of y that the fit explains
// (clamped to [0, 1]) and whether the fit is trustworthy. ok requires an
// explained share of at least 0.5 and a slope with the same sign on both halves
// of the days (split by index). Pairs with NaN or infinite values are dropped.
// Fewer than six remaining pairs, or a length mismatch, give ok false.
func FitKnee(temp, y []float64, cooling bool) (knee, a, b float64, explained float64, ok bool) {
	n := len(temp)
	if len(y) != n {
		return 0, 0, 0, 0, false
	}
	var t, v []float64
	for i := 0; i < n; i++ {
		if isFinite(temp[i]) && isFinite(y[i]) {
			t = append(t, temp[i])
			v = append(v, y[i])
		}
	}
	if len(v) < kneeMinPoints {
		return 0, 0, 0, 0, false
	}

	bestMAE := math.Inf(1)
	for k := kneeMinC; k <= kneeMaxC; k++ {
		x := hingeX(t, k, cooling)
		slope, icept := TheilSen(x, v)
		mae := 0.0
		for i := range x {
			mae += math.Abs(v[i] - (icept + slope*x[i]))
		}
		mae /= float64(len(x))
		if mae < bestMAE {
			bestMAE = mae
			knee, a, b = k, icept, slope
		}
	}

	explained = explainedShare(v, bestMAE)

	x := hingeX(t, knee, cooling)
	half := len(v) / 2
	bFirst, _ := TheilSen(x[:half], v[:half])
	bSecond, _ := TheilSen(x[half:], v[half:])
	sameSign := b != 0 && bFirst != 0 && bSecond != 0 &&
		math.Signbit(bFirst) == math.Signbit(b) && math.Signbit(bSecond) == math.Signbit(b)

	ok = explained >= kneeMinExplain && sameSign
	return knee, a, b, explained, ok
}

// hingeX maps temperatures to the hinge regressor for the given knee.
func hingeX(temp []float64, knee float64, cooling bool) []float64 {
	x := make([]float64, len(temp))
	for i, T := range temp {
		if cooling {
			x[i] = math.Max(0, T-knee)
		} else {
			x[i] = math.Max(0, knee-T)
		}
	}
	return x
}

// explainedShare returns 1 minus the mean absolute error over the median
// absolute deviation of y, clamped to [0, 1]. Without spread it is 0.
func explainedShare(y []float64, mae float64) float64 {
	med := Median(y)
	dev := make([]float64, len(y))
	for i, v := range y {
		dev[i] = math.Abs(v - med)
	}
	spread := Median(dev)
	if spread == 0 {
		return 0
	}
	share := 1 - mae/spread
	return math.Max(0, math.Min(1, share))
}

func isFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}
