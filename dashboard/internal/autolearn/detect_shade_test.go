package autolearn

import (
	"context"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// shadeSite is Berlin, the site of the shade tests.
var shadeSite = &sun.Location{Lat: 52.52, Lon: 13.405}

// shadeModel is a south facing roof, marked as safe as the fit would report it.
func shadeModel() PVModel {
	return PVModel{Surfaces: []Surface{{PMax: 6000, TiltDeg: 30, AzimuthDeg: 180}}, CapW: 6000, Safe: true}
}

// shadeFixture fills a raster patch from az 190 to 250 and el 3 to 48 with the
// clear sky value of m times factor(i, j), where i and j are the cell indices.
// It also returns one clear day file per day of 2026. A day has PV power and a
// weather factor of 0.9 in all quarters where the sun stands in the patch.
func shadeFixture(t *testing.T, m PVModel, factor func(i, j int) float64) (*Raster, []DayFile) {
	t.Helper()
	r := NewRaster(5, 3)
	for i := 38; i < 50; i++ {
		for j := 1; j < 16; j++ {
			az := float64(i)*5 + 2.5
			el := float64(j)*3 + 1.5
			w := factor(i, j) * m.Best(az, el, nil)
			if w <= 0 {
				continue
			}
			r.Add(az, el, w, "2026-06-01")
			r.Add(az, el, w, "2026-06-02")
		}
	}

	loc := berlinLoc(t)
	midnight, err := time.ParseInLocation(dayLayout, "2026-01-01", loc)
	if err != nil {
		t.Fatal(err)
	}
	var days []DayFile
	for d := 0; d < 365; d++ {
		day := midnight.AddDate(0, 0, d)
		f := DayFile{V: dayFileVersion, Day: day.Format(dayLayout), Devices: map[string]*DeviceDay{}}
		for q := 0; q < dayQuarters; q++ {
			f.Site.Seen[q] = 1
			k := float32(0.9)
			f.Site.K[q] = &k
			az, el := sun.Position(shadeQuarterTime(day, q), shadeSite.Lat, shadeSite.Lon)
			if el >= pvRasterMinElDeg && az >= 190 && az < 250 {
				f.Site.PV[q] = 1000
			}
		}
		days = append(days, f)
	}
	return r, days
}

// runShade runs the shade detector over the fixture of the year 2026.
func runShade(t *testing.T, r *Raster, m PVModel, days []DayFile) []Candidate {
	t.Helper()
	loc := berlinLoc(t)
	in := &Input{
		Now:  time.Date(2026, 12, 31, 12, 0, 0, 0, loc),
		Loc:  loc,
		Days: days,
		Doc:  Doc{Location: shadeSite},
	}
	det := NewShadeDetector(func() *Raster { return r }, func() (PVModel, bool) { return m, true })
	return det.Run(context.Background(), in, "")
}

// shadeInCell marks the cells of a shade at az 210 to 230 and el 12 to 18.
func shadeInCell(i, j int) bool {
	return i >= 42 && i <= 45 && j >= 4 && j <= 5
}

func TestShadeFindsRecurringShade(t *testing.T) {
	m := shadeModel()
	r, days := shadeFixture(t, m, func(i, j int) float64 {
		if shadeInCell(i, j) {
			return 0.4
		}
		return 0.95
	})
	cands := runShade(t, r, m, days)
	if len(cands) != 1 {
		t.Fatalf("got %d candidates, want 1", len(cands))
	}
	f := cands[0].Finding
	if f.ID != "shade:220:15" {
		t.Errorf("ID = %q, want shade:220:15", f.ID)
	}
	weeks, ok := f.Summary.Params["weeks"].([]int)
	if !ok || len(weeks) == 0 {
		t.Errorf("weeks = %v, want a non-empty week list", f.Summary.Params["weeks"])
	}
	if f.Evidence.Shape != "curve" {
		t.Errorf("Shape = %q, want curve", f.Evidence.Shape)
	}
	if len(f.Evidence.Quarters) != 96 || len(f.Evidence.SollQ) != 96 || len(f.Evidence.BestQ) != 96 {
		t.Errorf("curve lengths = %d/%d/%d, want 96 each",
			len(f.Evidence.Quarters), len(f.Evidence.SollQ), len(f.Evidence.BestQ))
	}
	if len(f.Apply) != 0 {
		t.Errorf("Apply = %v, want none", f.Apply)
	}
}

func TestShadeUniformLoweringGivesNone(t *testing.T) {
	m := shadeModel()
	r, days := shadeFixture(t, m, func(i, j int) float64 { return 0.5 })
	if cands := runShade(t, r, m, days); len(cands) != 0 {
		t.Fatalf("got %d candidates, want 0", len(cands))
	}
}

func TestShadeSingleCellGivesNone(t *testing.T) {
	m := shadeModel()
	r, days := shadeFixture(t, m, func(i, j int) float64 {
		if i == 44 && j == 4 {
			return 0.4
		}
		return 0.95
	})
	if cands := runShade(t, r, m, days); len(cands) != 0 {
		t.Fatalf("got %d candidates, want 0", len(cands))
	}
}

func TestShadeUnsafeModelGivesNone(t *testing.T) {
	m := shadeModel()
	m.Safe = false
	r, days := shadeFixture(t, m, func(i, j int) float64 {
		if shadeInCell(i, j) {
			return 0.4
		}
		return 0.95
	})
	if cands := runShade(t, r, m, days); len(cands) != 0 {
		t.Fatalf("got %d candidates, want 0", len(cands))
	}
}
