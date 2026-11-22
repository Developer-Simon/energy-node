package autolearn

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// Rest-Ueberschuss heute (spec 15.6). The surplus left today is forecast from
// the days that looked most like today up to now. The forecast works on the
// weather factor k, the measured PV and the clear sky curve of the roof model.

// NewLearnSurplusDetector returns the detector that publishes the surplus of the
// rest of today as a site value. It is light and runs with the other site
// values. check keeps the 10:00 forecast for the self-check, nil runs without
// it and always publishes the forecast.
func NewLearnSurplusDetector(check *ForecastCheck) Detector {
	return Detector{
		Name:      "learn_surplus",
		Group:     "energy",
		Light:     true,
		PerDevice: false,
		Run: func(ctx context.Context, in *Input, _ string) []Candidate {
			return surplusRun(ctx, in, check)
		},
	}
}

const (
	// surplusMinDays is the number of similar days a forecast needs.
	surplusMinDays = 3
	// surplusGroupPenalty is added to the distance of a day of another day type.
	surplusGroupPenalty = 0.1
	// surplusPercentile is the quantile of the converted rest values that is published.
	surplusPercentile = 0.25
	// surplusRoundStep rounds the result in Wh.
	surplusRoundStep = 100.0
)

// surplusResult is the forecast with the number of days it rests on.
type surplusResult struct {
	wh  float64
	est bool
	n   int
}

// SurplusLeft forecasts the energy of the rest of today, from now to midnight,
// in Wh. It returns ok false without a model, without the day file of today,
// without a site location or with fewer than surplusMinDays similar days.
//
// The distance of a day is the mean absolute difference of the k values up to
// now, plus surplusGroupPenalty when the day type differs. The Limits.SimilarDays
// closest days of the pool, the Limits.SimilarPoolDays most recent days before
// today or all days when that is 0, give the values. Limits.SimilarLastYear is
// ignored here, the last year pool comes later. For each similar day the rest
// is the surplus: measured PV plus unused PV, where unused is max(0, Best*k - PV)
// per quarter, minus the house load LoadTotal. It is converted to today by the ratio of the Best curves over the rest of the
// day. est is set when fewer similar days than SimilarDays were found. The
// result is the 25th percentile of the converted values.
func SurplusLeft(in *Input, now time.Time) (wh float64, est bool, ok bool) {
	r, found := surplusLeft(in, now)
	return r.wh, r.est, found
}

// surplusLeft does the work of SurplusLeft and also returns the number of days.
func surplusLeft(in *Input, now time.Time) (surplusResult, bool) {
	if in.PV == nil || in.Doc.Location == nil {
		return surplusResult{}, false
	}
	m, r, have := in.PV()
	if !have {
		return surplusResult{}, false
	}
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	site := in.Doc.Location
	local := now.In(loc)
	today := local.Format(dayLayout)

	var td *DayFile
	for i := range in.Days {
		if in.Days[i].Day == today {
			td = &in.Days[i]
		}
	}
	if td == nil {
		return surplusResult{}, false
	}
	todayMidnight, err := time.ParseInLocation(dayLayout, today, loc)
	if err != nil {
		return surplusResult{}, false
	}
	q0 := (local.Hour()*60 + local.Minute()) / shadeQuarterMin

	picks := surplusPicks(in, td, today, q0)
	if len(picks) < surplusMinDays {
		return surplusResult{}, false
	}

	var vals []float64
	for _, f := range picks {
		v, ok := surplusConvert(f, m, r, loc, site, todayMidnight, q0)
		if ok {
			vals = append(vals, v)
		}
	}
	if len(vals) < surplusMinDays {
		return surplusResult{}, false
	}
	sort.Float64s(vals)
	idx := int(surplusPercentile * float64(len(vals)-1))
	limit := in.Limits.SimilarDays
	return surplusResult{
		wh:  vals[idx],
		est: limit > 0 && len(vals) < limit,
		n:   len(vals),
	}, true
}

// surplusPicks returns the similar days of today, closest first. Ties go to
// the more recent day.
func surplusPicks(in *Input, td *DayFile, today string, q0 int) []*DayFile {
	type cand struct {
		f    *DayFile
		dist float64
	}
	pool := surplusPool(in, today)

	todayType, haveToday := in.Base.DayType[today]
	var cands []cand
	for _, f := range pool {
		dist, ok := surplusDistance(td, f, q0)
		if !ok {
			continue
		}
		if t, have := in.Base.DayType[f.Day]; haveToday && have && t != todayType {
			dist += surplusGroupPenalty
		}
		cands = append(cands, cand{f: f, dist: dist})
	}
	sort.SliceStable(cands, func(a, b int) bool {
		if cands[a].dist != cands[b].dist {
			return cands[a].dist < cands[b].dist
		}
		return cands[a].f.Day > cands[b].f.Day
	})
	if n := in.Limits.SimilarDays; n > 0 && len(cands) > n {
		cands = cands[:n]
	}
	out := make([]*DayFile, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.f)
	}
	return out
}

// surplusPool returns the days before the day before, most recent first, at
// most Limits.SimilarPoolDays of them (0 for all).
func surplusPool(in *Input, before string) []*DayFile {
	var pool []*DayFile
	for i := range in.Days {
		f := &in.Days[i]
		if f.Day < before {
			pool = append(pool, f)
		}
	}
	sort.Slice(pool, func(a, b int) bool { return pool[a].Day > pool[b].Day })
	if n := in.Limits.SimilarPoolDays; n > 0 && len(pool) > n {
		pool = pool[:n]
	}
	return pool
}

// surplusPoolRests returns the rest of the pool days before day from quarter q0
// on, each measured on its own day, the rest of surplusConvert without the
// conversion. The median of these is the season median of the rest.
func surplusPoolRests(in *Input, m PVModel, r *Raster, before string, q0 int) []float64 {
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	var out []float64
	for _, f := range surplusPool(in, before) {
		midnight, err := time.ParseInLocation(dayLayout, f.Day, loc)
		if err != nil {
			continue
		}
		if v, ok := surplusConvert(f, m, r, loc, in.Doc.Location, midnight, q0); ok {
			out = append(out, v)
		}
	}
	return out
}

// surplusDistance is the mean absolute difference of the k values of two days
// over the quarters before q0 that both days have. It reports false without a
// common quarter.
func surplusDistance(today, f *DayFile, q0 int) (float64, bool) {
	sum, n := 0.0, 0
	for q := 0; q < q0; q++ {
		a, b := today.Site.K[q], f.Site.K[q]
		if a == nil || b == nil {
			continue
		}
		sum += math.Abs(float64(*a) - float64(*b))
		n++
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

// surplusConvert returns the surplus of the rest of the day of one similar day in
// Wh, scaled to today. Per quarter the potential is the measured PV plus the
// unused PV, and the surplus is the part the house does not use:
// max(0, potential - LoadTotal) over 0.25 h. The rest runs from quarter q0 on,
// over the quarters the day has a measurement for, and quarters without
// LoadTotal data are skipped. A day without sun in the rest has no ratio and
// counts as 0.
func surplusConvert(f *DayFile, m PVModel, r *Raster, loc *time.Location, site *sun.Location, todayMidnight time.Time, q0 int) (float64, bool) {
	dayMidnight, err := time.ParseInLocation(dayLayout, f.Day, loc)
	if err != nil {
		return 0, false
	}
	rest, dayBest, todayBest := 0.0, 0.0, 0.0
	for q := q0; q < dayQuarters; q++ {
		if f.Site.Seen[q] == 0 {
			continue
		}
		az, el := sun.Position(shadeQuarterTime(dayMidnight, q), site.Lat, site.Lon)
		best := m.Best(az, el, r)
		k := 0.0
		if kv := f.Site.K[q]; kv != nil {
			k = float64(*kv)
		}
		pv := float64(f.Site.PV[q])
		unused := math.Max(0, best*k-pv)
		potential := pv + unused
		load := float64(f.Site.LoadTotal[q])
		rest += math.Max(0, potential-load) * shadeQuarterMin / 60
		dayBest += best

		taz, tel := sun.Position(shadeQuarterTime(todayMidnight, q), site.Lat, site.Lon)
		todayBest += m.Best(taz, tel, r)
	}
	if dayBest <= 0 {
		return 0, true
	}
	return rest * todayBest / dayBest, true
}

// surplusRun publishes the rest surplus of today as site/surplus_left_wh. With a
// check it first runs the self-check, and when the season median has been the
// better forecast, it publishes that median with Estimate.
func surplusRun(ctx context.Context, in *Input, check *ForecastCheck) []Candidate {
	if ctx.Err() != nil {
		return nil
	}
	if check != nil {
		surplusSelfCheck(in, check)
	}
	res, ok := surplusLeft(in, in.Now)
	if !ok {
		return nil
	}
	if check != nil && check.UseSeasonMedian() {
		if med, ok := surplusSeasonMedian(in, in.Now); ok {
			res.wh, res.est = med, true
		}
	}
	return []Candidate{{
		Detector: "learn_surplus",
		NEff:     float64(res.n),
		MinNEff:  surplusMinDays,
		Value: &Value{Key: "site/surplus_left_wh",
			Kind: ValueNumber, Unit: "Wh", Group: "energy",
			Num:      math.Round(res.wh/surplusRoundStep) * surplusRoundStep,
			N:        res.n,
			Estimate: res.est},
	}}
}

// surplusSelfCheck keeps the forecast of today at 10:00 once the day has reached
// 10:00, and closes the days before today with their measured rest and the
// season median of that day. A day without its file or without a median is
// closed without an entry. Without a model it does nothing.
func surplusSelfCheck(in *Input, check *ForecastCheck) {
	if in.PV == nil || in.Doc.Location == nil {
		return
	}
	m, r, have := in.PV()
	if !have {
		return
	}
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	local := in.Now.In(loc)
	today := local.Format(dayLayout)
	if local.Hour() >= forecastHour && !check.Recorded(today) {
		at := time.Date(local.Year(), local.Month(), local.Day(), forecastHour, 0, 0, 0, loc)
		if res, ok := surplusLeft(in, at); ok {
			check.Record(today, res.wh)
		}
	}
	for _, day := range check.Pending() {
		if day >= today {
			continue
		}
		f := dayFileOf(in, day)
		midnight, err := time.ParseInLocation(dayLayout, day, loc)
		if f == nil || err != nil {
			check.Close(day, 0, math.NaN())
			continue
		}
		actual, ok := surplusConvert(f, m, r, loc, in.Doc.Location, midnight, forecastQuarter)
		if !ok {
			check.Close(day, 0, math.NaN())
			continue
		}
		rests := surplusPoolRests(in, m, r, day, forecastQuarter)
		check.Close(day, actual, medianOrNaN(rests))
	}
}

// surplusSeasonMedian is the median of the pool days' rest from now on, the
// value that replaces the forecast when the self-check prefers it.
func surplusSeasonMedian(in *Input, now time.Time) (float64, bool) {
	if in.PV == nil || in.Doc.Location == nil {
		return 0, false
	}
	m, r, have := in.PV()
	if !have {
		return 0, false
	}
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	local := now.In(loc)
	today := local.Format(dayLayout)
	q0 := (local.Hour()*60 + local.Minute()) / shadeQuarterMin
	med := medianOrNaN(surplusPoolRests(in, m, r, today, q0))
	if math.IsNaN(med) {
		return 0, false
	}
	return med, true
}

// dayFileOf returns the day file of day in the input, nil when there is none.
func dayFileOf(in *Input, day string) *DayFile {
	for i := range in.Days {
		if in.Days[i].Day == day {
			return &in.Days[i]
		}
	}
	return nil
}

// medianOrNaN is the median of xs, NaN for none.
func medianOrNaN(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	return Median(xs)
}
