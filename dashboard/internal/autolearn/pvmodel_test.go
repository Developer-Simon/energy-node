package autolearn

import (
	"math"
	"testing"
	"time"
)

// synthRaster fills a 5 by 3 degree raster with the power of the given
// surfaces at every sky cell centre, skipping azimuths for which skip is true.
// Each cell gets two values so that Cell reports it.
func synthRaster(surfaces []Surface, skip func(az float64) bool) *Raster {
	r := NewRaster(5, 3)
	for i := 0; i < 72; i++ {
		az := float64(i)*5 + 2.5
		if skip != nil && skip(az) {
			continue
		}
		for j := 1; j < 30; j++ {
			el := float64(j)*3 + 1.5
			w := 0.0
			for _, s := range surfaces {
				w += s.PMax * pvGeom(az, el, s.TiltDeg, s.AzimuthDeg)
			}
			if w <= 0 {
				continue
			}
			r.Add(az, el, w, "2026-11-01")
			r.Add(az, el, w, "2026-11-02")
		}
	}
	return r
}

func TestFitPVSouthRoofWithThrottledNoon(t *testing.T) {
	r := synthRaster([]Surface{{PMax: 5000, TiltDeg: 30, AzimuthDeg: 180}},
		func(az float64) bool { return az >= 150 && az <= 210 })

	m, ok := FitPV(r, 5, 1)
	if !ok || len(m.Surfaces) != 1 {
		t.Fatalf("FitPV ok=%v surfaces=%d, want one surface", ok, len(m.Surfaces))
	}
	s := m.Surfaces[0]
	if math.Abs(s.TiltDeg-30) > 5 {
		t.Errorf("tilt = %v, want 30 ± 5", s.TiltDeg)
	}
	if math.Abs(s.AzimuthDeg-180) > 5 {
		t.Errorf("azimuth = %v, want 180 ± 5", s.AzimuthDeg)
	}
	if math.Abs(s.PMax-5000) > 300 {
		t.Errorf("PMax = %v, want 5000 ± 300", s.PMax)
	}
}

func TestFitPVEastWestGivesTwoSurfaces(t *testing.T) {
	r := synthRaster([]Surface{
		{PMax: 2500, TiltDeg: 45, AzimuthDeg: 90},
		{PMax: 2500, TiltDeg: 45, AzimuthDeg: 270},
	}, nil)

	m, ok := FitPV(r, 5, 2)
	if !ok {
		t.Fatal("FitPV reported not ok")
	}
	if len(m.Surfaces) != 2 {
		t.Fatalf("surfaces = %d, want 2: %+v", len(m.Surfaces), m.Surfaces)
	}
	near := func(a, want float64) bool {
		d := math.Mod(math.Abs(a-want), 360)
		return math.Min(d, 360-d) <= 15
	}
	east, west := m.Surfaces[0], m.Surfaces[1]
	if !(near(east.AzimuthDeg, 90) && near(west.AzimuthDeg, 270)) &&
		!(near(east.AzimuthDeg, 270) && near(west.AzimuthDeg, 90)) {
		t.Errorf("azimuths = %v and %v, want 90 and 270 within 15 deg", east.AzimuthDeg, west.AzimuthDeg)
	}
}

func TestFitPVSafeNeedsSixtyDegreesOfSun(t *testing.T) {
	narrow := synthRaster([]Surface{{PMax: 5000, TiltDeg: 30, AzimuthDeg: 180}},
		func(az float64) bool { return az < 100 || az > 140 })
	m, ok := FitPV(narrow, 5, 1)
	if !ok {
		t.Fatal("FitPV with 40 degrees of sun reported not ok")
	}
	if m.Safe {
		t.Error("Safe = true with only 40 degrees of sun azimuth")
	}

	wide := synthRaster([]Surface{{PMax: 5000, TiltDeg: 30, AzimuthDeg: 180}}, nil)
	m, ok = FitPV(wide, 5, 1)
	if !ok || !m.Safe {
		t.Errorf("Safe = %v, ok = %v with the full sky, want true, true", m.Safe, ok)
	}
}

func TestFitPVNeedsTwentyCells(t *testing.T) {
	r := NewRaster(5, 3)
	for i := 0; i < 5; i++ {
		r.Add(float64(i)*5+2.5, 30, 1000, "2026-11-01")
		r.Add(float64(i)*5+2.5, 30, 1000, "2026-11-02")
	}
	if m, ok := FitPV(r, 5, 1); ok || len(m.Surfaces) != 0 {
		t.Fatalf("FitPV with 5 cells = %+v, %v; want empty, false", m, ok)
	}
}

func TestFitPVEcoStepIsFast(t *testing.T) {
	r := synthRaster([]Surface{
		{PMax: 2500, TiltDeg: 20, AzimuthDeg: 90},
		{PMax: 2500, TiltDeg: 20, AzimuthDeg: 270},
	}, nil)
	start := time.Now()
	if _, ok := FitPV(r, 5, 2); !ok {
		t.Fatal("FitPV reported not ok")
	}
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Fatalf("FitPV took %v, want under 1.5 s", d)
	}
}

func TestBestZeroBelowThreeDegrees(t *testing.T) {
	m := PVModel{Surfaces: []Surface{{PMax: 5000, TiltDeg: 30, AzimuthDeg: 180}}, CapW: 6000}
	if got := m.Best(180, 2, nil); got != 0 {
		t.Fatalf("Best at 2 deg = %v, want 0", got)
	}
}

func TestBestUsesModelInEmptyCellAndCapsAtCapW(t *testing.T) {
	m := PVModel{Surfaces: []Surface{{PMax: 5000, TiltDeg: 30, AzimuthDeg: 180}}, CapW: 3000}
	r := NewRaster(5, 3)

	if got := m.Best(180, 60, r); got != 3000 {
		t.Fatalf("Best above CapW = %v, want 3000", got)
	}

	m.CapW = 6000
	want := 5000 * pvGeom(180, 60, 30, 180)
	if got := m.Best(180, 60, r); math.Abs(got-want) > 1e-6 {
		t.Fatalf("Best in empty cell = %v, want model %v", got, want)
	}
}

func TestBestPrefersCellWhenHigher(t *testing.T) {
	m := PVModel{Surfaces: []Surface{{PMax: 1000, TiltDeg: 30, AzimuthDeg: 180}}, CapW: 6000}
	r := NewRaster(5, 3)
	r.Add(180, 60, 4000, "2026-11-01")
	r.Add(180, 60, 4200, "2026-11-02")
	if got := m.Best(180, 60, r); got != 4000 {
		t.Fatalf("Best = %v, want cell value 4000", got)
	}
}
