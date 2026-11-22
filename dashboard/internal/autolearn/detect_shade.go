package autolearn

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// Shade detection (spec 15.6 "Verschattung"). A shade is a connected group of
// sky cells whose raster value stays below shadeLimit of the clear sky model,
// while the cells around it are normal. The shade sits fixed in the sky, so the
// sun path shows in which weeks and at which time of day it acts.

const (
	// shadeLimit is the quotient of cell value and clear sky value below which a cell is shaded.
	shadeLimit = 0.7
	// shadeClearK is the mean weather factor of a day above which the day counts as clear.
	shadeClearK = 0.85
	// shadeMinDays is the number of distinct clear days a cell must have been filled on.
	shadeMinDays = 5
	// shadeMinCells is the number of connected cells a shade needs.
	shadeMinCells = 3
	// shadeStepMin is the step in minutes of the sun path scan over the year.
	shadeStepMin = 5
	// shadeRoundDeg rounds the centre of a shade in degrees.
	shadeRoundDeg = 5.0
	// shadeQuarterMin is the length of one quarter of the day in minutes.
	shadeQuarterMin = 15
)

// shadeCell is one sky cell of the raster with its quotient and the clear
// days it was filled on.
type shadeCell struct {
	key    [2]int
	az, el float64
	w      float64 // cell value in W
	m      float64 // clear sky model value in W
	q      float64 // w / m
	days   map[string]bool
}

// isShadeCandidate reports whether a cell is shaded and filled on enough clear days.
func isShadeCandidate(c *shadeCell) bool {
	return c.q < shadeLimit && len(c.days) >= shadeMinDays
}

// NewShadeDetector creates the shade detector. raster returns a copy of the
// sky raster, model the stored roof model and whether one exists. The detector
// runs once per round for all devices and returns no fixes.
func NewShadeDetector(raster func() *Raster, model func() (PVModel, bool)) Detector {
	return Detector{
		Name:      "shade",
		Group:     "energy",
		Light:     false,
		PerDevice: false,
		Run: func(ctx context.Context, in *Input, _ string) []Candidate {
			return shadeRun(ctx, in, raster, model)
		},
	}
}

// shadeRun finds the shades of the raster. Without a site location, a safe
// model or a raster it returns nothing.
func shadeRun(ctx context.Context, in *Input, raster func() *Raster, model func() (PVModel, bool)) []Candidate {
	if ctx.Err() != nil || raster == nil || model == nil || in.Doc.Location == nil {
		return nil
	}
	r := raster()
	m, ok := model()
	if r == nil || !ok || !m.Safe {
		return nil
	}
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	site := in.Doc.Location
	cells := shadeCells(r, m)
	if len(cells) == 0 {
		return nil
	}
	clearDays := shadeClearDays(in.Days)
	shadeCountDays(cells, r, in.Days, clearDays, loc, site)

	var out []Candidate
	for _, comp := range shadeComponents(r, cells) {
		if ctx.Err() != nil {
			return nil
		}
		if c, ok := shadeCandidate(r, m, cells, comp, in, clearDays, loc, site); ok {
			out = append(out, c)
		}
	}
	return out
}

// shadeCells returns the raster cells that have a value and a clear sky value,
// with their quotient. The clear sky value is the model without the raster.
func shadeCells(r *Raster, m PVModel) map[[2]int]*shadeCell {
	out := map[[2]int]*shadeCell{}
	for key := range r.cells {
		az := float64(key[0])*r.azDeg + r.azDeg/2
		el := float64(key[1])*r.elDeg + r.elDeg/2
		w, ok := r.Cell(az, el)
		if !ok {
			continue
		}
		mv := m.Best(az, el, nil)
		if mv <= 0 {
			continue
		}
		out[key] = &shadeCell{key: key, az: az, el: el, w: w, m: mv, q: w / mv, days: map[string]bool{}}
	}
	return out
}

// shadeClearDays returns the days whose mean weather factor k is above
// shadeClearK. Days without any k are not clear.
func shadeClearDays(days []DayFile) map[string]bool {
	out := map[string]bool{}
	for _, f := range days {
		sum, n := 0.0, 0
		for _, k := range f.Site.K {
			if k != nil {
				sum += float64(*k)
				n++
			}
		}
		if n > 0 && sum/float64(n) > shadeClearK {
			out[f.Day] = true
		}
	}
	return out
}

// shadeCountDays counts, per cell, the distinct clear days on which the sun
// stood in the cell at a quarter with PV power. The raster keeps only its
// highest samples, so the days come from the day files.
func shadeCountDays(cells map[[2]int]*shadeCell, r *Raster, days []DayFile, clearDays map[string]bool, loc *time.Location, site *sun.Location) {
	for _, f := range days {
		if !clearDays[f.Day] {
			continue
		}
		midnight, err := time.ParseInLocation(dayLayout, f.Day, loc)
		if err != nil {
			continue
		}
		for q := 0; q < dayQuarters; q++ {
			if f.Site.Seen[q] == 0 || f.Site.PV[q] <= 0 {
				continue
			}
			az, el := sun.Position(shadeQuarterTime(midnight, q), site.Lat, site.Lon)
			if el < pvRasterMinElDeg {
				continue
			}
			if c, ok := cells[r.index(az, el)]; ok {
				c.days[f.Day] = true
			}
		}
	}
}

// shadeQuarterTime is the midpoint of quarter q of the day that starts at midnight.
func shadeQuarterTime(midnight time.Time, q int) time.Time {
	return midnight.Add(time.Duration(q*shadeQuarterMin)*time.Minute + time.Duration(shadeQuarterMin)*time.Minute/2)
}

// shadeNeighbours returns the eight neighbour keys of a cell. The azimuth wraps
// around, the elevation does not.
func shadeNeighbours(r *Raster, key [2]int) [][2]int {
	n := int(math.Round(360 / r.azDeg))
	var out [][2]int
	for di := -1; di <= 1; di++ {
		for dj := -1; dj <= 1; dj++ {
			if di == 0 && dj == 0 {
				continue
			}
			i := ((key[0]+di)%n + n) % n
			out = append(out, [2]int{i, key[1] + dj})
		}
	}
	return out
}

// shadeComponents groups the shaded cells that touch each other, 8-neighbourhood.
// The groups are returned in key order.
func shadeComponents(r *Raster, cells map[[2]int]*shadeCell) [][]*shadeCell {
	var keys [][2]int
	for k, c := range cells {
		if isShadeCandidate(c) {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(a, b int) bool {
		if keys[a][0] != keys[b][0] {
			return keys[a][0] < keys[b][0]
		}
		return keys[a][1] < keys[b][1]
	})
	seen := map[[2]int]bool{}
	var comps [][]*shadeCell
	for _, k := range keys {
		if seen[k] {
			continue
		}
		seen[k] = true
		stack := [][2]int{k}
		var comp []*shadeCell
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			comp = append(comp, cells[cur])
			for _, nb := range shadeNeighbours(r, cur) {
				if c, ok := cells[nb]; ok && isShadeCandidate(c) && !seen[nb] {
					seen[nb] = true
					stack = append(stack, nb)
				}
			}
		}
		comps = append(comps, comp)
	}
	return comps
}

// shadeBoundaryOK reports whether the cells around the group are normal: at
// least one filled neighbour lies outside the group, and all of them have a
// quotient of at least shadeLimit. A group that fills the whole raster, such
// as a uniform cloud, has no such neighbour and is rejected.
func shadeBoundaryOK(r *Raster, cells map[[2]int]*shadeCell, comp []*shadeCell) bool {
	inside := map[[2]int]bool{}
	for _, c := range comp {
		inside[c.key] = true
	}
	boundary := 0
	for _, c := range comp {
		for _, nb := range shadeNeighbours(r, c.key) {
			if inside[nb] {
				continue
			}
			o, ok := cells[nb]
			if !ok {
				continue
			}
			if o.q < shadeLimit {
				return false
			}
			boundary++
		}
	}
	return boundary > 0
}

// shadePath is what the sun path over one year says about a shade.
type shadePath struct {
	weeks   []int
	fromMin float64
	toMin   float64
	lostWh  float64
}

// shadeSunPath scans every day of the year of now, from sunrise to sunset in
// shadeStepMin steps, and collects the ISO weeks and the minutes of the day in
// which the sun stands in a cell of the group. The loss adds the cell deficit
// of each step, a rough estimate that uses the clear sky value of the cell.
func shadeSunPath(r *Raster, comp []*shadeCell, year int, loc *time.Location, site *sun.Location) shadePath {
	inside := map[[2]int]*shadeCell{}
	for _, c := range comp {
		inside[c.key] = c
	}
	weekSet := map[int]bool{}
	p := shadePath{fromMin: math.Inf(1), toMin: math.Inf(-1)}
	for day := time.Date(year, 1, 1, 12, 0, 0, 0, loc); day.Year() == year; day = day.AddDate(0, 0, 1) {
		rise, set, ok := sun.Times(day, site.Lat, site.Lon)
		if !ok {
			continue
		}
		for t := rise; t.Before(set); t = t.Add(shadeStepMin * time.Minute) {
			az, el := sun.Position(t, site.Lat, site.Lon)
			if el < pvRasterMinElDeg {
				continue
			}
			c, ok := inside[r.index(az, el)]
			if !ok {
				continue
			}
			local := t.In(loc)
			_, week := local.ISOWeek()
			weekSet[week] = true
			minute := float64(local.Hour()*60 + local.Minute())
			p.fromMin = math.Min(p.fromMin, minute)
			p.toMin = math.Max(p.toMin, minute)
			p.lostWh += (c.m - c.w) * shadeStepMin / 60
		}
	}
	for w := range weekSet {
		p.weeks = append(p.weeks, w)
	}
	sort.Ints(p.weeks)
	return p
}

// shadeEvidence builds the curve form of the most recent clear day with PV
// power before today: the measured quarters, the clear sky curve and the best
// curve with the raster, each per quarter of the day in W. Today is still
// running and would show a cut curve.
func shadeEvidence(r *Raster, m PVModel, days []DayFile, clearDays map[string]bool, today string, loc *time.Location, site *sun.Location) (Evidence, bool) {
	var pick *DayFile
	for i := range days {
		f := &days[i]
		if f.Day >= today || !clearDays[f.Day] || (pick != nil && f.Day <= pick.Day) {
			continue
		}
		for _, pv := range f.Site.PV {
			if pv > 0 {
				pick = f
				break
			}
		}
	}
	if pick == nil {
		return Evidence{}, false
	}
	midnight, err := time.ParseInLocation(dayLayout, pick.Day, loc)
	if err != nil {
		return Evidence{}, false
	}
	ev := Evidence{
		Shape:    "curve",
		Quarters: make([]float32, dayQuarters),
		SollQ:    make([]float32, dayQuarters),
		BestQ:    make([]float32, dayQuarters),
	}
	for q := 0; q < dayQuarters; q++ {
		ev.Quarters[q] = pick.Site.PV[q]
		az, el := sun.Position(shadeQuarterTime(midnight, q), site.Lat, site.Lon)
		ev.SollQ[q] = float32(m.Best(az, el, nil))
		ev.BestQ[q] = float32(m.Best(az, el, r))
	}
	return ev, true
}

// shadeCandidate builds the finding of one shaded group, or reports false when
// the group is too small, not bounded by normal cells, never crosses the sun
// path or has no clear day to show.
func shadeCandidate(r *Raster, m PVModel, cells map[[2]int]*shadeCell, comp []*shadeCell, in *Input, clearDays map[string]bool, loc *time.Location, site *sun.Location) (Candidate, bool) {
	if len(comp) < shadeMinCells || !shadeBoundaryOK(r, cells, comp) {
		return Candidate{}, false
	}
	path := shadeSunPath(r, comp, in.Now.In(loc).Year(), loc, site)
	if len(path.weeks) == 0 {
		return Candidate{}, false
	}
	ev, ok := shadeEvidence(r, m, in.Days, clearDays, in.Now.In(loc).Format(dayLayout), loc, site)
	if !ok {
		return Candidate{}, false
	}

	// Centre of the group: circular mean of the azimuths, mean of the elevations.
	var sinSum, cosSum, elSum float64
	minDays := math.MaxInt
	for _, c := range comp {
		rad := c.az * math.Pi / 180
		sinSum += math.Sin(rad)
		cosSum += math.Cos(rad)
		elSum += c.el
		minDays = min(minDays, len(c.days))
	}
	azC := math.Mod(math.Round(math.Atan2(sinSum, cosSum)*180/math.Pi/shadeRoundDeg)*shadeRoundDeg+360, 360)
	elC := math.Round(elSum/float64(len(comp))/shadeRoundDeg) * shadeRoundDeg

	var season *Season
	from, to := path.weeks[0], path.weeks[len(path.weeks)-1]
	if to-from+1 < 52 {
		season = &Season{FromWeek: from, ToWeek: to}
	}

	kwh := math.Round(path.lostWh/1000*10) / 10
	finding := Finding{
		ID:         fmt.Sprintf("shade:%d:%d", int(azC), int(elC)),
		Kind:       "shade",
		Group:      "check",
		Severity:   "info",
		Confidence: "high",
		Summary: Text{Code: "check.shade", Params: map[string]any{
			"from":     switchTimeClock(path.fromMin),
			"to":       switchTimeClock(path.toMin),
			"weeks":    path.weeks,
			"kwh_lost": kwh,
		}},
		Evidence: ev,
		Season:   season,
	}
	return Candidate{
		Finding:  finding,
		Detector: "shade",
		NEff:     float64(minDays),
		MinNEff:  shadeMinDays,
	}, true
}
