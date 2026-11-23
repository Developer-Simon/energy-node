package autolearn

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// reserveTestModel is a model whose surfaces give nothing, so Best is the raster
// cell value, capped at 10000 W.
func reserveTestModel() PVModel {
	return PVModel{CapW: 10000, Safe: true}
}

// reserveTestRunner builds a runner with a model in store, a location, a tracker
// on an empty raster and weather with k 0.8 measured just before pvTestMinute.
func reserveTestRunner(t *testing.T, withModel bool) (*PVReserveRunner, *PVTracker) {
	t.Helper()
	store := NewPVModelStore(filepath.Join(t.TempDir(), "pvmodel.json"))
	if withModel {
		store.Set(reserveTestModel())
	}
	tr := NewPVTracker(NewRaster(5, 3), pvTestLocation, nil, pvTestDayFunc, "")
	w := &Weather{}
	for i := 1; i <= 5; i++ {
		w.Observe(pvTestMinute.Add(-time.Duration(i)*time.Minute), 3200, 4000, true)
	}
	return NewPVReserveRunner(tr, store, pvTestLocation, w, ""), tr
}

// seedBest puts a cell of 4000 W at the sun position of pvTestMinute into the
// tracker raster, so Best there is 4000.
func seedBest(tr *PVTracker) {
	tr.ObserveMinute(pvTestFrame(map[string]float64{"pv": 4000}))
	tr.ObserveMinute(pvTestFrame(map[string]float64{"pv": 4000}))
}

func TestPVReserveRunnerReserveFromBestAndK(t *testing.T) {
	r, tr := reserveTestRunner(t, true)
	seedBest(tr)
	// The tracker observes the minute first, as the hub order requires.
	bal := map[string]float64{"pv": 1500}
	tr.ObserveMinute(learnrec.Frame{Minute: pvTestMinute, Balance: bal})
	r.ObserveMinute(learnrec.Frame{Minute: pvTestMinute, Balance: bal})

	w, est, ok := r.Current()
	if !ok || est {
		t.Fatalf("Current ok=%v est=%v, want ok true, est false", ok, est)
	}
	if math.Abs(w-1700) > 0.5 {
		t.Errorf("reserve = %v, want 1700", w)
	}
}

func TestPVReserveRunnerFeedInGivesNoReserve(t *testing.T) {
	r, tr := reserveTestRunner(t, true)
	seedBest(tr)
	// pv 3500 is above best 4000 * kLow 0.8, so the reserve is 0.
	bal := map[string]float64{"pv": 3500}
	tr.ObserveMinute(learnrec.Frame{Minute: pvTestMinute, Balance: bal})
	r.ObserveMinute(learnrec.Frame{Minute: pvTestMinute, Balance: bal})

	w, est, ok := r.Current()
	if !ok || est || w != 0 {
		t.Fatalf("Current = %v, %v, %v; want 0, false, true", w, est, ok)
	}
}

func TestPVReserveRunnerWritesKIntoBalance(t *testing.T) {
	r, tr := reserveTestRunner(t, true)
	seedBest(tr)
	bal := map[string]float64{"pv": 1500}
	tr.ObserveMinute(learnrec.Frame{Minute: pvTestMinute, Balance: bal})
	r.ObserveMinute(learnrec.Frame{Minute: pvTestMinute, Balance: bal})
	// k is the measurement of this minute, 1500 / 4000.
	if k, ok := bal["k"]; !ok || math.Abs(k-0.375) > 1e-9 {
		t.Fatalf("balance k = %v, %v; want 0.375, true", k, ok)
	}

	// A nil balance must not panic.
	r.ObserveMinute(learnrec.Frame{Minute: pvTestMinute})
}

func TestPVReserveRunnerCurrentNotOkWithoutModel(t *testing.T) {
	r, tr := reserveTestRunner(t, false)
	seedBest(tr)
	if _, _, ok := r.Current(); ok {
		t.Fatal("Current ok before any minute")
	}
	bal := map[string]float64{"pv": 1500}
	tr.ObserveMinute(learnrec.Frame{Minute: pvTestMinute, Balance: bal})
	r.ObserveMinute(learnrec.Frame{Minute: pvTestMinute, Balance: bal})
	if _, _, ok := r.Current(); ok {
		t.Fatal("Current ok without model")
	}
}

func TestPVReserveRunnerCurrentNotOkWithoutLocation(t *testing.T) {
	r, tr := reserveTestRunner(t, true)
	r.location = func() (float64, float64, bool) { return 0, 0, false }
	seedBest(tr)
	bal := map[string]float64{"pv": 1500}
	tr.ObserveMinute(learnrec.Frame{Minute: pvTestMinute, Balance: bal})
	r.ObserveMinute(learnrec.Frame{Minute: pvTestMinute, Balance: bal})
	if _, _, ok := r.Current(); ok {
		t.Fatal("Current ok without location")
	}
}

func TestPVReserveRunnerSaveOnlyWhenChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pvweather.json")
	r, tr := reserveTestRunner(t, true)
	r.path = path
	seedBest(tr)
	bal := map[string]float64{"pv": 1500}
	tr.ObserveMinute(learnrec.Frame{Minute: pvTestMinute, Balance: bal})
	r.ObserveMinute(learnrec.Frame{Minute: pvTestMinute, Balance: bal})
	if err := r.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("saved file missing: %v", err)
	}
	var st WeatherState
	if err := json.Unmarshal(first, &st); err != nil || len(st.Samples) == 0 {
		t.Fatalf("saved state invalid: %v, %d samples", err, len(st.Samples))
	}

	// Nothing changed, so the file is not rewritten.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unchanged state was written again: %v", err)
	}

	// The saved file loads back with the same samples.
	back := filepath.Join(dir, "back.json")
	if err := os.WriteFile(back, first, 0o600); err != nil {
		t.Fatal(err)
	}
	if n := len(LoadPVWeather(back, nil).State().Samples); n != len(st.Samples) {
		t.Fatalf("loaded %d samples, want %d", n, len(st.Samples))
	}
	if n := len(LoadPVWeather(filepath.Join(dir, "missing.json"), nil).State().Samples); n != 0 {
		t.Fatalf("missing file loaded %d samples, want 0", n)
	}
}

func TestPVReserveDetectorPublishesRoundedNumber(t *testing.T) {
	cases := []struct {
		name string
		w    float64
		est  bool
		ok   bool
		want float64
		n    int
	}{
		{"rounds to 50", 1712, false, true, 1700, 1},
		{"estimate kept", 830, true, true, 850, 1},
		{"not ok gives nothing", 0, false, false, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			det := PVReserveDetector(func() (float64, bool, bool) { return c.w, c.est, c.ok })
			if det.Name != "learn_pv_reserve" || det.Group != "energy" || !det.Light || det.PerDevice {
				t.Fatalf("detector settings wrong: %+v", det)
			}
			out := det.Run(context.Background(), &Input{}, "")
			if !c.ok {
				if len(out) != 0 {
					t.Fatalf("got %d candidates, want none", len(out))
				}
				return
			}
			if len(out) != 1 || out[0].Value == nil {
				t.Fatalf("got %d candidates, want one learned value", len(out))
			}
			v := out[0].Value
			if v.Key != "site/pv_reserve_w" || v.Kind != ValueNumber || v.Unit != "W" || v.Group != "energy" {
				t.Errorf("value = %+v", v)
			}
			if v.Num != c.want || v.Estimate != c.est || v.N != c.n {
				t.Errorf("Num, Estimate, N = %v, %v, %d; want %v, %v, %d", v.Num, v.Estimate, v.N, c.want, c.est, c.n)
			}
		})
	}
}

func TestPVReserveNeedsDays(t *testing.T) {
	r, tr := reserveTestRunner(t, true)
	// One sample on each of 10 days, each day in its own sky cell so no cell
	// keeps only the top values and drops a day. The sample of pvTestMinute's
	// cell is the reserve base.
	az, el := sun.Position(pvTestMinute, 52.5, 13.4)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		tr.Raster().Add(az+5*float64(i), el, 4000, start.AddDate(0, 0, i).Format(pvDayLayout))
	}
	bal := map[string]float64{"pv": 1500}
	r.ObserveMinute(learnrec.Frame{Minute: pvTestMinute, Balance: bal})
	if _, est, ok := r.Current(); !ok || est {
		t.Fatalf("Current ok=%v est=%v, want ok true, est false", ok, est)
	}

	det := PVReserveDetectorWithDays(r.Current, r.DayCount)
	out := det.Run(context.Background(), &Input{}, "")
	if len(out) != 1 {
		t.Fatalf("got %d candidates, want 1", len(out))
	}
	c := out[0]
	if c.NEff != 10 || c.MinNEff != 14 {
		t.Fatalf("NEff, MinNEff = %v, %v; want 10, 14", c.NEff, c.MinNEff)
	}
	if c.NEff >= c.MinNEff {
		t.Fatal("candidate with 10 days must fall at the gate")
	}

	// A model that does not cover enough sky makes the reserve an estimate.
	r.models.Set(PVModel{CapW: 10000, Safe: false})
	r.ObserveMinute(learnrec.Frame{Minute: pvTestMinute, Balance: bal})
	if _, est, ok := r.Current(); !ok || !est {
		t.Fatalf("Current ok=%v est=%v with unsafe model, want ok true, est true", ok, est)
	}
}
