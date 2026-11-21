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

// LearnSurplusDetector publishes the surplus of the rest of today as a site
// value. It is light and runs with the other site values.
var LearnSurplusDetector = Detector{
	Name:      "learn_surplus",
	Group:     "energy",
	Light:     true,
	PerDevice: false,
	Run:       surplusRun,
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
// is measured PV plus unused PV, where unused is max(0, Best*k - PV) per quarter.
// It is converted to today by the ratio of the Best curves over the rest of the
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
	var pool []*DayFile
	for i := range in.Days {
		f := &in.Days[i]
		if f.Day < today {
			pool = append(pool, f)
		}
	}
	sort.Slice(pool, func(a, b int) bool { return pool[a].Day > pool[b].Day })
	if n := in.Limits.SimilarPoolDays; n > 0 && len(pool) > n {
		pool = pool[:n]
	}

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

// surplusConvert returns the rest of the day of one similar day in Wh, scaled to
// today. The rest runs from quarter q0 on, over the quarters the day has a
// measurement for. A day without sun in the rest has no ratio and counts as 0.
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
		rest += (pv + unused) * shadeQuarterMin / 60
		dayBest += best

		taz, tel := sun.Position(shadeQuarterTime(todayMidnight, q), site.Lat, site.Lon)
		todayBest += m.Best(taz, tel, r)
	}
	if dayBest <= 0 {
		return 0, true
	}
	return rest * todayBest / dayBest, true
}

// surplusRun publishes the rest surplus of today as site/surplus_left_wh.
func surplusRun(ctx context.Context, in *Input, _ string) []Candidate {
	if ctx.Err() != nil {
		return nil
	}
	res, ok := surplusLeft(in, in.Now)
	if !ok {
		return nil
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
