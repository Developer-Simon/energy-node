package autolearn

import (
	"math"
	"testing"
)

func TestRasterCellReturnsSecondHighest(t *testing.T) {
	r := NewRaster(5, 3)
	r.Add(100, 40, 900, "2026-11-01")
	r.Add(101, 40, 1000, "2026-11-02")
	r.Add(102, 40, 1300, "2026-11-03")

	w, ok := r.Cell(100, 40)
	if !ok || w != 1000 {
		t.Fatalf("Cell = %v, %v; want 1000, true", w, ok)
	}
}

func TestRasterCellNeedsTwoValues(t *testing.T) {
	r := NewRaster(5, 3)
	if _, ok := r.Cell(100, 40); ok {
		t.Fatal("empty cell reported ok")
	}
	r.Add(100, 40, 800, "2026-11-01")
	if _, ok := r.Cell(100, 40); ok {
		t.Fatal("cell with one value reported ok")
	}
}

func TestRasterSpikeAloneReturnsLowerOfTwo(t *testing.T) {
	r := NewRaster(5, 3)
	r.Add(100, 40, 1000, "2026-11-01")
	r.Add(100, 40, 1300, "2026-11-02") // 30 % spike
	w, ok := r.Cell(100, 40)
	if !ok || w != 1000 {
		t.Fatalf("Cell = %v, %v; want 1000, true", w, ok)
	}
}

func TestRasterPruneDropsOldValues(t *testing.T) {
	r := NewRaster(5, 3)
	r.Add(100, 40, 2000, "2026-09-01") // 75 days before today
	r.Add(100, 40, 900, "2026-11-01")
	r.Add(100, 40, 1000, "2026-11-02")
	r.Add(200, 40, 500, "2026-09-01") // only old value, cell disappears

	r.Prune("2026-11-15")

	w, ok := r.Cell(100, 40)
	if !ok || w != 900 {
		t.Fatalf("after Prune Cell = %v, %v; want 900, true", w, ok)
	}
	if _, ok := r.Cell(200, 40); ok {
		t.Fatal("cell with only old values survived Prune")
	}
}

func TestRasterPruneKeeps45DayBoundary(t *testing.T) {
	r := NewRaster(5, 3)
	r.Add(100, 40, 1600, "2026-09-30") // 46 days before 2026-11-15, must go
	r.Add(100, 40, 1500, "2026-10-01") // exactly 45 days before 2026-11-15, kept
	r.Add(100, 40, 900, "2026-11-01")
	r.Prune("2026-11-15")
	if w, ok := r.Cell(100, 40); !ok || w != 900 {
		t.Fatalf("after Prune Cell = %v, %v; want 900, true (1500 kept, 1600 gone)", w, ok)
	}
	r.Add(100, 40, 1100, "2026-11-10")
	if w, ok := r.Cell(100, 40); !ok || w != 1100 {
		t.Fatalf("Cell = %v, %v; want 1100, true", w, ok)
	}
}

func TestRasterCellSizeFollowsConstructor(t *testing.T) {
	r := NewRaster(10, 6)
	r.Add(101, 40, 1000, "2026-11-01") // cell az 10, el 6
	r.Add(107, 41, 800, "2026-11-02")  // same 10 x 6 degree cell
	r.Add(111, 40, 500, "2026-11-03")  // next az cell

	w, ok := r.Cell(105, 40)
	if !ok || w != 800 {
		t.Fatalf("Cell(105,40) = %v, %v; want 800, true", w, ok)
	}
	if _, ok := r.Cell(115, 40); ok {
		t.Fatal("az 111 must be in another 10 degree cell than 105")
	}
}

func TestRasterStateRoundTrip(t *testing.T) {
	r := NewRaster(5, 3)
	r.Add(100, 40, 900, "2026-11-01")
	r.Add(100, 40, 1000, "2026-11-02")
	r.Add(250, 20, 300, "2026-11-03")
	r.Add(250, 20, 400, "2026-11-04")

	loaded := LoadRaster(r.State(), 5, 3)
	w, ok := loaded.Cell(100, 40)
	if !ok || w != 900 {
		t.Fatalf("round trip Cell(100,40) = %v, %v; want 900, true", w, ok)
	}
	w, ok = loaded.Cell(250, 20)
	if !ok || w != 300 {
		t.Fatalf("round trip Cell(250,20) = %v, %v; want 300, true", w, ok)
	}
}

func TestLoadRasterDifferentCellSizeStartsEmpty(t *testing.T) {
	r := NewRaster(5, 3)
	r.Add(100, 40, 900, "2026-11-01")
	r.Add(100, 40, 1000, "2026-11-02")

	loaded := LoadRaster(r.State(), 2, 1)
	if _, ok := loaded.Cell(100, 40); ok {
		t.Fatal("state with other cell size must start empty")
	}
}

func TestLoadRasterInvalidStateStartsEmpty(t *testing.T) {
	if _, ok := LoadRaster([]byte("not json"), 5, 3).Cell(100, 40); ok {
		t.Fatal("invalid state must start empty")
	}
	if _, ok := LoadRaster(nil, 5, 3).Cell(100, 40); ok {
		t.Fatal("nil state must start empty")
	}
}

func TestRasterIgnoresLowSun(t *testing.T) {
	r := NewRaster(5, 3)
	r.Add(100, 2.9, 900, "2026-11-01")
	r.Add(100, 2.9, 1000, "2026-11-02")
	if _, ok := r.Cell(100, 2.9); ok {
		t.Fatal("samples below 3 degrees must be ignored")
	}
	r.Add(100, 3, 900, "2026-11-01")
	r.Add(100, 3, 1000, "2026-11-02")
	if w, ok := r.Cell(100, 3); !ok || w != 900 {
		t.Fatalf("Cell at 3 degrees = %v, %v; want 900, true", w, ok)
	}
}

func TestRasterEachVisitsCellsWithCentre(t *testing.T) {
	r := NewRaster(5, 3)
	r.Add(100, 40, 900, "2026-11-01")
	r.Add(100, 40, 1000, "2026-11-02")
	r.Add(300, 20, 50, "2026-11-01") // single value, skipped

	var got [][3]float64
	r.Each(func(az, el, w float64) {
		got = append(got, [3]float64{az, el, w})
	})
	if len(got) != 1 {
		t.Fatalf("Each visited %d cells, want 1", len(got))
	}
	if math.Abs(got[0][0]-102.5) > 1e-9 || math.Abs(got[0][1]-40.5) > 1e-9 || got[0][2] != 900 {
		t.Fatalf("Each got %v, want centre 102.5/40.5 value 900", got[0])
	}
}

func TestRasterPruneDaysKeepsTheGivenWindow(t *testing.T) {
	r := NewRaster(5, 5)
	r.Add(180, 30, 1000, "2026-01-10")
	r.Add(180, 30, 900, "2026-01-11")
	r.Add(180, 60, 4000, "2025-06-20")
	r.Add(180, 60, 3900, "2025-06-21")
	r.PruneDays("2026-01-20", PVRasterYearDays)
	if _, ok := r.Cell(180, 60); !ok {
		t.Fatal("summer cell dropped within a year")
	}
	r.PruneDays("2026-01-20", 56)
	if _, ok := r.Cell(180, 60); ok {
		t.Fatal("summer cell kept beyond 56 days")
	}
	if _, ok := r.Cell(180, 30); !ok {
		t.Fatal("recent cell dropped")
	}
}
