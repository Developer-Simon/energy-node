package autolearn

import (
	"encoding/json"
	"math"
	"sort"
	"time"
)

const (
	// pvRasterDays is how long a sample stays in the raster.
	pvRasterDays = 45
	// PVRasterYearDays is how long a sample stays while the seasonal data are
	// kept for a whole year.
	PVRasterYearDays = 366
	// pvRasterKeep is how many of the highest values each cell keeps.
	pvRasterKeep = 3
	// pvRasterMinElDeg is the sun elevation below which samples are ignored.
	pvRasterMinElDeg = 3.0
	pvDayLayout      = "2006-01-02"
)

// pvSample is one stored power value of a sky cell and the day it was seen.
type pvSample struct {
	W   float64 `json:"w"`
	Day string  `json:"day"`
}

// pvCell holds the highest values of one sky cell, highest first.
type pvCell struct {
	I    int        `json:"i"`
	J    int        `json:"j"`
	Vals []pvSample `json:"vals"`
}

// Raster keeps, per sky cell of azimuth and elevation, the highest power
// values of the last pvRasterDays days.
type Raster struct {
	azDeg float64
	elDeg float64
	cells map[[2]int]*pvCell
}

// NewRaster creates an empty raster with the given cell size in degrees.
func NewRaster(azDeg, elDeg float64) *Raster {
	return &Raster{azDeg: azDeg, elDeg: elDeg, cells: map[[2]int]*pvCell{}}
}

// index returns the cell indices of a sky position.
func (r *Raster) index(az, el float64) [2]int {
	az = math.Mod(az, 360)
	if az < 0 {
		az += 360
	}
	return [2]int{int(math.Floor(az / r.azDeg)), int(math.Floor(el / r.elDeg))}
}

// Add stores a power value w in W seen on day (format 2006-01-02) at the sun
// position az/el. Samples below pvRasterMinElDeg and samples with an
// unparsable day are ignored.
func (r *Raster) Add(az, el, w float64, day string) {
	if el < pvRasterMinElDeg || math.IsNaN(w) {
		return
	}
	if _, err := time.Parse(pvDayLayout, day); err != nil {
		return
	}
	key := r.index(az, el)
	c, ok := r.cells[key]
	if !ok {
		c = &pvCell{I: key[0], J: key[1]}
		r.cells[key] = c
	}
	c.Vals = append(c.Vals, pvSample{W: w, Day: day})
	sort.SliceStable(c.Vals, func(a, b int) bool { return c.Vals[a].W > c.Vals[b].W })
	if len(c.Vals) > pvRasterKeep {
		c.Vals = c.Vals[:pvRasterKeep]
	}
}

// Cell returns the second highest of the top values of the cell at az/el.
// ok is false when the cell has fewer than two values.
func (r *Raster) Cell(az, el float64) (w float64, ok bool) {
	c, found := r.cells[r.index(az, el)]
	if !found || len(c.Vals) < 2 {
		return 0, false
	}
	return c.Vals[1].W, true
}

// Each calls fn with the centre az/el and the Cell value of every cell that
// has a value. Cells with fewer than two values are skipped.
func (r *Raster) Each(fn func(az, el, w float64)) {
	keys := make([][2]int, 0, len(r.cells))
	for k := range r.cells {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		if keys[a][0] != keys[b][0] {
			return keys[a][0] < keys[b][0]
		}
		return keys[a][1] < keys[b][1]
	})
	for _, k := range keys {
		w, ok := r.Cell(float64(k[0])*r.azDeg+r.azDeg/2, float64(k[1])*r.elDeg+r.elDeg/2)
		if !ok {
			continue
		}
		fn(float64(k[0])*r.azDeg+r.azDeg/2, float64(k[1])*r.elDeg+r.elDeg/2, w)
	}
}

// Prune drops samples older than pvRasterDays relative to today, see PruneDays.
func (r *Raster) Prune(today string) {
	r.PruneDays(today, pvRasterDays)
}

// PruneDays drops samples older than days relative to today (format
// 2006-01-02) and removes cells that end up empty. An unparsable today or
// days below 1 leave the raster unchanged.
func (r *Raster) PruneDays(today string, days int) {
	t, err := time.Parse(pvDayLayout, today)
	if err != nil || days < 1 {
		return
	}
	for key, c := range r.cells {
		kept := c.Vals[:0]
		for _, s := range c.Vals {
			d, err := time.Parse(pvDayLayout, s.Day)
			if err != nil {
				continue
			}
			if t.Sub(d) <= time.Duration(days)*24*time.Hour {
				kept = append(kept, s)
			}
		}
		c.Vals = kept
		if len(c.Vals) == 0 {
			delete(r.cells, key)
		}
	}
}

// pvRasterState is the stored form of a raster.
type pvRasterState struct {
	AzDeg float64  `json:"azDeg"`
	ElDeg float64  `json:"elDeg"`
	Cells []pvCell `json:"cells"`
}

// clear leert alle Zellen, die Zellgroesse bleibt.
func (r *Raster) clear() {
	r.cells = map[[2]int]*pvCell{}
}

// State returns the raster as JSON for storage.
func (r *Raster) State() []byte {
	st := pvRasterState{AzDeg: r.azDeg, ElDeg: r.elDeg, Cells: make([]pvCell, 0, len(r.cells))}
	for _, c := range r.cells {
		st.Cells = append(st.Cells, *c)
	}
	sort.Slice(st.Cells, func(a, b int) bool {
		if st.Cells[a].I != st.Cells[b].I {
			return st.Cells[a].I < st.Cells[b].I
		}
		return st.Cells[a].J < st.Cells[b].J
	})
	data, err := json.Marshal(st)
	if err != nil {
		return nil
	}
	return data
}

// LoadRaster restores a raster from State output. Invalid state, or state
// stored with a different cell size than azDeg/elDeg, yields an empty raster.
func LoadRaster(state []byte, azDeg, elDeg float64) *Raster {
	r := NewRaster(azDeg, elDeg)
	if len(state) == 0 {
		return r
	}
	var st pvRasterState
	if err := json.Unmarshal(state, &st); err != nil {
		return r
	}
	if st.AzDeg != azDeg || st.ElDeg != elDeg {
		return r
	}
	for i := range st.Cells {
		c := st.Cells[i]
		r.cells[[2]int{c.I, c.J}] = &c
	}
	return r
}
