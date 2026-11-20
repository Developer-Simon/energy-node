package autolearn

import (
	"math"
	"sort"
)

// Building blocks for the detectors of the automation learning. They are
// deliberately small and robust: medians instead of means, circular statistics
// for times of day, and bounded inputs so a few outliers cannot dominate.

const (
	minutesPerDay = 1440.0
	circBinMin    = 5.0    // width of one density bin in minutes
	circBins      = 288    // 1440 / circBinMin
	circSigmaMin  = 10.0   // kernel width in minutes
	dayHalfLife   = 21.0   // days after which a weight has halved
	wilsonZ       = 1.64   // z value of the one-sided lower bound
	cusumK        = 0.5    // CUSUM slack
	cusumH        = 5.0    // CUSUM decision threshold
	madToSigma    = 1.4826 // consistency factor of the MAD for normal data
)

// Median returns the median of xs, or 0 for an empty slice. xs is not modified.
func Median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	mid := len(s) / 2
	if len(s)%2 == 1 {
		return s[mid]
	}
	return (s[mid-1] + s[mid]) / 2
}

// MADScale returns 1.4826 times the median absolute deviation of xs, a robust
// estimate of the standard deviation for normal data.
func MADScale(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	m := Median(xs)
	dev := make([]float64, len(xs))
	for i, x := range xs {
		dev[i] = math.Abs(x - m)
	}
	return madToSigma * Median(dev)
}

// WilsonLower returns the lower bound of the Wilson score interval for hits
// successes out of n trials, with z = 1.64. It returns 0 when n is 0.
func WilsonLower(hits, n int) float64 {
	if n <= 0 {
		return 0
	}
	nf := float64(n)
	p := float64(hits) / nf
	z2 := wilsonZ * wilsonZ
	center := p + z2/(2*nf)
	spread := wilsonZ * math.Sqrt(p*(1-p)/nf+z2/(4*nf*nf))
	return (center - spread) / (1 + z2/nf)
}

// DayWeight returns the weight of an observation that is ageDays old, 0.5 to
// the power of age/21, so that observations lose half their weight every
// three weeks.
func DayWeight(ageDays float64) float64 {
	return math.Pow(0.5, ageDays/dayHalfLife)
}

// NEff returns the effective sample size (sum of w)^2 / sum of w^2 of the
// weights, or 0 for an empty slice.
func NEff(weights []float64) float64 {
	var sum, sumSq float64
	for _, w := range weights {
		sum += w
		sumSq += w * w
	}
	if sumSq == 0 {
		return 0
	}
	return sum * sum / sumSq
}

// CircDensity is a kernel density of times of day over 288 bins of 5 minutes.
// Bin j stands for the minute 5*j.
type CircDensity [circBins]float64

// wrapMinutes maps a time of day to [0, 1440).
func wrapMinutes(m float64) float64 {
	r := math.Mod(m, minutesPerDay)
	if r < 0 {
		r += minutesPerDay
	}
	if r > minutesPerDay-1e-9 {
		r = 0
	}
	return r
}

// circDelta returns the signed difference a-b wrapped to (-720, 720] minutes.
func circDelta(a, b float64) float64 {
	d := wrapMinutes(a - b)
	if d > minutesPerDay/2 {
		d -= minutesPerDay
	}
	return d
}

// CircKDE estimates the density of the times of day in minutes with a von
// Mises kernel of width 10 minutes (kappa = 1/sigma^2, sigma = 2*pi*10/1440).
// Each event contributes its weight, the kernel is exp(kappa*(cos d - 1)) so
// its peak value is 1. A nil or mismatching weights slice counts every event
// with weight 1.
func CircKDE(minutes []float64, weights []float64) CircDensity {
	var d CircDensity
	if len(weights) != len(minutes) {
		weights = make([]float64, len(minutes))
		for i := range weights {
			weights[i] = 1
		}
	}
	sigma := 2 * math.Pi * circSigmaMin / minutesPerDay
	kappa := 1 / (sigma * sigma)
	for j := range d {
		center := float64(j) * circBinMin
		for i, m := range minutes {
			angle := 2 * math.Pi * circDelta(center, m) / minutesPerDay
			d[j] += weights[i] * math.Exp(kappa*(math.Cos(angle)-1))
		}
	}
	return d
}

// Peaks returns the times in minutes of the local maxima of the density whose
// value is at least minShare of the largest value. A density without any mass
// has no peaks.
func (d CircDensity) Peaks(minShare float64) []float64 {
	maxV := 0.0
	for _, v := range d {
		if v > maxV {
			maxV = v
		}
	}
	if maxV <= 0 {
		return nil
	}
	var peaks []float64
	for j := 0; j < circBins; j++ {
		prev := d[(j+circBins-1)%circBins]
		next := d[(j+1)%circBins]
		if d[j] >= minShare*maxV && d[j] > prev && d[j] >= next {
			peaks = append(peaks, float64(j)*circBinMin)
		}
	}
	return peaks
}

// CircMedian returns the circular median of times of day in minutes, in
// [0, 1440). It takes the circular mean as the centre and the median of the
// signed deviations from it, so that times around midnight do not split.
func CircMedian(minutes []float64) float64 {
	if len(minutes) == 0 {
		return 0
	}
	var c, s float64
	for _, m := range minutes {
		a := 2 * math.Pi * m / minutesPerDay
		c += math.Cos(a)
		s += math.Sin(a)
	}
	center := wrapMinutes(math.Atan2(s, c) * minutesPerDay / (2 * math.Pi))
	dev := make([]float64, len(minutes))
	for i, m := range minutes {
		dev[i] = circDelta(m, center)
	}
	return wrapMinutes(center + Median(dev))
}

// CircSpread returns 1.4826 times the median circular distance of the times in
// minutes from center. It is the robust counterpart of a standard deviation on
// the clock.
func CircSpread(minutes []float64, center float64) float64 {
	if len(minutes) == 0 {
		return 0
	}
	dist := make([]float64, len(minutes))
	for i, m := range minutes {
		dist[i] = math.Abs(circDelta(m, center))
	}
	return madToSigma * Median(dist)
}

// CUSUM looks for an upward shift of the mean in the sequence r. The baseline
// is the median of r, the slack is 0.5 and the threshold 5. changeAt is the
// index where the shift starts. ok is false when there is no alarm, or when the
// values from changeAt on do not stay above the baseline plus the slack (so a
// single outlier does not count as a change).
func CUSUM(r []float64) (changeAt int, ok bool) {
	if len(r) < 4 {
		return 0, false
	}
	base := Median(r)
	s := 0.0
	start := 0
	for t, x := range r {
		prev := s
		s = math.Max(0, s+x-base-cusumK)
		if prev == 0 && s > 0 {
			start = t
		}
		if s > cusumH {
			tail := append([]float64(nil), r[start:]...)
			if len(tail) >= 3 && Median(tail) > base+cusumK {
				return start, true
			}
			return 0, false
		}
	}
	return 0, false
}

// TheilSen returns the Theil-Sen estimate of slope and intercept: the median of
// all pairwise slopes and the median of y - slope*x. Pairs with equal x are
// skipped. Fewer than two points give 0, 0.
func TheilSen(xs, ys []float64) (slope, intercept float64) {
	n := len(xs)
	if len(ys) < n {
		n = len(ys)
	}
	var slopes []float64
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if xs[j] == xs[i] {
				continue
			}
			slopes = append(slopes, (ys[j]-ys[i])/(xs[j]-xs[i]))
		}
	}
	if len(slopes) == 0 {
		return 0, 0
	}
	slope = Median(slopes)
	res := make([]float64, n)
	for i := 0; i < n; i++ {
		res[i] = ys[i] - slope*xs[i]
	}
	return slope, Median(res)
}

// ShiftNull returns the counts of count(shiftDays) for shifts of 1 to 7 days.
// Index 0 holds the count for 1 day, index 6 for 7 days. It is the reference
// against which a real pattern is compared with its shifted copies.
func ShiftNull(count func(shiftDays int) int) [7]int {
	var out [7]int
	for i := range out {
		out[i] = count(i + 1)
	}
	return out
}

// OtsuSplit finds the cut that best separates xs into two groups, using Otsu's
// method on a histogram with the given number of bins. ok is false when the
// values are constant, when bins is below 2, or when either side has fewer than
// four values. The cut is the boundary between the two groups: values below it
// belong to the lower group.
func OtsuSplit(xs []float64, bins int) (cut float64, ok bool) {
	if len(xs) < 8 || bins < 2 {
		return 0, false
	}
	lo, hi := xs[0], xs[0]
	for _, x := range xs {
		lo = math.Min(lo, x)
		hi = math.Max(hi, x)
	}
	if hi == lo {
		return 0, false
	}
	width := (hi - lo) / float64(bins)
	hist := make([]float64, bins)
	sums := make([]float64, bins)
	for _, x := range xs {
		b := int((x - lo) / width)
		if b >= bins {
			b = bins - 1
		}
		hist[b]++
		sums[b] += x
	}
	total := float64(len(xs))
	var totalSum float64
	for _, x := range xs {
		totalSum += x
	}
	bestT, bestVar := -1, -1.0
	var w0, s0 float64
	for t := 1; t < bins; t++ {
		w0 += hist[t-1]
		s0 += sums[t-1]
		w1 := total - w0
		if w0 == 0 || w1 == 0 {
			continue
		}
		m0 := s0 / w0
		m1 := (totalSum - s0) / w1
		between := w0 * w1 * (m0 - m1) * (m0 - m1)
		if between > bestVar {
			bestVar = between
			bestT = t
		}
	}
	if bestT < 0 {
		return 0, false
	}
	cut = lo + float64(bestT)*width
	left := 0
	for _, x := range xs {
		if x < cut {
			left++
		}
	}
	if left < 4 || len(xs)-left < 4 {
		return 0, false
	}
	return cut, true
}
