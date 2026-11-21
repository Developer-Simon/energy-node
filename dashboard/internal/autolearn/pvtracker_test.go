package autolearn

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

const pvTestDay = "2026-06-21"

// pvTestMinute is a summer noon in Berlin, the sun is high.
var pvTestMinute = time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)

func pvTestLocation() (float64, float64, bool) { return 52.5, 13.4, true }

func pvTestFrame(balance map[string]float64) learnrec.Frame {
	return learnrec.Frame{Minute: pvTestMinute, Balance: balance}
}

func pvTestDayFunc() string { return pvTestDay }

func TestPVTrackerAddsUnthrottledMinutes(t *testing.T) {
	r := NewRaster(5, 3)
	tr := NewPVTracker(r, pvTestLocation, nil, pvTestDayFunc, "")
	tr.ObserveMinute(pvTestFrame(map[string]float64{"pv": 900}))
	tr.ObserveMinute(pvTestFrame(map[string]float64{"pv": 1000}))
	if !tr.LastUnthrottled() {
		t.Fatal("LastUnthrottled = false, want true without regulator")
	}
	az, el := sun.Position(pvTestMinute, 52.5, 13.4)
	w, ok := r.Cell(az, el)
	if !ok || w != 900 {
		t.Fatalf("Cell = %v, %v; want 900, true (second highest of 1000, 900)", w, ok)
	}
}

func TestPVTrackerSkipsThrottledMinutes(t *testing.T) {
	r := NewRaster(5, 3)
	reg := &RegulatorView{DeadbandW: 30, MaxW: 800, OutputW: 100, Present: true}
	tr := NewPVTracker(r, pvTestLocation, func() *RegulatorView { return reg }, pvTestDayFunc, "")
	tr.ObserveMinute(pvTestFrame(map[string]float64{"pv": 900, "grid_export": 5}))
	if tr.LastUnthrottled() {
		t.Fatal("LastUnthrottled = true for a throttled minute")
	}
	tr.ObserveMinute(pvTestFrame(map[string]float64{"pv": 900, "grid_export": 5}))
	if len(r.cells) != 0 {
		t.Fatalf("throttled minutes stored %d cells, want 0", len(r.cells))
	}
	tr.ObserveMinute(pvTestFrame(map[string]float64{"pv": 900, "grid_export": 60}))
	tr.ObserveMinute(pvTestFrame(map[string]float64{"pv": 900, "grid_export": 60}))
	if len(r.cells) != 1 {
		t.Fatalf("unthrottled minutes stored %d cells, want 1", len(r.cells))
	}
}

func TestPVTrackerIgnoresMissingLocation(t *testing.T) {
	r := NewRaster(5, 3)
	noLocation := func() (float64, float64, bool) { return 0, 0, false }
	tr := NewPVTracker(r, noLocation, nil, pvTestDayFunc, "")
	tr.ObserveMinute(pvTestFrame(map[string]float64{"pv": 900}))
	tr.ObserveMinute(pvTestFrame(map[string]float64{"pv": 900}))
	if len(r.cells) != 0 {
		t.Fatalf("no location stored %d cells, want 0", len(r.cells))
	}
	if !tr.LastUnthrottled() {
		t.Fatal("LastUnthrottled = false, want true even without location")
	}
}

func TestPVTrackerSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pvraster.json")
	r := NewRaster(5, 3)
	tr := NewPVTracker(r, pvTestLocation, nil, pvTestDayFunc, path)
	tr.ObserveMinute(pvTestFrame(map[string]float64{"pv": 900}))
	tr.ObserveMinute(pvTestFrame(map[string]float64{"pv": 1000}))
	if err := tr.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved raster: %v", err)
	}
	loaded := LoadRaster(data, 5, 3)
	az, el := sun.Position(pvTestMinute, 52.5, 13.4)
	want, wantOK := r.Cell(az, el)
	got, gotOK := loaded.Cell(az, el)
	if got != want || gotOK != wantOK || !gotOK {
		t.Fatalf("loaded Cell = %v, %v; want %v, %v", got, gotOK, want, wantOK)
	}
	if entries, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".pvraster-*.tmp")); len(entries) != 0 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}
