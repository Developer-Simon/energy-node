package autolearn

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func nearly(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}

// observeRun misst k ueber mehrere ungedrosselte Minuten ab start.
func observeRun(w *Weather, start time.Time, minutes int, pv, best float64) {
	for i := 0; i < minutes; i++ {
		w.Observe(start.Add(time.Duration(i)*time.Minute), pv, best, true)
	}
}

func TestWeatherReserveFromMeasuredK(t *testing.T) {
	var w Weather
	t0 := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	observeRun(&w, t0, 10, 3200, 4000)

	at := t0.Add(9 * time.Minute)
	k, kLow, fresh := w.K(at)
	if !nearly(k, 0.8) || !nearly(kLow, 0.8) || !fresh {
		t.Fatalf("K = %v, %v, %v, want 0.8, 0.8, true", k, kLow, fresh)
	}
	res, est := Reserve(4000, 1500, kLow, fresh)
	if !nearly(res, 1700) || est {
		t.Errorf("Reserve = %v, %v, want 1700, false", res, est)
	}
}

func TestWeatherKLowIgnoresOldSamples(t *testing.T) {
	var w Weather
	t0 := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	observeRun(&w, t0, 1, 800, 4000)
	t1 := t0.Add(3 * time.Hour)
	observeRun(&w, t1, 5, 3200, 4000)

	_, kLow, fresh := w.K(t1.Add(4 * time.Minute))
	if !nearly(kLow, 0.8) || !fresh {
		t.Errorf("kLow = %v, fresh = %v, want 0.8, true", kLow, fresh)
	}
}

func TestWeatherNotFreshAfterTwoHours(t *testing.T) {
	var w Weather
	t0 := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	observeRun(&w, t0, 1, 3200, 4000)

	if _, _, fresh := w.K(t0.Add(2*time.Hour - time.Minute)); !fresh {
		t.Fatalf("fresh = false after 1h59m, want true")
	}
	// Ohne Messung im Fenster ist kLow der gealterte Wert: 0.5 + 0.3 / 2^6.
	_, kLow, fresh := w.K(t0.Add(2 * time.Hour))
	if fresh {
		t.Fatal("fresh = true after 2h, want false")
	}
	if want := 0.5 + 0.3/64; !nearly(kLow, want) {
		t.Errorf("kLow = %v, want aged %v", kLow, want)
	}
	res, est := Reserve(4000, 1500, kLow, fresh)
	if want := (4000*kLow - 1500) / 2; !est || !nearly(res, want) {
		t.Errorf("Reserve = %v, %v, want %v, true", res, est, want)
	}
}

func TestWeatherReserveZeroWhenPVAboveBest(t *testing.T) {
	if res, est := Reserve(4000, 3200, 0.8, true); res != 0 || est {
		t.Errorf("pv at best*kLow: Reserve = %v, %v, want 0, false", res, est)
	}
	if res, _ := Reserve(4000, 3500, 0.8, true); res != 0 {
		t.Errorf("pv above best*kLow: Reserve = %v, want 0", res)
	}
}

func TestWeatherThrottledMinuteIsNotMeasured(t *testing.T) {
	var w Weather
	t0 := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	w.Observe(t0, 1000, 4000, false)
	w.Observe(t0.Add(time.Minute), 1000, 0, true)

	k, _, fresh := w.K(t0.Add(time.Minute))
	if fresh || !nearly(k, 0.5) {
		t.Errorf("K = %v, fresh = %v, want season default 0.5, false", k, fresh)
	}
}

func TestWeatherAgesTowardSeasonMedian(t *testing.T) {
	var w Weather
	w.SetSeasonMedian(0.5)
	t0 := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	observeRun(&w, t0, 1, 3200, 4000)

	k, _, _ := w.K(t0.Add(20 * time.Minute))
	if !nearly(k, 0.65) {
		t.Errorf("K after one half-life = %v, want 0.65", k)
	}
}

func TestWeatherLoadJumpRaisesK(t *testing.T) {
	var w Weather
	t0 := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	observeRun(&w, t0, 1, 1000, 4000)

	at := t0.Add(time.Minute)
	w.LiftForLoad(at, 1000, 1000, 4000)
	k, _, fresh := w.K(at)
	if !nearly(k, 0.5) || !fresh {
		t.Fatalf("K after load jump = %v, fresh = %v, want 0.5, true", k, fresh)
	}

	w.LiftForLoad(at.Add(time.Minute), 1000, 0, 4000)
	if k, _, _ := w.K(at.Add(time.Minute)); !nearly(k, 0.5) {
		t.Errorf("K after lift without jump = %v, want unchanged 0.5", k)
	}
}

func TestWeatherLoadJumpClampsToMax(t *testing.T) {
	var w Weather
	t0 := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	w.LiftForLoad(t0, 4000, 4000, 4000)
	if k, _, _ := w.K(t0); !nearly(k, 1.2) {
		t.Errorf("K = %v, want clamp 1.2", k)
	}
}

func TestWeatherStateRoundTrip(t *testing.T) {
	var w Weather
	w.SetSeasonMedian(0.6)
	t0 := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	observeRun(&w, t0, 5, 3200, 4000)

	raw, err := json.Marshal(w.State())
	if err != nil {
		t.Fatal(err)
	}
	var st WeatherState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	back := LoadWeather(st)

	at := t0.Add(6 * time.Minute)
	k1, low1, fresh1 := w.K(at)
	k2, low2, fresh2 := back.K(at)
	if !nearly(k1, k2) || !nearly(low1, low2) || fresh1 != fresh2 {
		t.Errorf("restored K = %v, %v, %v, want %v, %v, %v", k2, low2, fresh2, k1, low1, fresh1)
	}
}

func TestWeatherLoadEmptyState(t *testing.T) {
	w := LoadWeather(WeatherState{})
	k, _, fresh := w.K(time.Now())
	if fresh || !nearly(k, 0.5) {
		t.Errorf("empty state: K = %v, fresh = %v, want 0.5, false", k, fresh)
	}
}
