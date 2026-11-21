package autolearn

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

func pvModelInput(now time.Time) *Input {
	return &Input{Now: now, Limits: learnrec.Limits{GeoStepDeg: 5, GeoSurfaces: 1}}
}

func TestPVModelDetectorFitsAndStores(t *testing.T) {
	r := synthRaster([]Surface{{PMax: 5000, TiltDeg: 30, AzimuthDeg: 180}}, nil)
	store := NewPVModelStore(filepath.Join(t.TempDir(), "pvmodel.json"))
	det := NewPVModelDetector(func() *Raster { return r }, store)

	if det.Name != "pv_model" || det.Group != "energy" || det.Light || det.PerDevice {
		t.Fatalf("detector metadata = %+v", det)
	}
	now := time.Date(2026, 11, 21, 3, 30, 0, 0, time.UTC)
	if got := det.Run(context.Background(), pvModelInput(now), ""); got != nil {
		t.Fatalf("candidates = %v, want nil", got)
	}

	m, ok := store.Model()
	if !ok || len(m.Surfaces) != 1 {
		t.Fatalf("model ok=%v surfaces=%d, want one surface", ok, len(m.Surfaces))
	}
	if m.FittedAt != now.Unix() {
		t.Fatalf("FittedAt = %d, want %d", m.FittedAt, now.Unix())
	}
}

func TestPVModelDetectorCancelledLeavesStoreEmpty(t *testing.T) {
	r := synthRaster([]Surface{{PMax: 5000, TiltDeg: 30, AzimuthDeg: 180}}, nil)
	store := NewPVModelStore(filepath.Join(t.TempDir(), "pvmodel.json"))
	det := NewPVModelDetector(func() *Raster { return r }, store)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := det.Run(ctx, pvModelInput(time.Now()), ""); got != nil {
		t.Fatalf("candidates = %v, want nil", got)
	}
	if _, ok := store.Model(); ok {
		t.Fatal("cancelled run stored a model")
	}
}

func TestPVModelDetectorNilRasterKeepsOldModel(t *testing.T) {
	store := NewPVModelStore(filepath.Join(t.TempDir(), "pvmodel.json"))
	old := PVModel{Surfaces: []Surface{{PMax: 1000, TiltDeg: 10, AzimuthDeg: 90}}, CapW: 1000, FittedAt: 7}
	store.Set(old)
	det := NewPVModelDetector(func() *Raster { return nil }, store)

	det.Run(context.Background(), pvModelInput(time.Now()), "")
	m, ok := store.Model()
	if !ok || m.FittedAt != 7 || len(m.Surfaces) != 1 || m.Surfaces[0].PMax != 1000 {
		t.Fatalf("model changed to %+v ok=%v, want the old model", m, ok)
	}
}

func TestPVModelStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pvmodel.json")
	if _, ok := NewPVModelStore(path).Model(); ok {
		t.Fatal("missing file should give an empty store")
	}

	store := NewPVModelStore(path)
	want := PVModel{Surfaces: []Surface{{PMax: 4800, TiltDeg: 30, AzimuthDeg: 181}}, CapW: 5040, Safe: true, FittedAt: 99}
	store.Set(want)
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, ok := NewPVModelStore(path).Model()
	if !ok || got.CapW != want.CapW || !got.Safe || got.FittedAt != want.FittedAt ||
		len(got.Surfaces) != 1 || got.Surfaces[0] != want.Surfaces[0] {
		t.Fatalf("reloaded model = %+v ok=%v, want %+v", got, ok, want)
	}
}

func TestPVModelStoreSavesOnlyChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pvmodel.json")
	store := NewPVModelStore(path)
	if err := store.Save(); err != nil {
		t.Fatalf("Save on empty store: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("empty store wrote a file, stat err = %v", err)
	}

	store.Set(PVModel{CapW: 1})
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unchanged model was written again, stat err = %v", err)
	}
}

func TestPVTrackerSnapshotIsCopy(t *testing.T) {
	r := synthRaster([]Surface{{PMax: 5000, TiltDeg: 30, AzimuthDeg: 180}}, nil)
	tracker := NewPVTracker(r, nil, nil, func() string { return "2026-11-21" }, "")
	snap := tracker.Snapshot()
	if snap == r {
		t.Fatal("Snapshot returned the live raster")
	}
	before, _ := snap.Cell(180, 30)
	r.Add(180, 30, 1000, "2026-11-21")
	after, _ := snap.Cell(180, 30)
	if before != after {
		t.Fatalf("snapshot changed with the live raster: %v -> %v", before, after)
	}
}
