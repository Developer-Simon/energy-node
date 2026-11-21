package autolearn

import (
	"math"
	"math/rand"
	"testing"
)

// kneeSynthDays returns 120 days of temperatures spread over 0..25 degC and the
// energy produced by fn with a small deterministic noise.
func kneeSynthDays(seed int64, fn func(T float64) float64) (temp, y []float64) {
	rng := rand.New(rand.NewSource(seed))
	for i := 0; i < 120; i++ {
		T := 25 * rng.Float64()
		temp = append(temp, T)
		y = append(y, fn(T)+0.2*rng.NormFloat64())
	}
	return temp, y
}

func TestFitKneeHeatPump(t *testing.T) {
	temp, y := kneeSynthDays(1, func(T float64) float64 {
		return 2 + 0.8*math.Max(0, 15-T)
	})
	knee, _, b, explained, ok := FitKnee(temp, y, false)
	if math.Abs(knee-15) > 1 {
		t.Fatalf("knee = %v, want 15 +- 1", knee)
	}
	if math.Abs(b-0.8) > 0.15 {
		t.Fatalf("b = %v, want 0.8 +- 0.15", b)
	}
	if !ok {
		t.Fatalf("ok = false, explained = %v, want a trusted fit", explained)
	}
}

func TestFitKneeCooling(t *testing.T) {
	// Most days sit above the cooling knee, so the median of y lies on the
	// sloped part and the explained share is meaningful.
	rng := rand.New(rand.NewSource(2))
	var temp, y []float64
	for i := 0; i < 120; i++ {
		T := 10 + 20*rng.Float64()
		temp = append(temp, T)
		y = append(y, 1+0.5*math.Max(0, T-18)+0.2*rng.NormFloat64())
	}
	knee, _, b, _, ok := FitKnee(temp, y, true)
	if math.Abs(knee-18) > 1 {
		t.Fatalf("knee = %v, want 18 +- 1", knee)
	}
	if math.Abs(b-0.5) > 0.15 {
		t.Fatalf("b = %v, want 0.5 +- 0.15", b)
	}
	if !ok {
		t.Fatal("ok = false, want a trusted cooling fit")
	}
}

func TestFitKneeNoTemperatureLink(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	var temp, y []float64
	for i := 0; i < 120; i++ {
		temp = append(temp, 25*rng.Float64())
		y = append(y, 5+rng.NormFloat64())
	}
	_, _, _, explained, ok := FitKnee(temp, y, false)
	if ok {
		t.Fatalf("ok = true for a device without temperature link, explained = %v", explained)
	}
}

func TestFitKneeFewerThanSixPoints(t *testing.T) {
	temp := []float64{1, 5, 10, 15, 20}
	y := []float64{9, 7, 5, 3, 1}
	if _, _, _, _, ok := FitKnee(temp, y, false); ok {
		t.Fatal("ok = true with five points, want false")
	}
}

func TestFitKneeDropsNaN(t *testing.T) {
	temp, y := kneeSynthDays(4, func(T float64) float64 {
		return 2 + 0.8*math.Max(0, 15-T)
	})
	temp = append(temp, math.NaN(), 3)
	y = append(y, 4, math.Inf(1))
	knee, _, _, _, ok := FitKnee(temp, y, false)
	if math.Abs(knee-15) > 1 {
		t.Fatalf("knee = %v with NaN input, want 15 +- 1", knee)
	}
	if !ok {
		t.Fatal("ok = false with NaN pairs dropped, want true")
	}
}

func TestFitKneeLengthMismatch(t *testing.T) {
	temp := []float64{1, 2, 3, 4, 5, 6, 7}
	y := []float64{1, 2, 3}
	if _, _, _, _, ok := FitKnee(temp, y, false); ok {
		t.Fatal("ok = true for mismatched lengths, want false")
	}
}
