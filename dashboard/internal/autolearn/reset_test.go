package autolearn

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

func assertGone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s still exists after Reset (err %v)", path, err)
	}
}

func TestPVTrackerResetClearsRasterAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pvraster.json")
	tr := NewPVTracker(NewRaster(5, 3), nil, nil, func() string { return "2026-11-20" }, path)
	tr.Raster().Add(180, 40, 1200, "2026-11-20")
	if err := tr.Save(); err != nil {
		t.Fatal(err)
	}
	if err := tr.Reset(); err != nil {
		t.Fatalf("Reset = %v", err)
	}
	assertGone(t, path)
	if n := len(tr.Raster().cells); n != 0 {
		t.Fatalf("raster keeps %d cells after Reset", n)
	}
	if err := tr.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Save after Reset writes no file: %v", err)
	}
	if n := len(LoadRaster(tr.Raster().State(), 5, 3).cells); n != 0 {
		t.Fatalf("saved raster after Reset has %d cells", n)
	}
}

func TestPVModelStoreResetClearsModelAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pvmodel.json")
	s := NewPVModelStore(path)
	s.Set(PVModel{})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if err := s.Reset(); err != nil {
		t.Fatalf("Reset = %v", err)
	}
	assertGone(t, path)
	if _, ok := s.Model(); ok {
		t.Fatal("model still present after Reset")
	}
	// Ohne Modell schreibt Save nichts: eine fehlende Datei ist der leere Stand.
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	assertGone(t, path)
}

func TestPVReserveRunnerResetClearsWeatherAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pvweather.json")
	r := NewPVReserveRunner(nil, nil, nil, LoadWeather(WeatherState{}), path)
	r.weather.Observe(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC), 1000, 1200, true)
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	if err := r.Reset(); err != nil {
		t.Fatalf("Reset = %v", err)
	}
	assertGone(t, path)
	if !reflect.DeepEqual(r.weather.State(), LoadWeather(WeatherState{}).State()) {
		t.Fatal("weather state not empty after Reset")
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Save after Reset writes no file: %v", err)
	}
}

func TestForecastCheckResetClearsEntriesAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "forecast.json")
	c := LoadForecastCheck(path, func(string, ...any) {})
	c.Record("2026-11-20", 5000)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if err := c.Reset(); err != nil {
		t.Fatalf("Reset = %v", err)
	}
	assertGone(t, path)
	if c.Recorded("2026-11-20") || len(c.Pending()) != 0 {
		t.Fatal("forecast entries survive Reset")
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Save after Reset writes no file: %v", err)
	}
	if again := LoadForecastCheck(path, func(string, ...any) {}); len(again.Pending()) != 0 {
		t.Fatal("reloaded check has pending days after Reset")
	}
}

func TestEngineResetClearsCandidatesKeepsLevel(t *testing.T) {
	e := newTestEngine(t, learnrec.LimitsFor(learnrec.LevelBalanced, 4))
	e.Register(Detector{Name: "rule", Group: "habit", Light: true, Run: func(ctx context.Context, in *Input, device string) []Candidate {
		return []Candidate{{Finding: Finding{Kind: "rule"}}}
	}})
	e.Tick(at(t, "2026-11-20", "10:00"), habitOnly)
	if len(e.Candidates()) != 1 {
		t.Fatal("precondition: no candidates after a run")
	}
	if err := e.Reset(); err != nil {
		t.Fatalf("Reset = %v", err)
	}
	if got := e.Candidates(); len(got) != 0 {
		t.Fatalf("candidates after Reset = %+v", got)
	}
	e.Tick(at(t, "2026-11-20", "10:20"), habitOnly)
	if len(e.Candidates()) != 1 {
		t.Fatal("engine does not run again after Reset")
	}
}
