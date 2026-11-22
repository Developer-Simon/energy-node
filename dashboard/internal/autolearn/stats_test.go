package autolearn

import (
	"math"
	"testing"
)

// round2 rounds to two decimals, the precision the spec values are given in.
func round2(x float64) float64 {
	return math.Round(x*100) / 100
}

// circDiff returns the shortest distance in minutes between two times of day.
func circDiff(a, b float64) float64 {
	d := math.Mod(math.Abs(a-b), 1440)
	if d > 720 {
		d = 1440 - d
	}
	return d
}

func TestMedianAndMADScale(t *testing.T) {
	if got := Median(nil); got != 0 {
		t.Fatalf("Median(nil) = %v, want 0", got)
	}
	if got := Median([]float64{3, 1, 2}); got != 2 {
		t.Fatalf("Median odd = %v, want 2", got)
	}
	if got := Median([]float64{4, 1, 3, 2}); got != 2.5 {
		t.Fatalf("Median even = %v, want 2.5", got)
	}
	// Values 1,2,3,4,100: median 3, absolute deviations 2,1,0,1,97, MAD 1.
	got := MADScale([]float64{1, 2, 3, 4, 100})
	if math.Abs(got-1.4826) > 1e-9 {
		t.Fatalf("MADScale = %v, want 1.4826", got)
	}
}

func TestWilsonLower(t *testing.T) {
	cases := []struct {
		hits, n int
		want    float64
	}{
		{12, 14, 0.65},
		{6, 7, 0.55},
		{24, 28, 0.72},
	}
	for _, c := range cases {
		got := round2(WilsonLower(c.hits, c.n))
		if got != c.want {
			t.Errorf("WilsonLower(%d,%d) = %.4f, want %.2f", c.hits, c.n, WilsonLower(c.hits, c.n), c.want)
		}
	}
	if got := WilsonLower(0, 0); got != 0 {
		t.Errorf("WilsonLower(0,0) = %v, want 0", got)
	}
}

func TestDayWeight(t *testing.T) {
	if got := DayWeight(0); math.Abs(got-1) > 1e-12 {
		t.Errorf("DayWeight(0) = %v, want 1", got)
	}
	if got := DayWeight(21); math.Abs(got-0.5) > 1e-12 {
		t.Errorf("DayWeight(21) = %v, want 0.5", got)
	}
	if got := DayWeight(42); math.Abs(got-0.25) > 1e-12 {
		t.Errorf("DayWeight(42) = %v, want 0.25", got)
	}
}

func TestNEff(t *testing.T) {
	w := make([]float64, 14)
	for i := range w {
		w[i] = 1
	}
	if got := NEff(w); math.Abs(got-14) > 1e-9 {
		t.Fatalf("NEff(14 equal) = %v, want 14", got)
	}
	if got := NEff(nil); got != 0 {
		t.Fatalf("NEff(nil) = %v, want 0", got)
	}
}

func TestCircMedian(t *testing.T) {
	if got := CircMedian([]float64{1435, 5}); circDiff(got, 0) > 1e-6 {
		t.Fatalf("CircMedian([1435,5]) = %v, want 0 on the circle", got)
	}
	if got := CircMedian([]float64{420, 430, 440}); math.Abs(got-430) > 1e-6 {
		t.Fatalf("CircMedian([420,430,440]) = %v, want 430", got)
	}
	if got := CircMedian(nil); got != 0 {
		t.Fatalf("CircMedian(nil) = %v, want 0", got)
	}
}

func TestCircSpread(t *testing.T) {
	// Distances to 0: 5, 5, 10 -> median 5 -> spread 1.4826*5.
	got := CircSpread([]float64{5, 1435, 10}, 0)
	if math.Abs(got-1.4826*5) > 1e-9 {
		t.Fatalf("CircSpread = %v, want %v", got, 1.4826*5)
	}
	if got := CircSpread(nil, 0); got != 0 {
		t.Fatalf("CircSpread(nil) = %v, want 0", got)
	}
}

func TestCircKDEPeaks(t *testing.T) {
	var minutes []float64
	for m := 415.0; m <= 425; m++ {
		minutes = append(minutes, m) // around 07:00
	}
	for m := 1135.0; m <= 1145; m++ {
		minutes = append(minutes, m) // around 19:00
	}
	d := CircKDE(minutes, nil)
	peaks := d.Peaks(0.3)
	if len(peaks) != 2 {
		t.Fatalf("Peaks(0.3) = %v, want exactly 2 peaks", peaks)
	}
	if circDiff(peaks[0], 420) > 10 {
		t.Errorf("first peak %v not near 07:00", peaks[0])
	}
	if circDiff(peaks[1], 1140) > 10 {
		t.Errorf("second peak %v not near 19:00", peaks[1])
	}
}

func TestCircKDEWeightsAndEmpty(t *testing.T) {
	var empty CircDensity = CircKDE(nil, nil)
	if got := empty.Peaks(0.3); len(got) != 0 {
		t.Fatalf("Peaks on empty density = %v, want none", got)
	}
	// A heavy weight on 19:00 makes 07:00 about a third of the maximum, so a
	// share of 0.5 keeps only the 19:00 peak.
	d := CircKDE([]float64{420, 1140}, []float64{1, 3})
	peaks := d.Peaks(0.5)
	if len(peaks) != 1 || circDiff(peaks[0], 1140) > 10 {
		t.Fatalf("weighted peaks at share 0.5 = %v, want only 19:00", peaks)
	}
}

func TestCUSUMDetectsStep(t *testing.T) {
	r := make([]float64, 0, 30)
	for i := 0; i < 20; i++ {
		r = append(r, 0)
	}
	for i := 0; i < 10; i++ {
		r = append(r, 2.0)
	}
	changeAt, ok := CUSUM(r)
	if !ok {
		t.Fatalf("CUSUM on step: ok = false, want true")
	}
	if changeAt < 20 || changeAt > 23 {
		t.Fatalf("CUSUM changeAt = %d, want 20 to 23", changeAt)
	}
}

func TestCUSUMIgnoresSingleOutlier(t *testing.T) {
	r := make([]float64, 30)
	r[10] = 6.0
	if _, ok := CUSUM(r); ok {
		t.Fatalf("CUSUM on single outlier: ok = true, want false")
	}
}

func TestTheilSenRobustSlope(t *testing.T) {
	var xs, ys []float64
	for x := 0.0; x < 10; x++ {
		xs = append(xs, x)
		ys = append(ys, 2*x+1)
	}
	ys[9] = 100 // outlier
	slope, intercept := TheilSen(xs, ys)
	if math.Abs(slope-2) > 1e-9 {
		t.Fatalf("TheilSen slope = %v, want 2", slope)
	}
	if math.Abs(intercept-1) > 1e-9 {
		t.Fatalf("TheilSen intercept = %v, want 1", intercept)
	}
}

func TestShiftNull(t *testing.T) {
	got := ShiftNull(func(shift int) int { return shift * 10 })
	want := [7]int{10, 20, 30, 40, 50, 60, 70}
	if got != want {
		t.Fatalf("ShiftNull = %v, want %v", got, want)
	}
}

func TestOtsuSplitTwoClusters(t *testing.T) {
	var xs []float64
	for i := 0; i < 10; i++ {
		xs = append(xs, 4.8+float64(i)*0.07) // about 5 s
	}
	for i := 0; i < 10; i++ {
		xs = append(xs, 19+float64(i)*0.2) // about 20 s
	}
	cut, ok := OtsuSplit(xs, 20)
	if !ok {
		t.Fatalf("OtsuSplit: ok = false, want true")
	}
	if cut <= 5.5 || cut >= 19 {
		t.Fatalf("OtsuSplit cut = %v, want between the clusters", cut)
	}
}

func TestOtsuSplitNeedsFourPerSide(t *testing.T) {
	xs := []float64{1, 1, 1, 20, 20, 20, 20, 20, 20, 20, 20, 20}
	if _, ok := OtsuSplit(xs, 20); ok {
		t.Fatalf("OtsuSplit with 3 values on one side: ok = true, want false")
	}
}

func TestQuarterNoise(t *testing.T) {
	// Alternating +100 and -100 W around 500 W: every quarter hour difference
	// is 200 W, so the noise of one reading is 100 W.
	series := make([]float32, 96)
	for i := range series {
		if i%2 == 0 {
			series[i] = 600
		} else {
			series[i] = 400
		}
	}
	if got := QuarterNoise(series); got < 80 || got > 120 {
		t.Fatalf("QuarterNoise = %v, want 100 +- 20", got)
	}
	if got := QuarterNoise(nil); got != 0 {
		t.Fatalf("QuarterNoise(nil) = %v, want 0", got)
	}
	if got := QuarterNoise([]float32{500}); got != 0 {
		t.Fatalf("QuarterNoise(one value) = %v, want 0", got)
	}
	// A gap (NaN) does not count as a difference.
	gapped := []float32{500, float32(math.NaN()), 500, 600, 400}
	if got := QuarterNoise(gapped); math.IsNaN(got) || got < 0 {
		t.Fatalf("QuarterNoise with gap = %v, want a finite value", got)
	}
}
